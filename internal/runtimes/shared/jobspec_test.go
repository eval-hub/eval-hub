package shared

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/pkg/api"
)

func baseEvaluation() *api.EvaluationJobResource {
	return &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{ID: "job-1"},
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{
				URL:  "http://model.example",
				Name: "model-1",
			},
			Benchmarks: []api.BenchmarkConfig{
				{
					Ref:        api.Ref{ID: "bench-1"},
					ProviderID: "provider-1",
					Parameters: map[string]any{
						"foo":          "bar",
						"num_examples": 5,
					},
				},
				{
					Ref:        api.Ref{ID: "bench-2"},
					ProviderID: "provider-2",
					Parameters: map[string]any{"baz": "qux"},
				},
			},
			Experiment: &api.ExperimentConfig{
				Name: "exp-1",
				Tags: []api.ExperimentTag{{Key: "env", Value: "test"}},
			},
		},
	}
}

// --- NumExamplesFromParameters ---

func TestNumExamplesFromParametersNilMap(t *testing.T) {
	result := NumExamplesFromParameters(nil)
	if result != nil {
		t.Fatalf("expected nil, got %d", *result)
	}
}

func TestNumExamplesFromParametersMissing(t *testing.T) {
	result := NumExamplesFromParameters(map[string]any{"foo": "bar"})
	if result != nil {
		t.Fatalf("expected nil, got %d", *result)
	}
}

func TestNumExamplesFromParametersInt(t *testing.T) {
	result := NumExamplesFromParameters(map[string]any{"num_examples": 10})
	if result == nil || *result != 10 {
		t.Fatalf("expected 10, got %v", result)
	}
}

func TestNumExamplesFromParametersInt32(t *testing.T) {
	result := NumExamplesFromParameters(map[string]any{"num_examples": int32(20)})
	if result == nil || *result != 20 {
		t.Fatalf("expected 20, got %v", result)
	}
}

func TestNumExamplesFromParametersInt64(t *testing.T) {
	result := NumExamplesFromParameters(map[string]any{"num_examples": int64(30)})
	if result == nil || *result != 30 {
		t.Fatalf("expected 30, got %v", result)
	}
}

func TestNumExamplesFromParametersFloat64(t *testing.T) {
	result := NumExamplesFromParameters(map[string]any{"num_examples": float64(42)})
	if result == nil || *result != 42 {
		t.Fatalf("expected 42, got %v", result)
	}
}

func TestNumExamplesFromParametersUnsupportedType(t *testing.T) {
	result := NumExamplesFromParameters(map[string]any{"num_examples": "not-a-number"})
	if result != nil {
		t.Fatalf("expected nil for string type, got %d", *result)
	}
}

// --- CopyParams ---

func TestCopyParamsNil(t *testing.T) {
	result := CopyParams(nil)
	if result == nil {
		t.Fatal("expected non-nil empty map, got nil")
	}
	if len(result) != 0 {
		t.Fatalf("expected empty map, got %v", result)
	}
}

func TestCopyParamsEmpty(t *testing.T) {
	result := CopyParams(map[string]any{})
	if result == nil {
		t.Fatal("expected non-nil empty map, got nil")
	}
	if len(result) != 0 {
		t.Fatalf("expected empty map, got %v", result)
	}
}

func TestCopyParamsShallowCopy(t *testing.T) {
	source := map[string]any{"a": 1, "b": "two"}
	result := CopyParams(source)

	if len(result) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result))
	}
	if result["a"] != 1 || result["b"] != "two" {
		t.Fatalf("expected copied values, got %v", result)
	}

	// Mutating the copy should not affect the source
	result["c"] = "three"
	if _, exists := source["c"]; exists {
		t.Fatal("mutating copy affected the source map")
	}
}

// --- FindBenchmarkConfig ---

func TestFindBenchmarkConfigFound(t *testing.T) {
	eval := baseEvaluation()
	config, err := FindBenchmarkConfig(eval, "bench-1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if config.ID != "bench-1" {
		t.Fatalf("expected bench-1, got %s", config.ID)
	}
	if config.ProviderID != "provider-1" {
		t.Fatalf("expected provider-1, got %s", config.ProviderID)
	}
}

func TestFindBenchmarkConfigSecondBenchmark(t *testing.T) {
	eval := baseEvaluation()
	config, err := FindBenchmarkConfig(eval, "bench-2")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if config.ID != "bench-2" {
		t.Fatalf("expected bench-2, got %s", config.ID)
	}
}

func TestFindBenchmarkConfigNotFound(t *testing.T) {
	eval := baseEvaluation()
	_, err := FindBenchmarkConfig(eval, "nonexistent")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "benchmark config not found") {
		t.Fatalf("expected 'benchmark config not found', got %q", err.Error())
	}
}

