package k8s

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
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

func TestParseHardwareProfileNodeScheduling(t *testing.T) {
	t.Parallel()
	profile := &unstructured.Unstructured{
		Object: map[string]any{
			"spec": map[string]any{
				"scheduling": map[string]any{
					"type": "Node",
					"node": map[string]any{
						"nodeSelector": map[string]any{
							"node.kubernetes.io/instance-type": "g6.12xlarge",
						},
						"tolerations": []any{
							map[string]any{
								"effect":   "NoExecute",
								"key":      "kubernetes.io/hostname",
								"operator": "Equal",
								"value":    "ip-10-0-68-201.ec2.internal",
							},
						},
					},
				},
			},
		},
	}
	got, err := parseHardwareProfileResources(profile)
	if err != nil {
		t.Fatalf("parseHardwareProfileResources: %v", err)
	}
	if got.schedulingType != hardwareProfileSchedulingNode {
		t.Fatalf("schedulingType = %q, want Node", got.schedulingType)
	}
	if got.nodeSelector["node.kubernetes.io/instance-type"] != "g6.12xlarge" {
		t.Fatalf("nodeSelector = %v", got.nodeSelector)
	}
	if len(got.tolerations) != 1 {
		t.Fatalf("tolerations len = %d, want 1", len(got.tolerations))
	}
	if got.tolerations[0].Key != "kubernetes.io/hostname" || got.tolerations[0].Effect != corev1.TaintEffectNoExecute {
		t.Fatalf("toleration = %+v", got.tolerations[0])
	}
}

func TestParseHardwareProfileQueueScheduling(t *testing.T) {
	t.Parallel()
	profile := &unstructured.Unstructured{
		Object: map[string]any{
			"spec": map[string]any{
				"scheduling": map[string]any{
					"type": "Queue",
					"kueue": map[string]any{
						"localQueueName": "default",
						"priorityClass":  "high-priority",
					},
				},
			},
		},
	}
	got, err := parseHardwareProfileResources(profile)
	if err != nil {
		t.Fatalf("parseHardwareProfileResources: %v", err)
	}
	if got.schedulingType != hardwareProfileSchedulingQueue {
		t.Fatalf("schedulingType = %q, want Queue", got.schedulingType)
	}
	if got.queueName != "default" {
		t.Fatalf("queueName = %q, want default", got.queueName)
	}
	if got.priorityClassName != "high-priority" {
		t.Fatalf("priorityClassName = %q, want high-priority", got.priorityClassName)
	}
}

func TestParseHardwareProfileQueueSchedulingIgnoresNonePriority(t *testing.T) {
	t.Parallel()
	profile := &unstructured.Unstructured{
		Object: map[string]any{
			"spec": map[string]any{
				"scheduling": map[string]any{
					"type": "Queue",
					"kueue": map[string]any{
						"localQueueName": "default",
						"priorityClass":  "None",
					},
				},
			},
		},
	}
	got, err := parseHardwareProfileResources(profile)
	if err != nil {
		t.Fatalf("parseHardwareProfileResources: %v", err)
	}
	if got.priorityClassName != "" {
		t.Fatalf("priorityClassName = %q, want empty for None", got.priorityClassName)
	}
}

func TestIsHardwareProfileDisabled(t *testing.T) {
	t.Parallel()
	disabled := &unstructured.Unstructured{}
	disabled.SetAnnotations(map[string]string{hardwareProfileDisabledAnnotation: "true"})
	if !isHardwareProfileDisabled(disabled) {
		t.Fatal("expected disabled profile")
	}
	enabled := &unstructured.Unstructured{}
	enabled.SetAnnotations(map[string]string{hardwareProfileDisabledAnnotation: "false"})
	if isHardwareProfileDisabled(enabled) {
		t.Fatal("expected enabled profile")
	}
	if isHardwareProfileDisabled(&unstructured.Unstructured{}) {
		t.Fatal("expected enabled when annotation missing")
	}
}

func TestHardwareProfilesNamespace(t *testing.T) {
	t.Setenv(hardwareProfilesNamespaceEnv, "opendatahub")
	got, err := hardwareProfilesNamespace()
	if err != nil || got != "opendatahub" {
		t.Fatalf("namespace = %q err=%v, want opendatahub", got, err)
	}
}

