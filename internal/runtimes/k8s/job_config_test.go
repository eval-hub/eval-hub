package k8s

import (
	"testing"

	"github.com/eval-hub/eval-hub/internal/runtimes/shared"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestBuildJobConfigDefaults(t *testing.T) {
	serviceURL := "http://eval-hub"
	t.Setenv(serviceURLEnv, serviceURL)
	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource:           api.Resource{ID: "job-123"},
			MLFlowExperimentID: "",
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{
				URL:  "http://model",
				Name: "model",
			},
			Benchmarks: []api.BenchmarkConfig{
				{
					Ref: api.Ref{ID: "bench-1"},
					Parameters: map[string]any{
						"num_examples": 50,
						"max_tokens":   128,
						"temperature":  0.2,
					},
				},
			},
		},
	}
	provider := &api.ProviderResource{
		Resource: api.Resource{ID: "provider-1"},
		ProviderConfigInternal: api.ProviderConfigInternal{
			Runtime: &api.Runtime{
				K8s: &api.K8sRuntime{
					Image: "adapter:latest",
				},
			},
		},
	}

	cfg, err := buildJobConfig(evaluation, provider, "bench-1", 0)
	if err != nil {
		t.Fatalf("buildJobConfig returned error: %v", err)
	}
	if cfg.jobID != "job-123" {
		t.Fatalf("expected job id to be set")
	}
	if cfg.adapterImage != "adapter:latest" {
		t.Fatalf("expected adapter image to be set")
	}
	if cfg.namespace == "" {
		t.Fatalf("expected namespace to be set")
	}
	if cfg.cpuRequest != defaultCPURequest {
		t.Fatalf("expected cpu request %s, got %s", defaultCPURequest, cfg.cpuRequest)
	}
	if cfg.memoryRequest != defaultMemoryRequest {
		t.Fatalf("expected memory request %s, got %s", defaultMemoryRequest, cfg.memoryRequest)
	}
	if cfg.cpuLimit != defaultCPULimit {
		t.Fatalf("expected cpu limit %s, got %s", defaultCPULimit, cfg.cpuLimit)
	}
	if cfg.memoryLimit != defaultMemoryLimit {
		t.Fatalf("expected memory limit %s, got %s", defaultMemoryLimit, cfg.memoryLimit)
	}

	spec := cfg.jobSpec
	jobID := spec.JobID
	if jobID != "job-123" {
		t.Fatalf("expected job spec json id to be %q, got %v", "job-123", jobID)
	}
	benchmarkID := spec.BenchmarkID
	if benchmarkID != "bench-1" {
		t.Fatalf("expected job spec json benchmark_id to be %q, got %v", "bench-1", benchmarkID)
	}
	numExamples := spec.NumExamples
	if numExamples == nil || *numExamples != 50 {
		t.Fatalf("expected job spec json num_examples to be %d, got %v", 50, numExamples)
	}
	benchmarkConfig := spec.BenchmarkConfig

	if _, exists := benchmarkConfig["num_examples"]; exists {
		t.Fatalf("expected benchmark_config not to include num_examples")
	}
	if benchmarkConfig["max_tokens"] != 128 {
		t.Fatalf("expected benchmark_config.max_tokens to be %d, got %v", 128, benchmarkConfig["max_tokens"])
	}
	if benchmarkConfig["temperature"] != 0.2 {
		t.Fatalf("expected benchmark_config.temperature to be 0.2, got %v", benchmarkConfig["temperature"])
	}
	callback := spec.CallbackURL
	if callback == nil || *callback != serviceURL {
		t.Fatalf("expected job spec json callback_url to be %q, got %v", serviceURL, callback)
	}
}