func TestFindBenchmarkConfigNoBenchmarks(t *testing.T) {
	eval := baseEvaluation()
	eval.Benchmarks = nil
	_, err := FindBenchmarkConfig(eval, "bench-1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// --- BuildJobSpecJSON ---

func TestBuildJobSpecJSONHappyPath(t *testing.T) {
	eval := baseEvaluation()
	callbackURL := "http://callback.example/status"

	result, err := BuildJobSpecJSON(eval, "provider-1", "bench-1", 0, &callbackURL)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	var spec JobSpec
	if err := json.Unmarshal([]byte(result), &spec); err != nil {
		t.Fatalf("expected valid JSON, got %v", err)
	}

	if spec.JobID != "job-1" {
		t.Fatalf("expected JobID %q, got %q", "job-1", spec.JobID)
	}
	if spec.ProviderID != "provider-1" {
		t.Fatalf("expected ProviderID %q, got %q", "provider-1", spec.ProviderID)
	}
	if spec.BenchmarkID != "bench-1" {
		t.Fatalf("expected BenchmarkID %q, got %q", "bench-1", spec.BenchmarkID)
	}
	if spec.Model.Name != "model-1" {
		t.Fatalf("expected Model.Name %q, got %q", "model-1", spec.Model.Name)
	}
	if spec.Model.URL != "http://model.example" {
		t.Fatalf("expected Model.URL %q, got %q", "http://model.example", spec.Model.URL)
	}
	if spec.NumExamples == nil || *spec.NumExamples != 5 {
		t.Fatalf("expected NumExamples 5, got %v", spec.NumExamples)
	}
	// num_examples should be stripped from BenchmarkConfig
	if _, exists := spec.BenchmarkConfig["num_examples"]; exists {
		t.Fatal("expected num_examples to be removed from BenchmarkConfig")
	}
	if spec.BenchmarkConfig["foo"] != "bar" {
		t.Fatalf("expected BenchmarkConfig[foo]=%q, got %q", "bar", spec.BenchmarkConfig["foo"])
	}
	if spec.ExperimentName != "exp-1" {
		t.Fatalf("expected ExperimentName %q, got %q", "exp-1", spec.ExperimentName)
	}
	if len(spec.Tags) != 1 || spec.Tags[0].Key != "env" || spec.Tags[0].Value != "test" {
		t.Fatalf("expected 1 tag with key=env value=test, got %v", spec.Tags)
	}
	if spec.CallbackURL == nil || *spec.CallbackURL != callbackURL {
		t.Fatalf("expected CallbackURL %q, got %v", callbackURL, spec.CallbackURL)
	}
}

func TestBuildJobSpecJSONNilCallbackURL(t *testing.T) {
	eval := baseEvaluation()

	result, err := BuildJobSpecJSON(eval, "provider-1", "bench-1", 0, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	var spec JobSpec
	if err := json.Unmarshal([]byte(result), &spec); err != nil {
		t.Fatalf("expected valid JSON, got %v", err)
	}

	if spec.CallbackURL != nil {
		t.Fatalf("expected nil CallbackURL, got %v", *spec.CallbackURL)
	}
}

func TestBuildJobSpecJSONNilExperiment(t *testing.T) {
	eval := baseEvaluation()
	eval.Experiment = nil

	result, err := BuildJobSpecJSON(eval, "provider-1", "bench-1", 0, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	var spec JobSpec
	if err := json.Unmarshal([]byte(result), &spec); err != nil {
		t.Fatalf("expected valid JSON, got %v", err)
	}

	if spec.ExperimentName != "" {
		t.Fatalf("expected empty ExperimentName, got %q", spec.ExperimentName)
	}
	if spec.Tags != nil {
		t.Fatalf("expected nil Tags, got %v", spec.Tags)
	}
}

func TestBuildJobSpecJSONNoNumExamples(t *testing.T) {
	eval := baseEvaluation()
	// Use bench-2 which has no num_examples
	result, err := BuildJobSpecJSON(eval, "provider-2", "bench-2", 0, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	var spec JobSpec
	if err := json.Unmarshal([]byte(result), &spec); err != nil {
		t.Fatalf("expected valid JSON, got %v", err)
	}

	if spec.NumExamples != nil {
		t.Fatalf("expected nil NumExamples, got %d", *spec.NumExamples)
	}
}

func TestBuildJobSpecJSONBenchmarkNotFound(t *testing.T) {
	eval := baseEvaluation()
	_, err := BuildJobSpecJSON(eval, "provider-1", "nonexistent", 0, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "benchmark config not found") {
		t.Fatalf("expected 'benchmark config not found', got %q", err.Error())
	}
}

func TestBuildJobSpecJSONDoesNotMutateOriginalParams(t *testing.T) {
	eval := baseEvaluation()
	originalParams := eval.Benchmarks[0].Parameters

	_, err := BuildJobSpecJSON(eval, "provider-1", "bench-1", 0, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// num_examples should still be in the original
	if _, exists := originalParams["num_examples"]; !exists {
		t.Fatal("BuildJobSpecJSON mutated the original parameters map")
	}
}
