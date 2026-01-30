package k8s

import (
	"testing"

	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestBuildConfigMap(t *testing.T) {
	cfg := &jobConfig{
		jobID:       "job-123",
		namespace:   "default",
		providerID:  "provider-1",
		benchmarkID: "bench-1",
		jobSpecJSON: "{}",
	}

	configMap := buildConfigMap(cfg)
	expectedName := jobPrefix + cfg.jobID + specSuffix
	if configMap.Name != expectedName {
		t.Fatalf("expected configmap name %s, got %s", expectedName, configMap.Name)
	}
	if configMap.Data[jobSpecFileName] != "{}" {
		t.Fatalf("expected job spec data to be set")
	}
}

func TestBuildJobRequiresAdapterImage(t *testing.T) {
	cfg := &jobConfig{
		jobID:       "job-123",
		namespace:   "default",
		providerID:  "provider-1",
		benchmarkID: "bench-1",
	}

	_, err := buildJob(cfg)
	if err == nil {
		t.Fatalf("expected error for missing adapter image")
	}
}

func TestBuildJobSecurityContext(t *testing.T) {
	cfg := &jobConfig{
		jobID:             "job-123",
		namespace:         "default",
		providerID:        "provider-1",
		benchmarkID:       "bench-1",
		adapterImage:      "adapter:latest",
		evalHubServiceURL: "http://eval-hub",
		defaultEnv:        []api.ProviderEnvVar{},
	}

	job, err := buildJob(cfg)
	if err != nil {
		t.Fatalf("buildJob returned error: %v", err)
	}
	container := job.Spec.Template.Spec.Containers[0]
	if container.SecurityContext == nil || container.SecurityContext.AllowPrivilegeEscalation == nil {
		t.Fatalf("expected security context with allowPrivilegeEscalation")
	}
	if *container.SecurityContext.AllowPrivilegeEscalation {
		t.Fatalf("expected allowPrivilegeEscalation to be false")
	}
	if container.SecurityContext.Capabilities == nil || len(container.SecurityContext.Capabilities.Drop) == 0 {
		t.Fatalf("expected dropped capabilities")
	}
	if container.SecurityContext.Capabilities.Drop[0] != "ALL" {
		t.Fatalf("expected ALL capability drop")
	}
}