func TestHardwareProfilesNamespaceRequiresEnv(t *testing.T) {
	t.Setenv(hardwareProfilesNamespaceEnv, "")
	_, err := hardwareProfilesNamespace()
	if err == nil {
		t.Fatal("expected error when env unset")
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

func TestApplyHardwareProfileNodeSchedulingOverridesProviderSelector(t *testing.T) {
	t.Parallel()
	cfg := &jobConfig{
		nodeSelector: map[string]string{"nvidia.com/gpu.product": "provider-gpu"},
	}
	profile := &hardwareProfileResources{
		schedulingType: hardwareProfileSchedulingNode,
		nodeSelector:   map[string]string{"node.kubernetes.io/instance-type": "g6.12xlarge"},
		tolerations: []corev1.Toleration{{
			Key:      "kubernetes.io/hostname",
			Operator: corev1.TolerationOpEqual,
			Value:    "node-a",
			Effect:   corev1.TaintEffectNoExecute,
		}},
	}
	applyHardwareProfileResources(cfg, profile)
	if cfg.nodeSelector["node.kubernetes.io/instance-type"] != "g6.12xlarge" {
		t.Fatalf("nodeSelector = %v", cfg.nodeSelector)
	}
	if len(cfg.tolerations) != 1 || cfg.tolerations[0].Value != "node-a" {
		t.Fatalf("tolerations = %+v", cfg.tolerations)
	}
}

func TestApplyHardwareProfileQueueSchedulingClearsNodeSelector(t *testing.T) {
	t.Parallel()
	cfg := &jobConfig{
		nodeSelector: map[string]string{"nvidia.com/gpu.product": "provider-gpu"},
		tolerations: []corev1.Toleration{{
			Key:      "nvidia.com/gpu",
			Operator: corev1.TolerationOpExists,
			Effect:   corev1.TaintEffectNoSchedule,
		}},
	}
	profile := &hardwareProfileResources{
		schedulingType:    hardwareProfileSchedulingQueue,
		queueName:         "gpu-queue",
		priorityClassName: "high-priority",
	}
	applyHardwareProfileResources(cfg, profile)
	if cfg.queueKind != "kueue" || cfg.queueName != "gpu-queue" {
		t.Fatalf("queue = %s/%s", cfg.queueKind, cfg.queueName)
	}
	if cfg.priorityClassName != "high-priority" {
		t.Fatalf("priorityClassName = %q", cfg.priorityClassName)
	}
	if len(cfg.nodeSelector) != 0 {
		t.Fatalf("expected nil nodeSelector, got %v", cfg.nodeSelector)
	}
	if len(cfg.tolerations) != 0 {
		t.Fatalf("expected nil tolerations, got %+v", cfg.tolerations)
	}
}

func TestApplyHardwareProfileQueueSchedulingWithoutQueuePreservesNodeSelector(t *testing.T) {
	t.Parallel()
	cfg := &jobConfig{
		nodeSelector: map[string]string{"nvidia.com/gpu.product": "provider-gpu"},
		tolerations: []corev1.Toleration{{
			Key:      "nvidia.com/gpu",
			Operator: corev1.TolerationOpExists,
			Effect:   corev1.TaintEffectNoSchedule,
		}},
	}
	profile := &hardwareProfileResources{
		schedulingType:    hardwareProfileSchedulingQueue,
		priorityClassName: "high-priority",
	}
	applyHardwareProfileResources(cfg, profile)
	if cfg.queueKind != "" || cfg.queueName != "" {
		t.Fatalf("expected empty queue, got %s/%s", cfg.queueKind, cfg.queueName)
	}
	if cfg.priorityClassName != "high-priority" {
		t.Fatalf("priorityClassName = %q", cfg.priorityClassName)
	}
	if cfg.nodeSelector["nvidia.com/gpu.product"] != "provider-gpu" {
		t.Fatalf("nodeSelector = %v, want provider placement preserved", cfg.nodeSelector)
	}
	if len(cfg.tolerations) != 1 {
		t.Fatalf("tolerations = %+v, want provider toleration preserved", cfg.tolerations)
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
		t.Fatalf("got %q", got)
	}
	if got := stringFromUnstructured(1); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestInt64PtrFromUnstructured(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   any
		want *int64
	}{
		{in: int64(7), want: int64Ptr(7)},
		{in: int32(3), want: int64Ptr(3)},
		{in: 5, want: int64Ptr(5)},
		{in: float64(9), want: int64Ptr(9)},
		{in: "nope", want: nil},
		{in: nil, want: nil},
	}
	for _, tc := range cases {
		got := int64PtrFromUnstructured(tc.in)
		if tc.want == nil {
			if got != nil {
				t.Fatalf("int64PtrFromUnstructured(%#v) = %v, want nil", tc.in, *got)
			}
			continue
		}
		if got == nil || *got != *tc.want {
			t.Fatalf("int64PtrFromUnstructured(%#v) = %v, want %d", tc.in, got, *tc.want)
		}
	}
}

func int64Ptr(v int64) *int64 { return &v }

func TestParseHardwareProfileSchedulingEdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("empty scheduling", func(t *testing.T) {
		t.Parallel()
		got, err := parseHardwareProfileResources(&unstructured.Unstructured{
			Object: map[string]any{"spec": map[string]any{"scheduling": map[string]any{}}},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.schedulingType != "" {
			t.Fatalf("schedulingType = %q, want empty", got.schedulingType)
		}
	})

	t.Run("invalid scheduling type", func(t *testing.T) {
		t.Parallel()
		_, err := parseHardwareProfileResources(&unstructured.Unstructured{
			Object: map[string]any{"spec": map[string]any{"scheduling": "bad"}},
		})
		if err == nil {
			t.Fatal("expected error for invalid scheduling type")
		}
	})

	t.Run("node with empty node map", func(t *testing.T) {
		t.Parallel()
		got, err := parseHardwareProfileResources(&unstructured.Unstructured{
			Object: map[string]any{
				"spec": map[string]any{
					"scheduling": map[string]any{
						"type": "Node",
						"node": map[string]any{},
					},
				},
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.schedulingType != hardwareProfileSchedulingNode {
			t.Fatalf("schedulingType = %q", got.schedulingType)
		}
		if got.nodeSelector != nil || len(got.tolerations) != 0 {
			t.Fatalf("expected empty node settings, got selector=%v tolerations=%v", got.nodeSelector, got.tolerations)
		}
	})

	t.Run("queue with empty kueue map", func(t *testing.T) {
		t.Parallel()
		got, err := parseHardwareProfileResources(&unstructured.Unstructured{
			Object: map[string]any{
				"spec": map[string]any{
					"scheduling": map[string]any{
						"type":  "Queue",
						"kueue": map[string]any{},
					},
				},
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.queueName != "" || got.priorityClassName != "" {
			t.Fatalf("expected empty queue settings, got %+v", got)
		}
	})

	t.Run("tolerationSeconds and non-map tolerations", func(t *testing.T) {
		t.Parallel()
		got, err := parseHardwareProfileResources(&unstructured.Unstructured{
			Object: map[string]any{
				"spec": map[string]any{
					"scheduling": map[string]any{
						"type": "Node",
						"node": map[string]any{
							"tolerations": []any{
								"ignored",
								map[string]any{
									"key":               "nvidia.com/gpu",
									"operator":          "Exists",
									"effect":            "NoSchedule",
									"tolerationSeconds": int64(30),
								},
							},
						},
					},
				},
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got.tolerations) != 1 {
			t.Fatalf("tolerations len = %d, want 1", len(got.tolerations))
		}
		if got.tolerations[0].TolerationSeconds == nil || *got.tolerations[0].TolerationSeconds != 30 {
			t.Fatalf("tolerationSeconds = %v, want 30", got.tolerations[0].TolerationSeconds)
		}
	})

	t.Run("accelerator without identifier skipped", func(t *testing.T) {
		t.Parallel()
		got, err := parseHardwareProfileResources(&unstructured.Unstructured{
			Object: map[string]any{
				"spec": map[string]any{
					"identifiers": []any{
						map[string]any{
							"resourceType": "Accelerator",
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
			t.Fatalf("expected skipped accelerator, got %+v", got)
		}
	})

	t.Run("invalid node map type", func(t *testing.T) {
		t.Parallel()
		_, err := parseHardwareProfileResources(&unstructured.Unstructured{
			Object: map[string]any{
				"spec": map[string]any{
					"scheduling": map[string]any{
						"type": "Node",
						"node": "bad",
					},
				},
			},
		})
		if err == nil {
			t.Fatal("expected error for invalid node type")
		}
	})

	t.Run("invalid nodeSelector type", func(t *testing.T) {
		t.Parallel()
		_, err := parseHardwareProfileResources(&unstructured.Unstructured{
			Object: map[string]any{
				"spec": map[string]any{
					"scheduling": map[string]any{
						"type": "Node",
						"node": map[string]any{
							"nodeSelector": "bad",
						},
					},
				},
			},
		})
		if err == nil {
			t.Fatal("expected error for invalid nodeSelector type")
		}
	})

	t.Run("invalid tolerations type", func(t *testing.T) {
		t.Parallel()
		_, err := parseHardwareProfileResources(&unstructured.Unstructured{
			Object: map[string]any{
				"spec": map[string]any{
					"scheduling": map[string]any{
						"type": "Node",
						"node": map[string]any{
							"tolerations": "bad",
						},
					},
				},
			},
		})
		if err == nil {
			t.Fatal("expected error for invalid tolerations type")
		}
	})

	t.Run("invalid kueue map type", func(t *testing.T) {
		t.Parallel()
		_, err := parseHardwareProfileResources(&unstructured.Unstructured{
			Object: map[string]any{
				"spec": map[string]any{
					"scheduling": map[string]any{
						"type":  "Queue",
						"kueue": "bad",
					},
				},
			},
		})
		if err == nil {
			t.Fatal("expected error for invalid kueue type")
		}
	})

	t.Run("empty tolerations slice", func(t *testing.T) {
		t.Parallel()
		got, err := parseHardwareProfileTolerations(nil)
		if err != nil || got != nil {
			t.Fatalf("got (%v, %v), want (nil, nil)", got, err)
		}
	})
}

func TestCopyStringMap(t *testing.T) {
	t.Parallel()
	if copyStringMap(nil) != nil {
		t.Fatal("expected nil for empty input")
	}
	in := map[string]string{"a": "1"}
	out := copyStringMap(in)
	out["a"] = "2"
	if in["a"] != "1" {
		t.Fatal("expected copy to be independent")
	}
}

func TestIsHardwareProfileDisabledNil(t *testing.T) {
	t.Parallel()
	if isHardwareProfileDisabled(nil) {
		t.Fatal("nil profile should not be disabled")
	}
}
