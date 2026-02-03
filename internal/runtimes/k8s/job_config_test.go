package k8s

import (
	"encoding/json"
	"testing"

	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestBuildJobConfigDefaults(t *testing.T) {
	retry := 2
	serviceURL := "http://eval-hub"
	t.Setenv(serviceURLEnv, serviceURL)
	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource:           api.Resource{ID: "job-123"},
			MLFlowExperimentID: nil,
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{
				URL:  "http://model",
				Name: "model",
			},
			RetryAttempts: &retry,
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
		ProviderID: "provider-1",
		Runtime: &api.Runtime{
			K8s: &api.K8sRuntime{
				Image: "adapter:latest",
			},
		},
	}

	cfg, err := buildJobConfig(evaluation, provider, "bench-1")
	if err != nil {
		t.Fatalf("buildJobConfig returned error: %v", err)
	}
	if cfg.jobID != "job-123" {
		t.Fatalf("expected job id to be set")
	}
	if cfg.adapterImage != "adapter:latest" {
		t.Fatalf("expected adapter image to be set")
	}
	if cfg.retryAttempts != retry {
		t.Fatalf("expected retry attempts %d, got %d", retry, cfg.retryAttempts)
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
	var decoded map[string]any
	if err := json.Unmarshal([]byte(cfg.jobSpecJSON), &decoded); err != nil {
		t.Fatalf("unmarshal job spec json: %v", err)
	}
	jobID, ok := decoded["job_id"].(string)
	if !ok || jobID != "job-123" {
		t.Fatalf("expected job spec json job_id to be %q, got %v", "job-123", decoded["job_id"])
	}
	benchmarkID, ok := decoded["benchmark_id"].(string)
	if !ok || benchmarkID != "bench-1" {
		t.Fatalf("expected job spec json benchmark_id to be %q, got %v", "bench-1", decoded["benchmark_id"])
	}
	if numExamples, ok := decoded["num_examples"].(float64); !ok || int(numExamples) != 50 {
		t.Fatalf("expected job spec json num_examples to be %d, got %v", 50, decoded["num_examples"])
	}
	benchmarkConfig, ok := decoded["benchmark_config"].(map[string]any)
	if !ok {
		t.Fatalf("expected job spec json benchmark_config to be a map, got %T", decoded["benchmark_config"])
	}
	if _, exists := benchmarkConfig["num_examples"]; exists {
		t.Fatalf("expected benchmark_config not to include num_examples")
	}
	if benchmarkConfig["max_tokens"] != float64(128) {
		t.Fatalf("expected benchmark_config.max_tokens to be %d, got %v", 128, benchmarkConfig["max_tokens"])
	}
	if benchmarkConfig["temperature"] != 0.2 {
		t.Fatalf("expected benchmark_config.temperature to be 0.2, got %v", benchmarkConfig["temperature"])
	}
	if callback, ok := decoded["callback_url"].(string); !ok || callback != serviceURL {
		t.Fatalf("expected job spec json callback_url to be %q, got %v", serviceURL, decoded["callback_url"])
	}
}

func TestBuildJobConfigMissingRuntime(t *testing.T) {
	t.Setenv(serviceURLEnv, "http://eval-hub")
	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource:           api.Resource{ID: "job-123"},
			MLFlowExperimentID: nil,
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{
				URL:  "http://model",
				Name: "model",
			},
		},
	}
	provider := &api.ProviderResource{
		ProviderID: "provider-1",
	}

	_, err := buildJobConfig(evaluation, provider, "bench-1")
	if err == nil {
		t.Fatalf("expected error for missing runtime")
	}
}

func TestBuildJobConfigMissingAdapterImage(t *testing.T) {
	t.Setenv(serviceURLEnv, "http://eval-hub")
	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource:           api.Resource{ID: "job-123"},
			MLFlowExperimentID: nil,
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{
				URL:  "http://model",
				Name: "model",
			},
		},
	}
	provider := &api.ProviderResource{
		ProviderID: "provider-1",
		Runtime:    &api.Runtime{},
	}

	_, err := buildJobConfig(evaluation, provider, "bench-1")
	if err == nil {
		t.Fatalf("expected error for missing adapter image")
	}
}

func TestBuildJobConfigMissingServiceURL(t *testing.T) {
	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource:           api.Resource{ID: "job-123"},
			MLFlowExperimentID: nil,
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
		ProviderID: "provider-1",
		Runtime: &api.Runtime{
			K8s: &api.K8sRuntime{
				Image: "adapter:latest",
			},
		},
	}

	_, err := buildJobConfig(evaluation, provider, "bench-1")
	if err == nil {
		t.Fatalf("expected error for missing %s", serviceURLEnv)
	}
}

func TestBuildJobConfigMissingBenchmarkConfig(t *testing.T) {
	t.Setenv(serviceURLEnv, "http://eval-hub")
	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource:           api.Resource{ID: "job-123"},
			MLFlowExperimentID: nil,
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
		ProviderID: "provider-1",
		Runtime: &api.Runtime{
			K8s: &api.K8sRuntime{
				Image: "adapter:latest",
			},
		},
	}

	_, err := buildJobConfig(evaluation, provider, "bench-1")
	if err == nil {
		t.Fatalf("expected error for missing benchmark_config")
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
			got := numExamplesFromParameters(tt.parameters)
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
	copied := copyParams(original)
	if len(copied) != len(original) {
		t.Fatalf("expected copy size %d, got %d", len(original), len(copied))
	}
	copied["temp"] = 0.3
	if original["temp"] == copied["temp"] {
		t.Fatalf("expected copy to be independent of original")
	}
}

func TestTimeoutSecondsFromMinutes(t *testing.T) {
	if timeoutSecondsFromMinutes(nil) != nil {
		t.Fatalf("expected nil for nil minutes")
	}
	minutes := 2
	seconds := timeoutSecondsFromMinutes(&minutes)
	if seconds == nil || *seconds != 120 {
		if seconds == nil {
			t.Fatalf("expected 120, got nil")
		}
		t.Fatalf("expected 120, got %d", *seconds)
	}
}

func intPtr(value int) *int {
	return &value
}
