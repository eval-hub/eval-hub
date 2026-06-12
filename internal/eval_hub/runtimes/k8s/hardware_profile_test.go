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

func TestResolveHardwareProfileNamespace(t *testing.T) {
	if got := resolveHardwareProfileNamespace("custom-ns", "tenant-ns"); got != "custom-ns" {
		t.Fatalf("namespace = %q, want custom-ns", got)
	}
	if got := resolveHardwareProfileNamespace("", "my-tenant"); got != "my-tenant" {
		t.Fatalf("empty namespace with tenant = %q, want my-tenant", got)
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

func TestParseHardwareProfileResourcesErrorsAndEdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("nil profile", func(t *testing.T) {
		t.Parallel()
		if _, err := parseHardwareProfileResources(nil); err == nil {
			t.Fatal("expected error for nil profile")
		}
	})

	t.Run("empty identifiers", func(t *testing.T) {
		t.Parallel()
		got, err := parseHardwareProfileResources(&unstructured.Unstructured{
			Object: map[string]any{"spec": map[string]any{"identifiers": []any{}}},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.cpuRequest != "" || got.gpuCount != 0 {
			t.Fatalf("expected empty resources, got %+v", got)
		}
	})

	t.Run("invalid identifiers type", func(t *testing.T) {
		t.Parallel()
		_, err := parseHardwareProfileResources(&unstructured.Unstructured{
			Object: map[string]any{"spec": map[string]any{"identifiers": "bad"}},
		})
		if err == nil {
			t.Fatal("expected error for invalid identifiers type")
		}
	})

	t.Run("invalid accelerator count", func(t *testing.T) {
		t.Parallel()
		_, err := parseHardwareProfileResources(&unstructured.Unstructured{
			Object: map[string]any{
				"spec": map[string]any{
					"identifiers": []any{
						map[string]any{
							"identifier":   "nvidia.com/gpu",
							"resourceType": "Accelerator",
							"defaultCount": "not-a-number",
						},
					},
				},
			},
		})
		if err == nil {
			t.Fatal("expected error for invalid accelerator count")
		}
	})

	t.Run("skips non-map identifier entries", func(t *testing.T) {
		t.Parallel()
		got, err := parseHardwareProfileResources(&unstructured.Unstructured{
			Object: map[string]any{
				"spec": map[string]any{
					"identifiers": []any{
						"ignored",
						map[string]any{
							"resourceType": "CPU",
							"defaultCount": int64(2),
						},
					},
				},
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.cpuRequest != "2" {
			t.Fatalf("cpuRequest = %q, want 2", got.cpuRequest)
		}
	})

	t.Run("standard resources are not treated as accelerators", func(t *testing.T) {
		t.Parallel()
		got, err := parseHardwareProfileResources(&unstructured.Unstructured{
			Object: map[string]any{
				"spec": map[string]any{
					"identifiers": []any{
						map[string]any{
							"identifier":   "ephemeral-storage",
							"defaultCount": int64(1),
						},
					},
				},
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.gpuResource != "" || got.gpuCount != 0 {
			t.Fatalf("expected no accelerator, got resource=%q count=%d", got.gpuResource, got.gpuCount)
		}
	})
}

func TestResolveHardwareProfileNamespaceTrimsWhitespace(t *testing.T) {
	t.Parallel()
	if got := resolveHardwareProfileNamespace("  custom-ns  ", "tenant-ns"); got != "custom-ns" {
		t.Fatalf("namespace = %q, want custom-ns", got)
	}
}

func TestApplyHardwareProfileResourcesNilGuards(t *testing.T) {
	t.Parallel()
	cfg := &jobConfig{cpuRequest: "100m"}
	applyHardwareProfileResources(nil, &hardwareProfileResources{cpuRequest: "4"})
	applyHardwareProfileResources(cfg, nil)
	if cfg.cpuRequest != "100m" {
		t.Fatalf("cpuRequest = %q, want unchanged 100m", cfg.cpuRequest)
	}
}

func TestApplyHardwareProfileResourcesFullOverlay(t *testing.T) {
	t.Parallel()
	cfg := &jobConfig{
		cpuRequest:    "100m",
		memoryRequest: "128Mi",
		cpuLimit:      "500m",
		memoryLimit:   "512Mi",
		gpuResource:   "nvidia.com/gpu",
		gpuCount:      2,
	}
	profile := &hardwareProfileResources{
		cpuRequest:    "4",
		cpuLimit:      "8",
		memoryRequest: "2Gi",
		memoryLimit:   "4Gi",
		gpuResource:   "amd.com/gpu",
		gpuCount:      1,
	}
	applyHardwareProfileResources(cfg, profile)
	if cfg.cpuRequest != "4" || cfg.cpuLimit != "8" ||
		cfg.memoryRequest != "2Gi" || cfg.memoryLimit != "4Gi" ||
		cfg.gpuResource != "amd.com/gpu" || cfg.gpuCount != 1 {
		t.Fatalf("unexpected overlay result: %+v", cfg)
	}
}

func TestIsStandardHardwareProfileResource(t *testing.T) {
	t.Parallel()
	if !isStandardHardwareProfileResource("cpu") {
		t.Fatal("cpu should be standard")
	}
	if isStandardHardwareProfileResource("nvidia.com/gpu") {
		t.Fatal("gpu should not be standard")
	}
}

func TestQuantityStringFromUnstructured(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in     any
		want   string
		wantOK bool
	}{
		{in: " 2Gi ", want: "2Gi", wantOK: true},
		{in: "", wantOK: false},
		{in: "   ", wantOK: false},
		{in: 4, want: "4", wantOK: true},
		{in: int32(2), want: "2", wantOK: true},
		{in: int64(8), want: "8", wantOK: true},
		{in: float64(3), want: "3", wantOK: true},
		{in: true, wantOK: false},
	}
	for _, tc := range cases {
		got, ok := quantityStringFromUnstructured(tc.in)
		if ok != tc.wantOK || got != tc.want {
			t.Fatalf("quantityStringFromUnstructured(%#v) = (%q, %v), want (%q, %v)", tc.in, got, ok, tc.want, tc.wantOK)
		}
	}
}

func TestStringFromUnstructured(t *testing.T) {
	t.Parallel()
	if got := stringFromUnstructured("profile"); got != "profile" {
		t.Fatalf("string = %q, want profile", got)
	}
	if got := stringFromUnstructured(42); got != "" {
		t.Fatalf("non-string = %q, want empty", got)
	}
}
