package k8s

import (
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestBuildJobConfigDefaults(t *testing.T) {
	t.Setenv(evalHubServiceEnv, "http://eval-hub")
	retry := 2
	evaluation := &api.EvaluationJobResource{
		Resource: api.Resource{ID: "job-123"},
		EvaluationJobConfig: api.EvaluationJobConfig{
			RetryAttempts: &retry,
		},
	}
	provider := &api.ProviderResource{
		ProviderID: "provider-1",
		Runtime: &api.ProviderRuntime{
			AdapterImage: "adapter:latest",
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
	if !strings.Contains(cfg.jobSpecJSON, "\"id\": \"job-123\"") {
		t.Fatalf("expected job spec json to include job id")
	}
}

func TestBuildJobConfigMissingRuntime(t *testing.T) {
	t.Setenv(evalHubServiceEnv, "http://eval-hub")
	evaluation := &api.EvaluationJobResource{
		Resource: api.Resource{ID: "job-123"},
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
	t.Setenv(evalHubServiceEnv, "http://eval-hub")
	evaluation := &api.EvaluationJobResource{
		Resource: api.Resource{ID: "job-123"},
	}
	provider := &api.ProviderResource{
		ProviderID: "provider-1",
		Runtime:    &api.ProviderRuntime{},
	}

	_, err := buildJobConfig(evaluation, provider, "bench-1")
	if err == nil {
		t.Fatalf("expected error for missing adapter image")
	}
}

func TestBuildJobConfigMissingServiceURL(t *testing.T) {
	evaluation := &api.EvaluationJobResource{
		Resource: api.Resource{ID: "job-123"},
	}
	provider := &api.ProviderResource{
		ProviderID: "provider-1",
		Runtime: &api.ProviderRuntime{
			AdapterImage: "adapter:latest",
		},
	}

	_, err := buildJobConfig(evaluation, provider, "bench-1")
	if err == nil {
		t.Fatalf("expected error for missing %s", evalHubServiceEnv)
	}
}
