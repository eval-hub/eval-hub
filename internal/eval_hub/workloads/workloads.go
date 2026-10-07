// Package workloads registers server-managed workloads for runtime provider resolution.
package workloads

import (
	"log/slog"
	"sync"

	"github.com/eval-hub/eval-hub/pkg/api"
)

// Type identifies a stored job's workload. These values are persisted in
// evaluations.workload_type and must remain stable.
type Type string

const Evaluation Type = "evaluation"

// Workload describes a workload that supplies its own runtime provider.
type Workload struct {
	Type            Type
	ProviderID      string
	BenchmarkID     string
	Internal        bool
	MatchesJob      func(*api.EvaluationJobConfig) bool
	RuntimeProvider func() *api.ProviderResource
}

var (
	registryMu sync.RWMutex
	registry   []Workload
)

// Register adds a workload. Invalid or duplicate registrations are logged and ignored.
func Register(workload Workload) {
	// Conventional evaluations use catalog providers and benchmarks, so they
	// cannot be represented by one fixed provider/benchmark registration.
	if workload.Type == Evaluation {
		slog.Error("Conventional evaluation cannot be registered as a workload", "type", workload.Type)
		return
	}
	if workload.Type == "" || workload.ProviderID == "" || workload.BenchmarkID == "" ||
		workload.MatchesJob == nil || workload.RuntimeProvider == nil {
		slog.Error("Incomplete workload registration", "type", workload.Type, "provider_id", workload.ProviderID, "benchmark_id", workload.BenchmarkID)
		return
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	for _, registered := range registry {
		if registered.Type == workload.Type ||
			(registered.ProviderID == workload.ProviderID && registered.BenchmarkID == workload.BenchmarkID) {
			slog.Error("Duplicate workload registration", "type", workload.Type, "provider_id", workload.ProviderID, "benchmark_id", workload.BenchmarkID)
			return
		}
	}
	registry = append(registry, workload)
}

// registeredWorkloads takes a snapshot so MatchesJob can run without holding
// the registry lock while concurrent registrations remain safe.
func registeredWorkloads() []Workload {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return append([]Workload(nil), registry...)
}

// ForJob returns the registered workload that owns a stored job.
func ForJob(job *api.EvaluationJobConfig) *Workload {
	workloads := registeredWorkloads()
	for i := range workloads {
		if workloads[i].MatchesJob(job) {
			return &workloads[i]
		}
	}
	return nil
}

// TypeForJob returns the registered workload type, or the default evaluation type.
func TypeForJob(job *api.EvaluationJobConfig) Type {
	if workload := ForJob(job); workload != nil {
		return workload.Type
	}
	return Evaluation
}

// ByType returns a registered workload by type.
func ByType(workloadType Type) *Workload {
	workloads := registeredWorkloads()
	for i := range workloads {
		if workloads[i].Type == workloadType {
			return &workloads[i]
		}
	}
	return nil
}

// IsInternalProviderID reports whether an internal workload registered an ID.
func IsInternalProviderID(id string) bool {
	for _, workload := range registeredWorkloads() {
		if workload.Internal && workload.ProviderID == id {
			return true
		}
	}
	return false
}
