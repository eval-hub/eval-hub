package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/eval-hub/eval-hub/internal/agent/loop"
	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_hub/generation"
	"github.com/eval-hub/eval-hub/internal/eval_hub/workloads"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/google/uuid"
)

const (
	launchFailureCode = "launch_failed"
)

// ModelSecretValidator verifies a reference in the job's tenant namespace. Implementations
// must not return or log Secret data.
type ModelSecretValidator interface {
	ValidateModelSecret(ctx context.Context, namespace, name string) error
}

// JobStorage is the tenant/owner/workload-scoped subset required by the generation service.
// RuntimeStorage is included so the Kubernetes runtime can use the same scoped store for callbacks.
type JobStorage interface {
	abstractions.RuntimeStorage
	CreateEvaluationJob(*api.EvaluationJobResource) error
	UpdateEvaluationJobStatus(string, api.OverallState, *api.MessageInfo) error
	DeleteEvaluationJob(string) error
}

// GenerationExpiryResetter clears a terminal generation job's expiry when its
// runtime resources could not be removed, so callers can retry DELETE.
type GenerationExpiryResetter interface {
	ClearCollectionGenerationExpiry(string) error
}

// Service admits and owns the lifecycle of internal collection-generation workloads.
// Public HTTP routing and proposal orchestration are provided by separate work items.
type Service struct {
	storage abstractions.Storage
	runtime abstractions.Runtime
	config  *config.ServiceGenerationConfig
	logger  *slog.Logger
	now     func() time.Time
	newID   func() string
}

func NewService(storage abstractions.Storage, runtime abstractions.Runtime, cfg *config.ServiceGenerationConfig, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg == nil {
		cfg = &config.ServiceGenerationConfig{}
	}
	return &Service{storage: storage, runtime: runtime, config: cfg, logger: logger, now: time.Now, newID: uuid.NewString}
}

// Admit normalizes the request, resolves all server-owned controls, validates any
// tenant Secret reference, persists one generation workload row, and starts its worker.
func (s *Service) Admit(ctx context.Context, tenant api.Tenant, owner api.User, request api.CollectionGenerationRequest) (*api.EvaluationJobResource, error) {
	if s == nil || s.storage == nil || s.runtime == nil {
		return nil, errors.New("collection generation is not configured")
	}
	if tenant.IsEmpty() || strings.TrimSpace(owner.String()) == "" {
		return nil, errors.New("tenant and owner are required for collection generation")
	}
	if err := s.config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid collection-generation configuration: %w", err)
	}
	normalized, err := generation.NormalizeRequest(request)
	if err != nil {
		return nil, err
	}
	image := strings.TrimSpace(s.config.RunnerImage)
	if image == "" {
		return nil, errors.New("collection-generation runner image is not configured")
	}
	if auth := normalized.Model.Auth; auth != nil {
		validator, ok := s.runtime.(ModelSecretValidator)
		if !ok {
			return nil, errors.New("runtime cannot validate model Secret references")
		}
		if err := validator.ValidateModelSecret(ctx, tenant.String(), auth.SecretRef); err != nil {
			return nil, fmt.Errorf("model Secret reference %q is unavailable in tenant namespace %q", auth.SecretRef, tenant.String())
		}
	}
	presets, err := s.config.EffectiveEffortBudgets()
	if err != nil {
		return nil, err
	}
	budget, err := presets.Resolve(loop.Effort(normalized.GenerationEffort), loop.DefaultHardLimits())
	if err != nil {
		return nil, err
	}
	reasoningEffort := s.config.ResolveReasoningEffort(normalized.Model.URL, normalized.Model.Name, normalized.GenerationEffort)
	job, err := generation.NewJob(s.newID(), tenant, owner, normalized, image, budget, reasoningEffort, s.config.EffectiveRetention(), s.now().UTC())
	if err != nil {
		return nil, err
	}
	jobCtx := s.operationContext(ctx)
	store := s.scopedStorage(jobCtx, tenant, owner)
	if err := store.CreateEvaluationJob(job); err != nil {
		return nil, fmt.Errorf("persist collection-generation job: %w", err)
	}
	if err := s.runtime.WithLogger(s.logger).WithContext(jobCtx).RunEvaluationJob(job, job.Benchmarks, store); err != nil {
		if failErr := store.UpdateEvaluationJob(job.Resource.ID, launchFailureEvent()); failErr != nil {
			return nil, fmt.Errorf("persist collection-generation launch failure: %w", failErr)
		}
	}
	return store.GetEvaluationJob(job.Resource.ID)
}

