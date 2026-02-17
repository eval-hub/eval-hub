package shared
// Assisted-by: claude code

import (
	"encoding/json"
	"fmt"

	"github.com/eval-hub/eval-hub/pkg/api"
)

// JobSpec is the JSON structure written to job.json for benchmark adapters to consume.
type JobSpec struct {
	JobID           string              `json:"id"`
	ProviderID      string              `json:"provider_id"`
	BenchmarkID     string              `json:"benchmark_id"`
	Model           api.ModelRef        `json:"model"`
	NumExamples     *int                `json:"num_examples,omitempty"`
	BenchmarkConfig map[string]any      `json:"benchmark_config"`
	ExperimentName  string              `json:"experiment_name,omitempty"`
	Tags            []api.ExperimentTag `json:"tags,omitempty"`
	CallbackURL     *string             `json:"callback_url"`
	Exports         *JobSpecExports     `json:"exports,omitempty"`
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
func BuildJobSpecJSON(
	evaluation *api.EvaluationJobResource,
	providerID string,
	benchmarkID string,
	callbackURL *string,
) (string, error) {
	benchmarkConfig, err := FindBenchmarkConfig(evaluation, benchmarkID)
	if err != nil {
		return "", err
	}
	benchmarkParams := CopyParams(benchmarkConfig.Parameters)
	numExamples := NumExamplesFromParameters(benchmarkParams)
	delete(benchmarkParams, "num_examples")

	spec := JobSpec{
		JobID:           evaluation.Resource.ID,
		ProviderID:      providerID,
		BenchmarkID:     benchmarkID,
		Model:           evaluation.Model,
		NumExamples:     numExamples,
		BenchmarkConfig: benchmarkParams,
		CallbackURL:     callbackURL,
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
	specJSON, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal job spec: %w", err)
	}
	return string(specJSON), nil
}

// FindBenchmarkConfig finds a benchmark config by ID within an evaluation.
func FindBenchmarkConfig(
	evaluation *api.EvaluationJobResource,
	benchmarkID string,
) (*api.BenchmarkConfig, error) {
	for i := range evaluation.Benchmarks {
		benchmark := &evaluation.Benchmarks[i]
		if benchmark.ID == benchmarkID {
			return benchmark, nil
		}
	}
	return nil, fmt.Errorf("benchmark config not found for %q", benchmarkID)
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
