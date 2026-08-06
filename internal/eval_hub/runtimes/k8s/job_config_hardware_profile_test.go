package k8s

import (
	"testing"

	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestBuildJobConfigHardwareProfileOverridesProvider(t *testing.T) {
	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-hwp"}},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: &api.ModelRef{URL: "http://model", Name: "model"},
			Benchmarks: []api.EvaluationBenchmarkConfig{
				{Ref: api.Ref{ID: "bench-1"}, ProviderID: "provider-1"},
			},
		},
	}
	provider := &api.ProviderResource{
		Resource: api.Resource{ID: "provider-1"},
		ProviderConfig: api.ProviderConfig{
			Runtime: &api.Runtime{
				K8s: &api.K8sRuntime{
					Image:         "adapter:latest",
					CPURequest:    "100m",
					MemoryRequest: "128Mi",
					CPULimit:      "500m",
					MemoryLimit:   "512Mi",
					GPU: &api.GPUConfig{
						Resource: "nvidia.com/gpu",
						Count:    2,
					},
				},
			},
		},
	}
	profile := &hardwareProfileResources{
		cpuRequest:    "4",
		cpuLimit:      "8",
		memoryRequest: "2Gi",
		gpuResource:   "amd.com/gpu",
		gpuCount:      1,
	}

	cfg, err := buildJobConfig(evaluation, provider, &evaluation.Benchmarks[0], 0, nil, profile)
	if err != nil {
		t.Fatalf("buildJobConfig returned error: %v", err)
	}
	if cfg.cpuRequest != "4" {
		t.Fatalf("cpuRequest = %q, want 4", cfg.cpuRequest)
	}
	if cfg.cpuLimit != "8" {
		t.Fatalf("cpuLimit = %q, want 8", cfg.cpuLimit)
	}
	if cfg.memoryRequest != "2Gi" {
		t.Fatalf("memoryRequest = %q, want 2Gi", cfg.memoryRequest)
	}
	if cfg.memoryLimit != "512Mi" {
		t.Fatalf("memoryLimit = %q, want provider fallback 512Mi", cfg.memoryLimit)
	}
	if cfg.gpuResource != "amd.com/gpu" {
		t.Fatalf("gpuResource = %q, want amd.com/gpu", cfg.gpuResource)
	}
	if cfg.gpuCount != 1 {
		t.Fatalf("gpuCount = %d, want 1", cfg.gpuCount)
	}
}

func TestBuildJobConfigWithoutHardwareProfileUnchanged(t *testing.T) {
	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-no-hwp"}},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: &api.ModelRef{URL: "http://model", Name: "model"},
			Benchmarks: []api.EvaluationBenchmarkConfig{
				{Ref: api.Ref{ID: "bench-1"}, ProviderID: "provider-1"},
			},
		},
	}
	provider := &api.ProviderResource{
		Resource: api.Resource{ID: "provider-1"},
		ProviderConfig: api.ProviderConfig{
			Runtime: &api.Runtime{
				K8s: &api.K8sRuntime{
					Image:         "adapter:latest",
					CPURequest:    "100m",
					MemoryRequest: "128Mi",
				},
			},
		},
	}

	cfg, err := buildJobConfig(evaluation, provider, &evaluation.Benchmarks[0], 0, nil, nil)
	if err != nil {
		t.Fatalf("buildJobConfig returned error: %v", err)
	}
	if cfg.cpuRequest != "100m" || cfg.memoryRequest != "128Mi" {
		t.Fatalf("expected provider resources unchanged, got cpu=%q memory=%q", cfg.cpuRequest, cfg.memoryRequest)
	}
}

