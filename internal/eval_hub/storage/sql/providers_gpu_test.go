package sql_test

import (
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestProviderStorage_GPURuntimeRoundTrip(t *testing.T) {
	tenant := api.Tenant("tenant-gpu")
	store, err := getTestStorage(t, "sqlite", getDBName())
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	store = store.WithTenant(tenant)

	provider := &api.ProviderResource{
		Resource: api.Resource{
			ID:        "gpu-provider-1",
			CreatedAt: time.Now(),
			Tenant:    tenant,
		},
		ProviderConfig: api.ProviderConfig{
			Name: "GPU Test Provider",
			Runtime: &api.Runtime{
				K8s: &api.K8sRuntime{
					Image:      "quay.io/example/adapter:latest",
					Entrypoint: []string{"/bin/true"},
					GPU: &api.GPUConfig{
						Resource: "nvidia.com/gpu",
						Count:    1,
						NodeSelector: map[string]string{
							"nvidia.com/gpu.product": "A100-SXM4-40GB",
						},
					},
				},
			},
			Benchmarks: []api.BenchmarkResource{
				{ID: "arc_easy", Name: "ARC Easy", Category: "reasoning"},
			},
		},
	}

	if err := store.CreateProvider(provider); err != nil {
		t.Fatalf("CreateProvider failed: %v", err)
	}
	defer func() { _ = store.DeleteProvider("gpu-provider-1") }()

	got, err := store.GetProvider("gpu-provider-1")
	if err != nil {
		t.Fatalf("GetProvider failed: %v", err)
	}
	if got.Runtime == nil || got.Runtime.K8s == nil || got.Runtime.K8s.GPU == nil {
		t.Fatal("expected runtime.k8s.gpu after round trip")
	}
	if got.Runtime.K8s.GPU.Resource != "nvidia.com/gpu" {
		t.Errorf("gpu resource = %q, want nvidia.com/gpu", got.Runtime.K8s.GPU.Resource)
	}
	if got.Runtime.K8s.GPU.Count != 1 {
		t.Errorf("gpu count = %d, want 1", got.Runtime.K8s.GPU.Count)
	}
	if got.Runtime.K8s.GPU.NodeSelector["nvidia.com/gpu.product"] != "A100-SXM4-40GB" {
		t.Errorf("node_selector = %v", got.Runtime.K8s.GPU.NodeSelector)
	}
	if len(got.Benchmarks) != 1 || got.Benchmarks[0].ID != "arc_easy" {
		t.Fatalf("benchmarks = %+v, want arc_easy", got.Benchmarks)
	}
}