func TestBuildJobConfigAllowsNumExamplesOnly(t *testing.T) {
	t.Setenv(serviceURLEnv, "http://eval-hub")
	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource:           api.Resource{ID: "job-456"},
			MLFlowExperimentID: "",
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{
				URL:  "http://model",
				Name: "model",
			},
			Benchmarks: []api.BenchmarkConfig{
				{
					Ref:        api.Ref{ID: "bench-1"},
					Parameters: map[string]any{"num_examples": 10},
				},
			},
		},
	}
	provider := &api.ProviderResource{
		Resource: api.Resource{ID: "provider-1"},
		ProviderConfigInternal: api.ProviderConfigInternal{
			Runtime: &api.Runtime{
				K8s: &api.K8sRuntime{
					Image: "adapter:latest",
				},
			},
		},
	}

	cfg, err := buildJobConfig(evaluation, provider, "bench-1", 0)
	if err != nil {
		t.Fatalf("expected no error for num_examples-only benchmark_config, got %v", err)
	}

	spec := cfg.jobSpec
	numExamples := spec.NumExamples
	if numExamples == nil || *numExamples != 10 {
		t.Fatalf("expected job spec json num_examples to be %d, got %v", 10, numExamples)
	}

	benchmarkConfig := spec.BenchmarkConfig

	if len(benchmarkConfig) != 0 {
		t.Fatalf("expected empty benchmark_config, got %v", benchmarkConfig)
	}
}

func TestBuildJobConfigMissingRuntime(t *testing.T) {
	t.Setenv(serviceURLEnv, "http://eval-hub")
	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource:           api.Resource{ID: "job-123"},
			MLFlowExperimentID: "",
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{
				URL:  "http://model",
				Name: "model",
			},
		},
	}
	provider := &api.ProviderResource{
		Resource: api.Resource{ID: "provider-1"},
		ProviderConfigInternal: api.ProviderConfigInternal{
			Runtime: &api.Runtime{},
		},
	}

	_, err := buildJobConfig(evaluation, provider, "bench-1", 0)
	if err == nil {
		t.Fatalf("expected error for missing runtime")
	}
}

func TestBuildJobConfigMissingAdapterImage(t *testing.T) {
	t.Setenv(serviceURLEnv, "http://eval-hub")
	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource:           api.Resource{ID: "job-123"},
			MLFlowExperimentID: "",
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{
				URL:  "http://model",
				Name: "model",
			},
		},
	}
	provider := &api.ProviderResource{
		Resource: api.Resource{ID: "provider-1"},
		ProviderConfigInternal: api.ProviderConfigInternal{
			Runtime: &api.Runtime{},
		},
	}

	_, err := buildJobConfig(evaluation, provider, "bench-1", 0)
	if err == nil {
		t.Fatalf("expected error for missing adapter image")
	}
}

func TestBuildJobConfigMissingServiceURL(t *testing.T) {
	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource:           api.Resource{ID: "job-123"},
			MLFlowExperimentID: "",
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{
				URL:  "http://model",
				Name: "model",
			},
			Benchmarks: []api.BenchmarkConfig{
				{
					Ref:        api.Ref{ID: "bench-1"},
					Parameters: map[string]any{"num_examples": 50},
				},
			},
		},
	}
	provider := &api.ProviderResource{
		Resource: api.Resource{ID: "provider-1"},
		ProviderConfigInternal: api.ProviderConfigInternal{
			Runtime: &api.Runtime{
				K8s: &api.K8sRuntime{
					Image: "adapter:latest",
				},
			},
		},
	}

	_, err := buildJobConfig(evaluation, provider, "bench-1", 0)
	if err == nil {
		t.Fatalf("expected error for missing %s", serviceURLEnv)
	}
}

func TestBuildJobConfigAllowsEmptyBenchmarkConfig(t *testing.T) {
	t.Setenv(serviceURLEnv, "http://eval-hub")
	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource:           api.Resource{ID: "job-123"},
			MLFlowExperimentID: "",
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{
				URL:  "http://model",
				Name: "model",
			},
			Benchmarks: []api.BenchmarkConfig{
				{
					Ref: api.Ref{ID: "bench-1"},
				},
			},
		},
	}
	provider := &api.ProviderResource{
		Resource: api.Resource{ID: "provider-1"},
		ProviderConfigInternal: api.ProviderConfigInternal{
			Runtime: &api.Runtime{
				K8s: &api.K8sRuntime{
					Image: "adapter:latest",
				},
			},
		},
	}

	cfg, err := buildJobConfig(evaluation, provider, "bench-1", 0)
	if err != nil {
		t.Fatalf("expected no error for empty benchmark_config, got %v", err)
	}

	spec := cfg.jobSpec
	benchmarkConfig := spec.BenchmarkConfig

	if len(benchmarkConfig) != 0 {
		t.Fatalf("expected empty benchmark_config, got %v", benchmarkConfig)
	}
}

