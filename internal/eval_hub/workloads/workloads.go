// Package workloads registers workload implementations for API and runtime use.
package workloads

import (
	"context"
	"log/slog"
	"sync"

	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// Type identifies a stored job's workload. These values are persisted in
// evaluations.workload_type and must remain stable.
type Type string

const (
	Evaluation     Type = "evaluation"
	PostProcessing Type = "post-processing"
)

type workloadTypeContextKey struct{}

// WithType adds an optional workload scope to ctx for storage lookups.
func WithType(ctx context.Context, workloadType Type) context.Context {
	return context.WithValue(ctx, workloadTypeContextKey{}, workloadType)
}

// TypeFromContext returns the optional workload scope carried by ctx.
func TypeFromContext(ctx context.Context) Type {
	if ctx == nil {
		return ""
	}
	workloadType, _ := ctx.Value(workloadTypeContextKey{}).(Type)
	return workloadType
}

// GetOptions preserves each workload's public identifier and observability names.
type GetOptions struct {
	PathParameter    string
	ResourceName     string
	SpanName         string
	TraceIDAttribute string
}

// Workload supplies job identification, API conversion, operation validation,
// and runtime provider resolution. Implementations must be safe for concurrent
// use after registration. Routes select a workload independently of request data.
type Workload interface {
	Type() Type
	ProviderID() string
	BenchmarkID() string
	Internal() bool
	MatchesJob(*api.EvaluationJobConfig) bool
	RuntimeProvider() *api.ProviderResource
	GetOptions() GetOptions
	NewRequest() any
	ToEvaluationJob(request any) (*api.EvaluationJobConfig, error)
	ResourceFromJob(*api.EvaluationJobResource) (any, error)

	// ValidateCreate checks workload invariants after request decoding. Schema,
	// catalog, source, and runtime validation in the existing create handlers
	// still applies until those handlers are migrated to this interface.
	ValidateCreate(*api.EvaluationJobConfig) error
	ValidateGet(id string, job *api.EvaluationJobResource) error
	ValidateDelete(id string, job *api.EvaluationJobResource) error
}

var (
	registryMu sync.RWMutex
	registry   []Workload
)

// Register accepts any Workload implementation. Invalid or duplicate
// registrations are logged and ignored. Conventional evaluations use catalog
// providers and do not require a fixed internal provider or benchmark.
func Register(workload Workload) {
	if workload == nil {
		slog.Error("Cannot register a nil workload")
		return
	}
	if workload.Type() == "" ||
		(workload.Type() != Evaluation && (workload.ProviderID() == "" || workload.BenchmarkID() == "")) {
		slog.Error("Incomplete workload registration", "type", workload.Type(), "provider_id", workload.ProviderID(), "benchmark_id", workload.BenchmarkID())
		return
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	for _, registered := range registry {
		if registered.Type() == workload.Type() ||
			(workload.ProviderID() != "" && registered.ProviderID() == workload.ProviderID() && registered.BenchmarkID() == workload.BenchmarkID()) {
			slog.Error("Duplicate workload registration", "type", workload.Type(), "provider_id", workload.ProviderID(), "benchmark_id", workload.BenchmarkID())
			return
		}
	}
	registry = append(registry, workload)
}

// registeredWorkloads takes a snapshot so implementations run without holding
// the registry lock while concurrent registrations remain safe.
func registeredWorkloads() []Workload {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return append([]Workload(nil), registry...)
}

// ForJob returns the workload that owns a stored job. The conventional
// evaluation implementation is the fallback, regardless of registration order.
func ForJob(job *api.EvaluationJobConfig) Workload {
	var evaluation Workload
	for _, workload := range registeredWorkloads() {
		if workload.Type() == Evaluation {
			evaluation = workload
			continue
		}
		if workload.MatchesJob(job) {
			return workload
		}
	}
	return evaluation
}

// TypeForJob returns the registered workload type, or the default evaluation type.
func TypeForJob(job *api.EvaluationJobConfig) Type {
	if workload := ForJob(job); workload != nil {
		return workload.Type()
	}
	return Evaluation
}

// ByType returns a registered workload by type.
func ByType(workloadType Type) Workload {
	for _, workload := range registeredWorkloads() {
		if workload.Type() == workloadType {
			return workload
		}
	}
	return nil
}

// IsInternalProviderID reports whether an internal workload registered an ID.
func IsInternalProviderID(id string) bool {
	for _, workload := range registeredWorkloads() {
		if workload.Internal() && workload.ProviderID() == id {
			return true
		}
	}
	return false
}

// NotFound uses the public resource name even when storage uses a shared job model.
func NotFound(workload Workload, id string) error {
	return serviceerrors.NewServiceError(messages.ResourceNotFound, "Type", workload.GetOptions().ResourceName, "ResourceId", id)
}

// ValidateResource rejects absent jobs and jobs belonging to another workload.
func ValidateResource(workload Workload, id string, job *api.EvaluationJobResource) error {
	if job == nil || !workload.MatchesJob(&job.EvaluationJobConfig) {
		return NotFound(workload, id)
	}
	return nil
}