func TestBuildJobConfigDirectHardwareConfigOverridesProvider(t *testing.T) {
	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-direct-hw"}},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: &api.ModelRef{URL: "http://model", Name: "model"},
			Benchmarks: []api.EvaluationBenchmarkConfig{
				{
					Ref:        api.Ref{ID: "bench-1"},
					ProviderID: "provider-1",
					HardwareConfig: &api.BenchmarkHardwareConfig{
						Queue:  &api.QueueConfig{Kind: "kueue", Name: "my-queue"},
						CPU:    &api.HardwareResourceQuantity{Request: "1", Limit: "2"},
						Memory: &api.HardwareResourceQuantity{Request: "1Gi", Limit: "2Gi"},
						GPU:    &api.HardwareGPUConfig{Name: "nvidia.com/gpu", Count: 1},
					},
				},
			},
		},
	}
	provider := &api.ProviderResource{
		Resource: api.Resource{ID: "provider-1"},
		ProviderConfig: api.ProviderConfig{
			Runtime: &api.Runtime{
				K8s: &api.K8sRuntime{
					Image:         "adapter:latest",
					CPURequest:    "100m",
					MemoryRequest: "128Mi",
					CPULimit:      "500m",
					MemoryLimit:   "512Mi",
					GPU: &api.GPUConfig{
						Resource: "amd.com/gpu",
						Count:    4,
						NodeSelector: map[string]string{
							"accelerator": "amd",
						},
					},
				},
			},
		},
	}

	cfg, err := buildJobConfig(evaluation, provider, &evaluation.Benchmarks[0], 0, nil, nil)
	if err != nil {
		t.Fatalf("buildJobConfig returned error: %v", err)
	}
	if cfg.cpuRequest != "1" || cfg.cpuLimit != "2" {
		t.Fatalf("cpu = %q/%q, want 1/2", cfg.cpuRequest, cfg.cpuLimit)
	}
	if cfg.memoryRequest != "1Gi" || cfg.memoryLimit != "2Gi" {
		t.Fatalf("memory = %q/%q, want 1Gi/2Gi", cfg.memoryRequest, cfg.memoryLimit)
	}
	if cfg.gpuResource != "nvidia.com/gpu" || cfg.gpuCount != 1 {
		t.Fatalf("gpu = %q/%d, want nvidia.com/gpu/1", cfg.gpuResource, cfg.gpuCount)
	}
	if cfg.queueKind != "kueue" || cfg.queueName != "my-queue" {
		t.Fatalf("queue = %q/%q, want kueue/my-queue", cfg.queueKind, cfg.queueName)
	}
	if cfg.nodeSelector != nil {
		t.Fatalf("expected nodeSelector cleared when queue set, got %#v", cfg.nodeSelector)
	}
}

func TestBuildJobConfigDirectHardwareConfigPartialOverride(t *testing.T) {
	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-partial-hw"}},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: &api.ModelRef{URL: "http://model", Name: "model"},
			Benchmarks: []api.EvaluationBenchmarkConfig{
				{
					Ref:        api.Ref{ID: "bench-1"},
					ProviderID: "provider-1",
					HardwareConfig: &api.BenchmarkHardwareConfig{
						CPU: &api.HardwareResourceQuantity{Request: "2"},
					},
				},
			},
		},
	}
	provider := &api.ProviderResource{
		Resource: api.Resource{ID: "provider-1"},
		ProviderConfig: api.ProviderConfig{
			Runtime: &api.Runtime{
				K8s: &api.K8sRuntime{
					Image:         "adapter:latest",
					CPURequest:    "100m",
					MemoryRequest: "128Mi",
					CPULimit:      "500m",
					MemoryLimit:   "512Mi",
				},
			},
		},
	}

	cfg, err := buildJobConfig(evaluation, provider, &evaluation.Benchmarks[0], 0, nil, nil)
	if err != nil {
		t.Fatalf("buildJobConfig returned error: %v", err)
	}
	if cfg.cpuRequest != "2" {
		t.Fatalf("cpuRequest = %q, want 2", cfg.cpuRequest)
	}
	if cfg.cpuLimit != "500m" || cfg.memoryRequest != "128Mi" || cfg.memoryLimit != "512Mi" {
		t.Fatalf("expected provider fallbacks, got cpuLimit=%q memory=%q/%q", cfg.cpuLimit, cfg.memoryRequest, cfg.memoryLimit)
	}
}

func TestApplyDirectHardwareConfigInvalidGPUCount(t *testing.T) {
	cfg := &jobConfig{}
	err := applyDirectHardwareConfig(cfg, &api.BenchmarkHardwareConfig{
		GPU: &api.HardwareGPUConfig{Name: "nvidia.com/gpu", Count: -1},
	})
	if err == nil {
		t.Fatal("expected error for negative count")
	}
}
