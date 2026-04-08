package shared

import (
	"fmt"

	"github.com/eval-hub/eval-hub/pkg/api"
)

// JobSpec is the JSON structure written to job.json for benchmark adapters to consume.
type JobSpec struct {
	JobID          string `json:"id"`
	ProviderID     string `json:"provider_id"`
	BenchmarkID    string `json:"benchmark_id"`
	BenchmarkIndex int    `json:"benchmark_index"`
	// Tenant is the API evaluation tenant (X-Tenant scope); adapters/SDKs should prefer this over inferring from the pod namespace.
	Tenant         string              `json:"tenant,omitempty"`
	Model          api.ModelRef        `json:"model"`
	NumExamples    *int                `json:"num_examples,omitempty"`
	Parameters     map[string]any      `json:"parameters"`
	ExperimentName string              `json:"experiment_name,omitempty"`
	Tags           []api.ExperimentTag `json:"tags,omitempty"`
	CallbackURL    *string             `json:"callback_url"`
	Exports        *JobSpecExports     `json:"exports,omitempty"`
}

// JobSpecExports is the subset of EvaluationExports serialized into the job spec (excludes k8s connection config).
type JobSpecExports struct {
	OCI *JobSpecExportsOCI `json:"oci,omitempty"`
}

// JobSpecExportsOCI contains OCI coordinates for artifact export.
type JobSpecExportsOCI struct {
	Coordinates api.OCICoordinates `json:"coordinates"`
}

// BuildJobSpecJSON builds a JobSpec from evaluation data and returns it as indented JSON.
func BuildJobSpec(
	evaluation *api.EvaluationJobResource,
	providerID string,
	benchmarkConfig *api.EvaluationBenchmarkConfig,
	benchmarkIndex int,
	callbackURL *string,
) (*JobSpec, error) {
	if benchmarkConfig == nil {
		return nil, fmt.Errorf("benchmark is required")
	}
	benchmarkParams := CopyParams(benchmarkConfig.Parameters)
	numExamples := NumExamplesFromParameters(benchmarkParams)
	delete(benchmarkParams, "num_examples")

	spec := JobSpec{
		JobID:          evaluation.Resource.ID,
		ProviderID:     providerID,
		BenchmarkID:    benchmarkConfig.ID,
		BenchmarkIndex: benchmarkIndex,
		Model:          evaluation.Model,
		NumExamples:    numExamples,
		Parameters:     benchmarkParams,
		CallbackURL:    callbackURL,
	}
	if !evaluation.Resource.Tenant.IsEmpty() {
		spec.Tenant = evaluation.Resource.Tenant.String()
	}
	if evaluation.Experiment != nil {
		spec.ExperimentName = evaluation.Experiment.Name
		spec.Tags = evaluation.Experiment.Tags
	}
	if evaluation.Exports != nil && evaluation.Exports.OCI != nil {
		spec.Exports = &JobSpecExports{
			OCI: &JobSpecExportsOCI{
				Coordinates: evaluation.Exports.OCI.Coordinates,
			},
		}
	}

	return &spec, nil
}

// CopyParams creates a shallow copy of a parameters map.
func CopyParams(source map[string]any) map[string]any {
	if len(source) == 0 {
		return map[string]any{}
	}
	clone := make(map[string]any, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

// NumExamplesFromParameters extracts num_examples from a parameters map.
func NumExamplesFromParameters(parameters map[string]any) *int {
	if parameters == nil {
		return nil
	}
	value, ok := parameters["num_examples"]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case int:
		return &typed
	case int32:
		converted := int(typed)
		return &converted
	case int64:
		converted := int(typed)
		return &converted
	case float64:
		converted := int(typed)
		return &converted
	default:
		return nil
	}
}
