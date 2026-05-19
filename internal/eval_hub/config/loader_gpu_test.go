package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_hub/validation"
	"github.com/eval-hub/eval-hub/internal/logging"
)

func TestLoadProviderConfigs_ParsesGPUProviderFixture(t *testing.T) {
	logger := logging.FallbackLogger()
	configRoot := t.TempDir()
	copyProviderFixture(t, configRoot, "provider_gpu_test.yaml")

	providers, err := config.LoadProviderConfigs(logger, validation.NewValidator(), configRoot)
	if err != nil {
		t.Fatalf("LoadProviderConfigs failed: %v", err)
	}
	p, ok := providers["gpu_test_provider"]
	if !ok {
		t.Fatalf("expected gpu_test_provider, got keys: %v", providerIDs(providers))
	}
	if p.Runtime == nil || p.Runtime.K8s == nil {
		t.Fatal("expected runtime.k8s to be set")
	}
	if p.Runtime.K8s.GPU == nil {
		t.Fatal("expected runtime.k8s.gpu to be set")
	}
	if p.Runtime.K8s.GPU.Resource != "nvidia.com/gpu" {
		t.Errorf("gpu resource = %q, want nvidia.com/gpu", p.Runtime.K8s.GPU.Resource)
	}
	if p.Runtime.K8s.GPU.Count != 1 {
		t.Errorf("gpu count = %d, want 1", p.Runtime.K8s.GPU.Count)
	}
	if len(p.Runtime.K8s.GPU.NodeSelector) != 0 {
		t.Errorf("expected no node_selector, got %v", p.Runtime.K8s.GPU.NodeSelector)
	}
	if len(p.Benchmarks) != 1 || p.Benchmarks[0].ID != "arc_easy" {
		t.Fatalf("expected benchmark arc_easy, got %+v", p.Benchmarks)
	}
}

func TestLoadProviderConfigs_ParsesGPUNodeSelectorFixture(t *testing.T) {
	logger := logging.FallbackLogger()
	configRoot := t.TempDir()
	copyProviderFixture(t, configRoot, "provider_gpu_a100.yaml")

	providers, err := config.LoadProviderConfigs(logger, validation.NewValidator(), configRoot)
	if err != nil {
		t.Fatalf("LoadProviderConfigs failed: %v", err)
	}
	p, ok := providers["gpu_test_provider_l4"]
	if !ok {
		t.Fatalf("expected gpu_test_provider_l4, got keys: %v", providerIDs(providers))
	}
	if p.Runtime == nil || p.Runtime.K8s == nil || p.Runtime.K8s.GPU == nil {
		t.Fatal("expected runtime.k8s.gpu to be set")
	}
	got, ok := p.Runtime.K8s.GPU.NodeSelector["nvidia.com/gpu.product"]
	if !ok {
		t.Fatalf("expected nvidia.com/gpu.product in node_selector, got %v", p.Runtime.K8s.GPU.NodeSelector)
	}
	if got != "NVIDIA-L4" {
		t.Errorf("node_selector value = %q, want NVIDIA-L4", got)
	}
}

func TestLoadProviderConfigs_ParsesGPUNodeSelectorFromInlineYAML(t *testing.T) {
	logger := logging.FallbackLogger()
	configRoot := t.TempDir()
	provDir := filepath.Join(configRoot, "providers")
	if err := os.MkdirAll(provDir, 0o755); err != nil {
		t.Fatalf("mkdir providers: %v", err)
	}
	content := `id: gpu_selector_test
name: GPU Selector Test
description: provider with dotted node selector label
runtime:
  k8s:
    image: quay.io/example/adapter:latest
    entrypoint:
      - /bin/true
    gpu:
      resource: nvidia.com/gpu
      count: 1
      node_selector:
        nvidia.com/gpu.product: A100-SXM4-40GB
benchmarks:
  - id: arc_easy
    name: arc_easy
    description: test benchmark
    category: reasoning
`
	if err := os.WriteFile(filepath.Join(provDir, "gpu_selector_test.yaml"), []byte(content), 0o600); err != nil {
		t.Fatalf("write provider yaml: %v", err)
	}

	providers, err := config.LoadProviderConfigs(logger, validation.NewValidator(), configRoot)
	if err != nil {
		t.Fatalf("LoadProviderConfigs failed: %v", err)
	}
	p, ok := providers["gpu_selector_test"]
	if !ok {
		t.Fatalf("expected gpu_selector_test in providers, got keys: %v", providerIDs(providers))
	}
	if p.Runtime == nil {
		t.Fatalf("gpu_selector_test: expected runtime, got nil")
	}
	if p.Runtime.K8s == nil {
		t.Fatalf("gpu_selector_test: expected runtime.k8s, got nil")
	}
	if p.Runtime.K8s.GPU == nil {
		t.Fatalf("gpu_selector_test: expected runtime.k8s.gpu, got nil")
	}
	if p.Runtime.K8s.GPU.NodeSelector == nil {
		t.Fatalf("gpu_selector_test: expected runtime.k8s.gpu.node_selector map, got nil")
	}
	got, ok := p.Runtime.K8s.GPU.NodeSelector["nvidia.com/gpu.product"]
	if !ok {
		t.Fatalf("gpu_selector_test: expected nvidia.com/gpu.product in node_selector, got %v", p.Runtime.K8s.GPU.NodeSelector)
	}
	if got != "A100-SXM4-40GB" {
		t.Fatalf("gpu_selector_test: node_selector[nvidia.com/gpu.product] = %q, want A100-SXM4-40GB", got)
	}
}

func copyProviderFixture(t *testing.T, configRoot, fixtureName string) {
	t.Helper()
	src := filepath.Join("..", "..", "..", "tests", "features", "test_data", fixtureName)
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read fixture %s: %v", fixtureName, err)
	}
	provDir := filepath.Join(configRoot, "providers")
	if err := os.MkdirAll(provDir, 0o755); err != nil {
		t.Fatalf("mkdir providers: %v", err)
	}
	if err := os.WriteFile(filepath.Join(provDir, fixtureName), data, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}
