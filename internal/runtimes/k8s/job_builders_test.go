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
	// RunAsUser and RunAsGroup are intentionally not set to allow OpenShift SCC to assign them
	// from the allowed range based on the namespace's security constraints
	if container.SecurityContext.RunAsUser != nil {
		t.Fatalf("expected runAsUser to be nil (let OpenShift SCC assign it)")
	}
	if container.SecurityContext.RunAsGroup != nil {
		t.Fatalf("expected runAsGroup to be nil (let OpenShift SCC assign it)")
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

func TestContainerCommandList(t *testing.T) {
	command := buildContainerCommand([]string{"/bin/sh", "-c", "echo hello"})
	if len(command) != 3 {
		t.Fatalf("expected 3 command parts, got %d", len(command))
	}
	if command[0] != "/bin/sh" || command[1] != "-c" || command[2] != "echo hello" {
		t.Fatalf("unexpected command parts: %v", command)
	}
}

func TestContainerCommandTrimsEmptyItems(t *testing.T) {
	command := buildContainerCommand([]string{"  entrypoint ", "", " "})
	if len(command) != 1 || command[0] != "entrypoint" {
		t.Fatalf("unexpected command: %v", command)
	}
}
