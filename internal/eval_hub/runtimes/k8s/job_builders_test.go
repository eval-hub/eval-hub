package k8s

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_hub/runtimes/shared"
	"github.com/eval-hub/eval-hub/pkg/api"
	corev1 "k8s.io/api/core/v1"
)

func TestBuildJobRejectsInvalidSidecarPort(t *testing.T) {
	cfg := &jobConfig{
		jobID:          "job-bad-port",
		resourceGUID:   "guid-bp",
		benchmarkIndex: 0,
		namespace:      "default",
		providerID:     "provider-1",
		benchmarkID:    "bench-1",
		adapterImage:   "adapter:latest",
		sidecarConfig: &config.SidecarConfig{
			Port: 70000,
		},
	}

	_, err := buildJob(cfg)
	if err == nil {
		t.Fatal("buildJob() = nil, want sidecar port error")
	}
	if !strings.Contains(err.Error(), "sidecar port") {
		t.Fatalf("buildJob() error = %v, want sidecar port error", err)
	}
}

func findContainer(containers []corev1.Container, name string) *corev1.Container {
	for i := range containers {
		if containers[i].Name == name {
			return &containers[i]
		}
	}
	return nil
}

func findVolume(volumes []corev1.Volume, name string) *corev1.Volume {
	for i := range volumes {
		if volumes[i].Name == name {
			return &volumes[i]
		}
	}
	return nil
}

func findVolumeMount(mounts []corev1.VolumeMount, name string) *corev1.VolumeMount {
	for i := range mounts {
		if mounts[i].Name == name {
			return &mounts[i]
		}
	}
	return nil
}

func TestBuildConfigMap(t *testing.T) {

	cfg := &jobConfig{
		jobID:          "job-123",
		benchmarkIndex: 0,
		namespace:      "default",
		providerID:     "provider-1",
		benchmarkID:    "bench-1",
		jobSpec:        shared.JobSpec{},
		resourceGUID:   "guid-123",
	}

	configMap, err := buildConfigMap(cfg)
	if err != nil {
		t.Fatalf("buildConfigMap returned error: %v", err)
	}
	expectedName := configMapName(cfg.jobID, cfg.resourceGUID)
	if configMap.Name != expectedName {
		t.Fatalf("expected configmap name %s, got %s", expectedName, configMap.Name)
	}

	annotations := configMap.Annotations
	if annotations[annotationJobIDKey] != cfg.jobID {
		t.Fatalf("expected job_id annotation %q, got %q", cfg.jobID, annotations[annotationJobIDKey])
	}
	if annotations[annotationProviderIDKey] != cfg.providerID {
		t.Fatalf("expected provider_id annotation %q, got %q", cfg.providerID, annotations[annotationProviderIDKey])
	}
	if annotations[annotationBenchmarkIDKey] != cfg.benchmarkID {
		t.Fatalf("expected benchmark_id annotation %q, got %q", cfg.benchmarkID, annotations[annotationBenchmarkIDKey])
	}
	if _, ok := configMap.Data[sidecarConfigFileName]; !ok {
		t.Fatalf("expected ConfigMap data key %q", sidecarConfigFileName)
	}
	if configMap.Data[sidecarConfigFileName] == "" {
		t.Fatalf("sidecar_config.json should be non-empty")
	}
	var sidecar map[string]any
	if err := json.Unmarshal([]byte(configMap.Data[sidecarConfigFileName]), &sidecar); err != nil {
		t.Fatalf("sidecar_config.json invalid JSON: %v", err)
	}
}

func TestBuildConfigMapSidecarConfigJSONContent(t *testing.T) {
	cfg := &jobConfig{
		jobID:          "job-123",
		benchmarkIndex: 0,
		namespace:      "default",
		providerID:     "provider-1",
		benchmarkID:    "bench-1",
		jobSpec:        shared.JobSpec{},
		resourceGUID:   "guid-123",
		sidecarConfig: &config.SidecarConfig{
			Port:    8081,
			BaseURL: "http://localhost:8081",
		},
	}
	cm, err := buildConfigMap(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(cm.Data[sidecarConfigFileName]), &m); err != nil {
		t.Fatal(err)
	}
	if m["port"].(float64) != 8081 {
		t.Fatalf("port: %v", m["port"])
	}
}

func TestBuildJobHasEvaluationPhasePendingLabel(t *testing.T) {
	cfg := &jobConfig{
		jobID:          "job-123",
		resourceGUID:   "guid-123",
		benchmarkIndex: 0,
		namespace:      "default",
		providerID:     "provider-1",
		benchmarkID:    "bench-1",
		adapterImage:   "adapter:latest",
	}
	job, err := buildJob(cfg)
	if err != nil {
		t.Fatalf("buildJob: %v", err)
	}
	if got := job.Labels[labelEvaluationPhaseKey]; got != EvaluationPhasePending {
		t.Fatalf("expected Job label %q=%q, got %q", labelEvaluationPhaseKey, EvaluationPhasePending, got)
	}
}

func TestBuildJobRequiresAdapterImage(t *testing.T) {
	cfg := &jobConfig{
		jobID:          "job-123",
		resourceGUID:   "guid-123",
		benchmarkIndex: 0,
		namespace:      "default",
		providerID:     "provider-1",
		benchmarkID:    "bench-1",
	}

	_, err := buildJob(cfg)
	if err == nil {
		t.Fatalf("expected error for missing adapter image")
	}
}

func TestBuildJobAnnotations(t *testing.T) {
	cfg := &jobConfig{
		jobID:          "job-123",
		resourceGUID:   "guid-123",
		benchmarkIndex: 0,
		namespace:      "default",
		providerID:     "provider-1",
		benchmarkID:    "bench-1",
		adapterImage:   "adapter:latest",
		defaultEnv:     []api.EnvVar{},
	}

	job, err := buildJob(cfg)
	if err != nil {
		t.Fatalf("buildJob returned error: %v", err)
	}

	if job.Annotations[annotationJobIDKey] != cfg.jobID {
		t.Fatalf("expected job_id annotation %q, got %q", cfg.jobID, job.Annotations[annotationJobIDKey])
	}
	if job.Annotations[annotationProviderIDKey] != cfg.providerID {
		t.Fatalf("expected provider_id annotation %q, got %q", cfg.providerID, job.Annotations[annotationProviderIDKey])
	}
	if job.Annotations[annotationBenchmarkIDKey] != cfg.benchmarkID {
		t.Fatalf("expected benchmark_id annotation %q, got %q", cfg.benchmarkID, job.Annotations[annotationBenchmarkIDKey])
	}

	podAnnotations := job.Spec.Template.Annotations
	if podAnnotations[annotationJobIDKey] != cfg.jobID {
		t.Fatalf("expected pod job_id annotation %q, got %q", cfg.jobID, podAnnotations[annotationJobIDKey])
	}
	if podAnnotations[annotationProviderIDKey] != cfg.providerID {
		t.Fatalf("expected pod provider_id annotation %q, got %q", cfg.providerID, podAnnotations[annotationProviderIDKey])
	}
	if podAnnotations[annotationBenchmarkIDKey] != cfg.benchmarkID {
		t.Fatalf("expected pod benchmark_id annotation %q, got %q", cfg.benchmarkID, podAnnotations[annotationBenchmarkIDKey])
	}
}
