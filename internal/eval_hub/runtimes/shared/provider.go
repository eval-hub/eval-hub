package shared

import (
	"fmt"
	"strings"

	"github.com/eval-hub/eval-hub/internal/eval_hub/postprocessing"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// ProviderRuntimeKind identifies the runtime that will execute a benchmark.
type ProviderRuntimeKind int

const (
	// ProviderRuntimeKubernetes selects the Kubernetes runtime configuration.
	ProviderRuntimeKubernetes ProviderRuntimeKind = iota
	// ProviderRuntimeLocal selects the local runtime configuration.
	ProviderRuntimeLocal
)

// ProviderStorage provides the catalog lookup required for ordinary benchmarks.
type ProviderStorage interface {
	GetProvider(id string) (*api.ProviderResource, error)
}

// ProviderForBenchmark resolves the provider used to execute a benchmark.
// Dedicated post-processing jobs use the service runtime configuration; other
// jobs always resolve their provider from the catalog.
func ProviderForBenchmark(
	evaluation *api.EvaluationJobResource,
	benchmark api.EvaluationBenchmarkConfig,
	postProcessingRuntime *api.Runtime,
	runtimeKind ProviderRuntimeKind,
	storage ProviderStorage,
) (*api.ProviderResource, error) {
	if evaluation != nil && postprocessing.IsPostProcessingJob(&evaluation.EvaluationJobConfig) &&
		postProcessingRuntimeConfigured(postProcessingRuntime, runtimeKind) {
		return postprocessing.RuntimeProvider(postProcessingRuntime), nil
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

func postProcessingRuntimeConfigured(runtimeConfig *api.Runtime, runtimeKind ProviderRuntimeKind) bool {
	if runtimeConfig == nil {
		return false
	}
	switch runtimeKind {
	case ProviderRuntimeKubernetes:
		return runtimeConfig.K8s != nil && strings.TrimSpace(runtimeConfig.K8s.Image) != ""
	case ProviderRuntimeLocal:
		return runtimeConfig.Local != nil && strings.TrimSpace(runtimeConfig.Local.Command) != ""
	default:
		return false
	}
}
