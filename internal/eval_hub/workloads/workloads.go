// Package workloads registers server-managed workloads for runtime provider resolution.
package workloads

import (
	"log/slog"

	"github.com/eval-hub/eval-hub/pkg/api"
)

// Type identifies how a benchmark's execution provider is resolved.
type Type string

const BenchmarkEvaluation Type = "benchmark-evaluation"

// Workload describes a workload that supplies its own runtime provider.
type Workload struct {
	Type            Type
	ProviderID      string
	BenchmarkID     string
	Internal        bool
	MatchesJob      func(*api.EvaluationJobConfig) bool
	RuntimeProvider func() *api.ProviderResource
}

var registry []Workload

// Register adds a workload during package initialization. Invalid or duplicate
// registrations are logged and ignored.
func Register(workload Workload) {
	if workload.Type == "" || workload.Type == BenchmarkEvaluation || workload.ProviderID == "" || workload.BenchmarkID == "" ||
		workload.MatchesJob == nil || workload.RuntimeProvider == nil {
		slog.Error("Incomplete workload registration", "type", workload.Type, "provider_id", workload.ProviderID, "benchmark_id", workload.BenchmarkID)
		return
	}
	for _, registered := range registry {
		if registered.Type == workload.Type ||
			(registered.ProviderID == workload.ProviderID && registered.BenchmarkID == workload.BenchmarkID) {
			slog.Error("Duplicate workload registration", "type", workload.Type, "provider_id", workload.ProviderID, "benchmark_id", workload.BenchmarkID)
			return
		}
	}
	registry = append(registry, workload)
}

// ForJob returns the registered workload that owns a stored job.
func ForJob(job *api.EvaluationJobConfig) (Workload, bool) {
	for _, workload := range registry {
		if workload.MatchesJob(job) {
			return workload, true
		}
	}
	return Workload{}, false
}

// ByType returns a registered workload by type.
func ByType(workloadType Type) (Workload, bool) {
	for _, workload := range registry {
		if workload.Type == workloadType {
			return workload, true
		}
	}
	return Workload{}, false
}

// IsInternalProviderID reports whether an internal workload registered an ID.
func IsInternalProviderID(id string) bool {
	for _, workload := range registry {
		if workload.Internal && workload.ProviderID == id {
			return true
		}
	}
	return false
}
