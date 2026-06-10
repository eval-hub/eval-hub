package k8s

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestParseHardwareProfileResources(t *testing.T) {
	profile := &unstructured.Unstructured{
		Object: map[string]any{
			"spec": map[string]any{
				"identifiers": []any{
					map[string]any{
						"identifier":   "cpu",
						"resourceType": "CPU",
						"defaultCount": int64(4),
						"maxCount":     int64(8),
					},
					map[string]any{
						"identifier":   "memory",
						"resourceType": "Memory",
						"defaultCount": "2Gi",
						"maxCount":     "4Gi",
					},
					map[string]any{
						"identifier":   "nvidia.com/gpu",
						"resourceType": "Accelerator",
						"defaultCount": int64(1),
					},
				},
			},
		},
	}

	got, err := parseHardwareProfileResources(profile)
	if err != nil {
		t.Fatalf("parseHardwareProfileResources returned error: %v", err)
	}
	if got.cpuRequest != "4" {
		t.Fatalf("cpuRequest = %q, want 4", got.cpuRequest)
	}
	if got.cpuLimit != "8" {
		t.Fatalf("cpuLimit = %q, want 8", got.cpuLimit)
	}
	if got.memoryRequest != "2Gi" {
		t.Fatalf("memoryRequest = %q, want 2Gi", got.memoryRequest)
	}
	if got.memoryLimit != "4Gi" {
		t.Fatalf("memoryLimit = %q, want 4Gi", got.memoryLimit)
	}
	if got.gpuResource != "nvidia.com/gpu" {
		t.Fatalf("gpuResource = %q, want nvidia.com/gpu", got.gpuResource)
	}
	if got.gpuCount != 1 {
		t.Fatalf("gpuCount = %d, want 1", got.gpuCount)
	}
}

func TestApplyHardwareProfileResourcesPartialFallback(t *testing.T) {
	cfg := &jobConfig{
		cpuRequest:    "250m",
		memoryRequest: "512Mi",
		cpuLimit:      "1",
		memoryLimit:   "2Gi",
		gpuResource:   "nvidia.com/gpu",
		gpuCount:      2,
	}
	profile := &hardwareProfileResources{
		cpuRequest:    "4",
		memoryRequest: "2Gi",
	}

	applyHardwareProfileResources(cfg, profile)

	if cfg.cpuRequest != "4" {
		t.Fatalf("cpuRequest = %q, want 4", cfg.cpuRequest)
	}
	if cfg.memoryRequest != "2Gi" {
		t.Fatalf("memoryRequest = %q, want 2Gi", cfg.memoryRequest)
	}
	if cfg.cpuLimit != "1" {
		t.Fatalf("cpuLimit = %q, want provider fallback 1", cfg.cpuLimit)
	}
	if cfg.memoryLimit != "2Gi" {
		t.Fatalf("memoryLimit = %q, want provider fallback 2Gi", cfg.memoryLimit)
	}
	if cfg.gpuResource != "nvidia.com/gpu" || cfg.gpuCount != 2 {
		t.Fatalf("expected provider GPU fallback, got resource=%q count=%d", cfg.gpuResource, cfg.gpuCount)
	}
}
