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
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/internal/eval_hub/workloads"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/google/uuid"
)

const (
	launchFailureCode = "launch_failed"
)

// generationRuntime checks whether the runtime can start a generation worker.
// Secret validation checks only for existence in the tenant namespace.
type generationRuntime interface {
	SupportsCollectionGeneration() bool
	ValidateModelSecret(ctx context.Context, namespace, name string) error
}

// JobStorage is the tenant/owner/workload-scoped subset required by the generation service.
// RuntimeStorage is included so the Kubernetes runtime can use the same scoped store for callbacks.
type JobStorage interface {
	abstractions.RuntimeStorage
	CreateEvaluationJob(*api.EvaluationJobResource) error
}

// GenerationCleanupProtector claims a visible job for cleanup and returns its
// status after cancellation, if required. The retention deadline stays unchanged.
type GenerationCleanupProtector interface {
	ProtectCollectionGenerationJobForCleanup(string, *api.MessageInfo) (*api.EvaluationJobResource, error)
	DeleteCleanedCollectionGenerationJob(string) error
}

// Service admits and owns the lifecycle of internal collection-generation workloads.
// Public HTTP routing and proposal orchestration are provided by separate work items.
type Service struct {
	storage abstractions.Storage
	runtime abstractions.Runtime
	config  *config.GenerationConfig
	logger  *slog.Logger
	now     func() time.Time
	newID   func() string
}

func NewService(storage abstractions.Storage, runtime abstractions.Runtime, cfg *config.GenerationConfig, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg == nil {
		cfg = &config.GenerationConfig{}
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
	runtime, ok := s.runtime.(generationRuntime)
	if !ok || !runtime.SupportsCollectionGeneration() {
		return nil, errors.New("runtime does not support collection generation")
	}
	if auth := normalized.Model.Auth; auth != nil {
		if err := runtime.ValidateModelSecret(ctx, tenant.String(), auth.SecretRef); err != nil {
			var serviceErr *serviceerrors.ServiceError
			if errors.As(err, &serviceErr) {
				return nil, serviceErr
			}
			return nil, serviceerrors.NewServiceError(messages.GenerationModelSecretLookupUnavailable).WithCause(err)
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

// Get returns an unexpired generation job within the caller's tenant.
func (s *Service) Get(ctx context.Context, tenant api.Tenant, owner api.User, id string) (*api.CollectionGenerationResource, error) {
	if s == nil || s.storage == nil {
		return nil, errors.New("collection generation is not configured")
	}
	if tenant.IsEmpty() {
		return nil, errors.New("tenant is required for collection generation")
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

// Delete atomically cancels an active job and installs a bounded cleanup lease
// before deleting runtime resources. The original retention deadline remains
// unchanged, so a failed cleanup can be retried only during the record's retention
// window and cannot keep the row alive indefinitely.
func (s *Service) Delete(ctx context.Context, tenant api.Tenant, owner api.User, id string) error {
	if s == nil || s.storage == nil || s.runtime == nil {
		return errors.New("collection generation is not configured")
	}
	if tenant.IsEmpty() {
		return errors.New("tenant is required for collection generation")
	}
	jobCtx := s.operationContext(ctx)
	store := s.scopedStorage(jobCtx, tenant, owner)
	protector, ok := store.(GenerationCleanupProtector)
	if !ok {
		return errors.New("storage cannot protect collection-generation job during cleanup")
	}
	message := api.WithMessageOrigin(&api.MessageInfo{
		Message: "Collection-generation job cancelled by the caller.", MessageCode: "collection_generation_cancelled",
	}, api.MessageOriginServer)
	job, err := protector.ProtectCollectionGenerationJobForCleanup(id, message)
	if err != nil {
		return fmt.Errorf("protect collection-generation job before runtime cleanup: %w", err)
	}
	cleanupCtx, cancelCleanup := context.WithTimeout(jobCtx, generation.RuntimeResourceCleanupTimeout)
	defer cancelCleanup()
	if err := s.runtime.WithLogger(s.logger).WithContext(cleanupCtx).DeleteEvaluationJobResources(job); err != nil {
		return fmt.Errorf("clean up collection-generation runtime resources: %w", err)
	}
	if err := protector.DeleteCleanedCollectionGenerationJob(id); err != nil {
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
