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
	expectedName := configMapName(cfg.jobID, cfg.benchmarkID)
	if configMap.Name != expectedName {
		t.Fatalf("expected configmap name %s, got %s", expectedName, configMap.Name)
	}
	if configMap.Data[jobSpecFileName] != "{}" {
		t.Fatalf("expected job spec data to be set")
	}
}

func TestBuildK8sNameSanitizes(t *testing.T) {
	name := buildK8sName("Job-123", "AraDiCE_boolq_lev", "")
	if name != "eval-job-job-123-aradice-boolq-lev" {
		t.Fatalf("expected sanitized name %q, got %q", "eval-job-job-123-aradice-boolq-lev", name)
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
		jobID:        "job-123",
		namespace:    "default",
		providerID:   "provider-1",
		benchmarkID:  "bench-1",
		adapterImage: "adapter:latest",
		defaultEnv:   []api.EnvVar{},
	}

	job, err := buildJob(cfg)
	if err != nil {
		t.Fatalf("buildJob returned error: %v", err)
	}
	if len(job.Spec.Template.Spec.Containers) == 0 {
		t.Fatalf("expected at least one container in pod spec")
	}
	container := job.Spec.Template.Spec.Containers[0]
	if container.SecurityContext == nil || container.SecurityContext.AllowPrivilegeEscalation == nil {
		t.Fatalf("expected security context with allowPrivilegeEscalation")
	}
	if *container.SecurityContext.AllowPrivilegeEscalation {
		t.Fatalf("expected allowPrivilegeEscalation to be false")
	}
	if container.SecurityContext.RunAsNonRoot == nil || !*container.SecurityContext.RunAsNonRoot {
		t.Fatalf("expected runAsNonRoot to be true")
	}
	if container.SecurityContext.RunAsUser == nil || *container.SecurityContext.RunAsUser == 0 {
		t.Fatalf("expected non-zero runAsUser")
	}
	if container.SecurityContext.RunAsGroup == nil || *container.SecurityContext.RunAsGroup == 0 {
		t.Fatalf("expected non-zero runAsGroup")
	}
	if container.SecurityContext.Capabilities == nil || len(container.SecurityContext.Capabilities.Drop) == 0 {
		t.Fatalf("expected dropped capabilities")
	}
	if container.SecurityContext.Capabilities.Drop[0] != "ALL" {
		t.Fatalf("expected ALL capability drop")
	}
	if container.SecurityContext.SeccompProfile == nil || container.SecurityContext.SeccompProfile.Type == "" {
		t.Fatalf("expected seccomp profile to be set")
	}
}
