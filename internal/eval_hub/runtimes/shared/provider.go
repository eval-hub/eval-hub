package shared

import (
	"fmt"

	_ "github.com/eval-hub/eval-hub/internal/eval_hub/postprocessing" // Register the post-processing workload.
	"github.com/eval-hub/eval-hub/internal/eval_hub/workloads"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// ProviderStorage provides the catalog lookup required for ordinary benchmarks.
type ProviderStorage interface {
	GetProvider(id string) (*api.ProviderResource, error)
}

// WorkloadType identifies a server-managed workload from its stored job and
// verifies that the effective benchmark belongs to that workload.
func WorkloadType(evaluation *api.EvaluationJobResource, benchmark api.EvaluationBenchmarkConfig) (workloads.Type, error) {
	var job *api.EvaluationJobConfig
	if evaluation != nil {
		job = &evaluation.EvaluationJobConfig
	}
	if definition, ok := workloads.ForJob(job); ok {
		if benchmark.ProviderID != definition.ProviderID || benchmark.ID != definition.BenchmarkID {
			return "", fmt.Errorf("workload %q benchmark does not match the stored job", definition.Type)
		}
		return definition.Type, nil
	}
	if workloads.IsInternalProviderID(benchmark.ProviderID) {
		return "", fmt.Errorf("internal workload provider %q cannot be resolved as a conventional benchmark", benchmark.ProviderID)
	}
	return workloads.BenchmarkEvaluation, nil
}

// ProviderForBenchmark builds a runtime provider for an internal workload or
// resolves a conventional benchmark provider from the catalog.
func ProviderForBenchmark(
	evaluation *api.EvaluationJobResource,
	benchmark api.EvaluationBenchmarkConfig,
	storage ProviderStorage,
) (*api.ProviderResource, error) {
	workloadType, err := WorkloadType(evaluation, benchmark)
	if err != nil {
		return nil, err
	}
	if workloadType != workloads.BenchmarkEvaluation {
		definition, ok := workloads.ByType(workloadType)
		if !ok {
			return nil, fmt.Errorf("workload %q is not registered", workloadType)
		}
		provider := definition.RuntimeProvider()
		if provider == nil {
			return nil, fmt.Errorf("workload %q did not provide a runtime provider", workloadType)
		}
		return provider, nil
	}
	if storage == nil {
		return nil, fmt.Errorf("provider %q is not configured", benchmark.ProviderID)
	}
	provider, err := storage.GetProvider(benchmark.ProviderID)
	if err != nil {
		return nil, err
	}
	if provider == nil {
		return nil, fmt.Errorf("provider %q is not configured", benchmark.ProviderID)
	}
	return provider, nil
}