func TestBuildJobConfigWithOCIExports(t *testing.T) {
	t.Setenv(serviceURLEnv, "http://eval-hub")
	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{ID: "job-oci"},
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{
				URL:  "http://model",
				Name: "model",
			},
			Benchmarks: []api.BenchmarkConfig{
				{
					Ref:        api.Ref{ID: "bench-1"},
					Parameters: map[string]any{},
				},
			},
			Exports: &api.EvaluationExports{
				OCI: &api.EvaluationExportsOCI{
					Coordinates: api.OCICoordinates{
						OCIHost:       "quay.io",
						OCIRepository: "my-org/my-repo",
						OCITag:        "eval-123",
					},
					K8s: &api.OCIConnectionConfig{
						Connection: "my-pull-secret",
					},
				},
			},
		},
	}
	provider := &api.ProviderResource{
		Resource: api.Resource{ID: "provider-1"},
		ProviderConfigInternal: api.ProviderConfigInternal{
			Runtime: &api.Runtime{
				K8s: &api.K8sRuntime{
					Image: "adapter:latest",
				},
			},
		},
	}

	cfg, err := buildJobConfig(evaluation, provider, "bench-1", 0)
	if err != nil {
		t.Fatalf("buildJobConfig returned error: %v", err)
	}

	// ociCredentialsSecret should be extracted from k8s.connection
	if cfg.ociCredentialsSecret != "my-pull-secret" {
		t.Fatalf("expected ociCredentialsSecret %q, got %q", "my-pull-secret", cfg.ociCredentialsSecret)
	}

	// jobSpecJSON should contain coordinates but NOT k8s connection

	spec := cfg.jobSpec
	exports := spec.Exports
	if exports == nil {
		t.Fatalf("expected exports object, got %v", exports)
	}
	oci := exports.OCI
	if oci == nil {
		t.Fatalf("expected exports.oci, got %v", oci)
	}
	coords := oci.Coordinates

	if coords.OCIHost != "quay.io" {
		t.Fatalf("expected oci_host %q, got %v", "quay.io", coords.OCIHost)
	}
	if coords.OCIRepository != "my-org/my-repo" {
		t.Fatalf("expected oci_repository %q, got %v", "my-org/my-repo", coords.OCIRepository)
	}

}

func TestNumExamplesFromParametersTypes(t *testing.T) {
	tests := []struct {
		name       string
		parameters map[string]any
		want       *int
	}{
		{"nil map", nil, nil},
		{"missing", map[string]any{"other": 1}, nil},
		{"int", map[string]any{"num_examples": 3}, intPtr(3)},
		{"int32", map[string]any{"num_examples": int32(4)}, intPtr(4)},
		{"int64", map[string]any{"num_examples": int64(5)}, intPtr(5)},
		{"float64", map[string]any{"num_examples": float64(6)}, intPtr(6)},
		{"invalid", map[string]any{"num_examples": "bad"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shared.NumExamplesFromParameters(tt.parameters)
			if tt.want == nil && got != nil {
				t.Fatalf("expected nil, got %v", *got)
			}
			if tt.want != nil && (got == nil || *got != *tt.want) {
				if got == nil {
					t.Fatalf("expected %d, got nil", *tt.want)
				}
				t.Fatalf("expected %d, got %d", *tt.want, *got)
			}
		})
	}
}

func TestCopyParamsCreatesCopy(t *testing.T) {
	original := map[string]any{"num_examples": 1, "temp": 0.2}
	copied := shared.CopyParams(original)
	if len(copied) != len(original) {
		t.Fatalf("expected copy size %d, got %d", len(original), len(copied))
	}
	copied["temp"] = 0.3
	if original["temp"] == copied["temp"] {
		t.Fatalf("expected copy to be independent of original")
	}
}

func intPtr(value int) *int {
	return &value
}