// Get returns a generation job scoped to its tenant, owner, workload type, and retention.
func (s *Service) Get(ctx context.Context, tenant api.Tenant, owner api.User, id string) (*api.CollectionGenerationResource, error) {
	if s == nil || s.storage == nil {
		return nil, errors.New("collection generation is not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	job, err := s.scopedStorage(ctx, tenant, owner).GetEvaluationJob(id)
	if err != nil {
		return nil, err
	}
	resource, err := generation.ResourceFromJob(job)
	if err != nil {
		return nil, fmt.Errorf("read collection-generation job: %w", err)
	}
	return resource, nil
}

// Delete cancels an active job, removes its runtime resources, and then deletes
// the record. If runtime cleanup fails, it clears the retention deadline so the
// record remains pollable and a later DELETE can retry cleanup.
func (s *Service) Delete(ctx context.Context, tenant api.Tenant, owner api.User, id string) error {
	if s == nil || s.storage == nil || s.runtime == nil {
		return errors.New("collection generation is not configured")
	}
	jobCtx := s.operationContext(ctx)
	store := s.scopedStorage(jobCtx, tenant, owner)
	job, err := store.GetEvaluationJob(id)
	if err != nil {
		return err
	}
	if !generation.IsJob(&job.EvaluationJobConfig) {
		return errors.New("evaluation job is not a collection-generation workload")
	}
	if job.Status == nil {
		return errors.New("collection-generation job has no status")
	}
	if !job.Status.State.IsTerminalState() {
		message := api.WithMessageOrigin(&api.MessageInfo{
			Message: "Collection-generation job cancelled by the caller.", MessageCode: "collection_generation_cancelled",
		}, api.MessageOriginServer)
		if updateErr := store.UpdateEvaluationJobStatus(id, api.OverallStateCancelled, message); updateErr != nil {
			// A terminal worker callback may win the race. Continue only if that state
			// is now visible; the SQL terminal-state guard rejects late callbacks.
			current, readErr := store.GetEvaluationJob(id)
			if readErr != nil || current.Status == nil || !current.Status.State.IsTerminalState() {
				return fmt.Errorf("cancel collection-generation job: %w", updateErr)
			}
			job = current
		}
	}
	if err := s.runtime.WithLogger(s.logger).WithContext(jobCtx).DeleteEvaluationJobResources(job); err != nil {
		if resetter, ok := store.(GenerationExpiryResetter); ok {
			if resetErr := resetter.ClearCollectionGenerationExpiry(id); resetErr != nil {
				return errors.Join(fmt.Errorf("clean up collection-generation runtime resources: %w", err), fmt.Errorf("preserve generation record for cleanup retry: %w", resetErr))
			}
		}
		return fmt.Errorf("clean up collection-generation runtime resources: %w", err)
	}
	if err := store.DeleteEvaluationJob(id); err != nil {
		return fmt.Errorf("delete collection-generation job record: %w", err)
	}
	return nil
}

func (s *Service) operationContext(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithoutCancel(ctx)
}

func (s *Service) scopedStorage(ctx context.Context, tenant api.Tenant, owner api.User) JobStorage {
	return s.storage.WithLogger(s.logger).WithContext(ctx).WithTenant(tenant).WithOwner(owner).WithWorkloadType(workloads.CollectionGeneration)
}

func launchFailureEvent() *api.StatusEvent {
	message := api.WithMessageOrigin(&api.MessageInfo{Message: generation.LaunchFailureMessage, MessageCode: launchFailureCode}, api.MessageOriginServer)
	return &api.StatusEvent{BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
		ProviderID: generation.ProviderID, ID: generation.BenchmarkID, BenchmarkIndex: 0, Status: api.StateFailed,
		ErrorMessage: message,
		AdditionalInfo: map[string]any{"collection_generation_error": api.CollectionGenerationError{
			Code: launchFailureCode, Message: generation.LaunchFailureMessage,
		}},
	}}
}
