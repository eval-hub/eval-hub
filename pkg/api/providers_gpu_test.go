package api

import (
	"encoding/json"
	"testing"
)

func TestProviderConfigJSON_GPURuntimeRoundTrip(t *testing.T) {
	original := ProviderConfig{
		Name: "GPU Provider",
		Runtime: &Runtime{
			K8s: &K8sRuntime{
				Image:      "quay.io/example/adapter:latest",
				Entrypoint: []string{"/bin/true"},
				GPU: &GPUConfig{
					Resource: "nvidia.com/gpu",
					Count:    2,
					NodeSelector: map[string]string{
						"nvidia.com/gpu.product": "NVIDIA-H100-SXM5-80GB",
					},
				},
			},
		},
		Benchmarks: []BenchmarkResource{{ID: "arc_easy", Name: "ARC Easy"}},
	}

	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded ProviderConfig
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Runtime == nil || decoded.Runtime.K8s == nil || decoded.Runtime.K8s.GPU == nil {
		t.Fatal("expected runtime.k8s.gpu after JSON round trip")
	}
	if decoded.Runtime.K8s.GPU.Resource != "nvidia.com/gpu" {
		t.Errorf("gpu resource = %q, want nvidia.com/gpu", decoded.Runtime.K8s.GPU.Resource)
	}
	if decoded.Runtime.K8s.GPU.Count != 2 {
		t.Errorf("gpu count = %d, want 2", decoded.Runtime.K8s.GPU.Count)
	}
	if decoded.Runtime.K8s.GPU.NodeSelector["nvidia.com/gpu.product"] != "NVIDIA-H100-SXM5-80GB" {
		t.Errorf("node_selector = %v", decoded.Runtime.K8s.GPU.NodeSelector)
	}
}
