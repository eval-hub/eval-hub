package k8s

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/google/uuid"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func getTestNamespace(t *testing.T) string {
	t.Helper()
	namespace := strings.TrimSpace(os.Getenv("KUBERNETES_NAMESPACE"))
	if namespace == "" {
		return metav1.NamespaceDefault
	}
	return namespace
}

func listJobsByJobID(t *testing.T, clientset kubernetes.Interface, jobID string) []batchv1.Job {
	t.Helper()
	labelSelector := fmt.Sprintf("%s=%s", labelJobIDKey, sanitizeLabelValue(jobID))
	jobs, err := clientset.BatchV1().Jobs(getTestNamespace(t)).List(context.Background(), metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		t.Fatalf("failed to list jobs: %v", err)
	}
	return jobs.Items
}

func listConfigMapsByJobID(t *testing.T, clientset kubernetes.Interface, jobID string) []corev1.ConfigMap {
	t.Helper()
	labelSelector := fmt.Sprintf("%s=%s", labelJobIDKey, sanitizeLabelValue(jobID))
	configMaps, err := clientset.CoreV1().ConfigMaps(getTestNamespace(t)).List(context.Background(), metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		t.Fatalf("failed to list configmaps: %v", err)
	}
	return configMaps.Items
}

func TestRunEvaluationJobCreatesResources(t *testing.T) {
	// Integration test: creates one ConfigMap and Job per benchmark in a real cluster.
	if os.Getenv("K8S_INTEGRATION_TEST") != "1" {
		t.Skip("set K8S_INTEGRATION_TEST=1 to run against a real cluster")
	}
	const apiTimeout = 15 * time.Second
	t.Setenv("SERVICE_URL", "http://eval-hub")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	helper, err := NewKubernetesHelper()
	if err != nil {
		t.Fatalf("failed to create kubernetes helper: %v", err)
	}
	jobID := uuid.NewString()
	benchmarkID := "arc_easy"
	benchmarkIDTwo := "arc"
	runtime := &K8sRuntime{
		logger: logger,
		helper: helper,
		ctx:    context.Background(),
		providers: map[string]api.ProviderResource{
			"lm_evaluation_harness": {
				Resource: api.Resource{ID: "lm_evaluation_harness"},
				ProviderConfig: api.ProviderConfig{
					Runtime: &api.Runtime{
						K8s: &api.K8sRuntime{
							Image:       "docker.io/library/busybox:1.36",
							Entrypoint:  []string{"/bin/sh", "-c", "echo hello"},
							CPULimit:    "500m",
							MemoryLimit: "1Gi",
							Env: []api.EnvVar{
								{Name: "VAR_NAME", Value: "VALUE"},
							},
						},
					},
				},
			},
		},
	}

	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{ID: jobID},
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{
				URL:  "http://model",
				Name: "model",
			},
			Benchmarks: []api.BenchmarkConfig{
				{
					Ref:        api.Ref{ID: benchmarkID},
					ProviderID: "lm_evaluation_harness",
					Parameters: map[string]any{
						"num_examples": 1,
						"max_tokens":   128,
						"temperature":  0.2,
					},
				},
				{
					Ref:        api.Ref{ID: benchmarkIDTwo},
					ProviderID: "lm_evaluation_harness",
					Parameters: map[string]any{
						"num_examples": 2,
						"max_tokens":   256,
						"temperature":  0.1,
					},
				},
			},
		},
	}

	var storageNil = (*abstractions.Storage)(nil)

	if err := runtime.RunEvaluationJob(evaluation, storageNil); err != nil {
		t.Fatalf("RunEvaluationJob returned error: %v", err)
	}

	t.Cleanup(func() {
		_ = runtime.DeleteEvaluationJobResources(evaluation)
	})
	namespace := resolveNamespace("")
	labelSelector := fmt.Sprintf("%s=%s", labelJobIDKey, sanitizeLabelValue(jobID))
	deadline := time.Now().Add(apiTimeout)
	for time.Now().Before(deadline) {
		jobs, err := helper.ListJobs(context.Background(), namespace, labelSelector)
		if err != nil {
			t.Fatalf("failed to list jobs: %v", err)
		}
		configMaps, err := helper.ListConfigMaps(context.Background(), namespace, labelSelector)
		if err != nil {
			t.Fatalf("failed to list configmaps: %v", err)
		}
		if len(jobs) == len(evaluation.Benchmarks) &&
			len(configMaps) == len(evaluation.Benchmarks) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	jobs, err := helper.ListJobs(context.Background(), namespace, labelSelector)
	if err != nil {
		t.Fatalf("failed to list jobs: %v", err)
	}
	configMaps, err := helper.ListConfigMaps(context.Background(), namespace, labelSelector)
	if err != nil {
		t.Fatalf("failed to list configmaps: %v", err)
	}
	if len(jobs) != len(evaluation.Benchmarks) {
		t.Fatalf("expected %d jobs, got %d", len(evaluation.Benchmarks), len(jobs))
	}
	if len(configMaps) != len(evaluation.Benchmarks) {
		t.Fatalf("expected %d configmaps, got %d", len(evaluation.Benchmarks), len(configMaps))
	}
	expectedBenchmarks := map[string]struct{}{
		benchmarkID:    {},
		benchmarkIDTwo: {},
	}
	foundBenchmarks := map[string]struct{}{}
	for _, job := range jobs {
		if id, ok := job.Labels[labelBenchmarkIDKey]; ok {
			foundBenchmarks[id] = struct{}{}
		}
	}
	for id := range expectedBenchmarks {
		if _, ok := foundBenchmarks[sanitizeLabelValue(id)]; !ok {
			t.Fatalf("expected benchmark label %s to be present", id)
		}
	}
}

func TestCreateBenchmarkResourcesDuplicateBenchmarkIDDoesNotCollide(t *testing.T) {
	// Integration test: duplicates should still create distinct resources.
	if os.Getenv("K8S_INTEGRATION_TEST") != "1" {
		t.Skip("set K8S_INTEGRATION_TEST=1 to run against a real cluster")
	}
	t.Setenv("SERVICE_URL", "http://eval-hub")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	helper, err := NewKubernetesHelper()
	if err != nil {
		t.Fatalf("failed to create kubernetes helper: %v", err)
	}
	runtime := &K8sRuntime{
		logger: logger,
		helper: helper,
		ctx:    context.Background(),
		providers: map[string]api.ProviderResource{
			"lm_evaluation_harness": {
				Resource: api.Resource{ID: "lm_evaluation_harness"},
				ProviderConfig: api.ProviderConfig{
					Runtime: &api.Runtime{
						K8s: &api.K8sRuntime{
							Image: "docker.io/library/busybox:1.36",
						},
					},
				},
			},
			"lighteval": {
				Resource: api.Resource{ID: "lighteval"},
				ProviderConfig: api.ProviderConfig{
					Runtime: &api.Runtime{
						K8s: &api.K8sRuntime{
							Image: "docker.io/library/busybox:1.36",
						},
					},
				},
			},
		},
	}

	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{ID: uuid.NewString()},
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{
				URL:  "http://model",
				Name: "model",
			},
			Benchmarks: []api.BenchmarkConfig{
				{
					Ref:        api.Ref{ID: "arc_easy"},
					ProviderID: "lm_evaluation_harness",
				},
				{
					Ref:        api.Ref{ID: "arc:easy"},
					ProviderID: "lighteval",
				},
			},
		},
	}

	t.Cleanup(func() {
		_ = runtime.DeleteEvaluationJobResources(evaluation)
	})

	if err := runtime.createBenchmarkResources(context.Background(), logger, evaluation, &evaluation.Benchmarks[0], 0); err != nil {
		t.Logf("first createBenchmarkResources error: %v", err)
		t.Fatalf("unexpected error creating first benchmark resources: %v", err)
	}

	if err := runtime.createBenchmarkResources(context.Background(), logger, evaluation, &evaluation.Benchmarks[1], 1); err != nil {
		t.Fatalf("unexpected error creating second benchmark resources: %v", err)
	}

	jobs := listJobsByJobID(t, helper.clientset, evaluation.Resource.ID)
	configMaps := listConfigMapsByJobID(t, helper.clientset, evaluation.Resource.ID)
	if len(jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(jobs))
	}
	if len(configMaps) != 2 {
		t.Fatalf("expected 2 configmaps, got %d", len(configMaps))
	}
}

func TestCreateBenchmarkResourcesSetsAnnotationsIntegration(t *testing.T) {
	// Integration test: verify annotations on Job/ConfigMap/Pod.
	if os.Getenv("K8S_INTEGRATION_TEST") != "1" {
		t.Skip("set K8S_INTEGRATION_TEST=1 to run against a real cluster")
	}
	t.Setenv("SERVICE_URL", "http://eval-hub")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	helper, err := NewKubernetesHelper()
	if err != nil {
		t.Fatalf("failed to create kubernetes helper: %v", err)
	}
	runtime := &K8sRuntime{
		logger: logger,
		helper: helper,
		ctx:    context.Background(),
		providers: map[string]api.ProviderResource{
			"lm_evaluation_harness": {
				Resource: api.Resource{ID: "lm_evaluation_harness"},
				ProviderConfig: api.ProviderConfig{
					Runtime: &api.Runtime{
						K8s: &api.K8sRuntime{
							Image: "docker.io/library/busybox:1.36",
						},
					},
				},
			},
		},
	}

	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{ID: uuid.NewString()},
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{
				URL:  "http://model",
				Name: "model",
			},
			Benchmarks: []api.BenchmarkConfig{
				{
					Ref:        api.Ref{ID: "arc_easy"},
					ProviderID: "lm_evaluation_harness",
				},
			},
		},
	}

	t.Cleanup(func() {
		_ = runtime.DeleteEvaluationJobResources(evaluation)
	})

	if err := runtime.createBenchmarkResources(context.Background(), logger, evaluation, &evaluation.Benchmarks[0], 0); err != nil {
		t.Fatalf("unexpected error creating benchmark resources: %v", err)
	}

	configMaps := listConfigMapsByJobID(t, helper.clientset, evaluation.Resource.ID)
	if len(configMaps) != 1 {
		t.Fatalf("expected 1 configmap, got %d", len(configMaps))
	}
	cm := configMaps[0]
	if cm.Annotations[annotationJobIDKey] != evaluation.Resource.ID {
		t.Fatalf("expected configmap job_id annotation %q, got %q", evaluation.Resource.ID, cm.Annotations[annotationJobIDKey])
	}
	if cm.Annotations[annotationProviderIDKey] != evaluation.Benchmarks[0].ProviderID {
		t.Fatalf("expected configmap provider_id annotation %q, got %q", evaluation.Benchmarks[0].ProviderID, cm.Annotations[annotationProviderIDKey])
	}
	if cm.Annotations[annotationBenchmarkIDKey] != evaluation.Benchmarks[0].ID {
		t.Fatalf("expected configmap benchmark_id annotation %q, got %q", evaluation.Benchmarks[0].ID, cm.Annotations[annotationBenchmarkIDKey])
	}

	jobs := listJobsByJobID(t, helper.clientset, evaluation.Resource.ID)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	job := jobs[0]
	if job.Annotations[annotationJobIDKey] != evaluation.Resource.ID {
		t.Fatalf("expected job job_id annotation %q, got %q", evaluation.Resource.ID, job.Annotations[annotationJobIDKey])
	}
	if job.Annotations[annotationProviderIDKey] != evaluation.Benchmarks[0].ProviderID {
		t.Fatalf("expected job provider_id annotation %q, got %q", evaluation.Benchmarks[0].ProviderID, job.Annotations[annotationProviderIDKey])
	}
	if job.Annotations[annotationBenchmarkIDKey] != evaluation.Benchmarks[0].ID {
		t.Fatalf("expected job benchmark_id annotation %q, got %q", evaluation.Benchmarks[0].ID, job.Annotations[annotationBenchmarkIDKey])
	}
	if job.Spec.Template.Annotations[annotationJobIDKey] != evaluation.Resource.ID {
		t.Fatalf("expected pod job_id annotation %q, got %q", evaluation.Resource.ID, job.Spec.Template.Annotations[annotationJobIDKey])
	}
	if job.Spec.Template.Annotations[annotationProviderIDKey] != evaluation.Benchmarks[0].ProviderID {
		t.Fatalf("expected pod provider_id annotation %q, got %q", evaluation.Benchmarks[0].ProviderID, job.Spec.Template.Annotations[annotationProviderIDKey])
	}
	if job.Spec.Template.Annotations[annotationBenchmarkIDKey] != evaluation.Benchmarks[0].ID {
		t.Fatalf("expected pod benchmark_id annotation %q, got %q", evaluation.Benchmarks[0].ID, job.Spec.Template.Annotations[annotationBenchmarkIDKey])
	}
}

func TestCreateBenchmarkResourcesAddsModelAuthVolumeAndEnvIntegration(t *testing.T) {
	// Integration test: verify model auth volume/mount/env on Job/Pod.
	if os.Getenv("K8S_INTEGRATION_TEST") != "1" {
		t.Skip("set K8S_INTEGRATION_TEST=1 to run against a real cluster")
	}
	t.Setenv("SERVICE_URL", "http://eval-hub")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	helper, err := NewKubernetesHelper()
	if err != nil {
		t.Fatalf("failed to create kubernetes helper: %v", err)
	}
	runtime := &K8sRuntime{
		logger: logger,
		helper: helper,
		ctx:    context.Background(),
		providers: map[string]api.ProviderResource{
			"lm_evaluation_harness": {
				Resource: api.Resource{ID: "lm_evaluation_harness"},
				ProviderConfig: api.ProviderConfig{
					Runtime: &api.Runtime{
						K8s: &api.K8sRuntime{
							Image: "docker.io/library/busybox:1.36",
						},
					},
				},
			},
		},
	}

	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{ID: uuid.NewString()},
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{
				URL:  "http://model",
				Name: "model",
				Auth: &api.ModelAuth{SecretRef: "model-auth-secret"},
			},
			Benchmarks: []api.BenchmarkConfig{
				{
					Ref:        api.Ref{ID: "arc_easy"},
					ProviderID: "lm_evaluation_harness",
				},
			},
		},
	}

	t.Cleanup(func() {
		_ = runtime.DeleteEvaluationJobResources(evaluation)
	})

	if err := runtime.createBenchmarkResources(context.Background(), logger, evaluation, &evaluation.Benchmarks[0], 0); err != nil {
		t.Fatalf("unexpected error creating benchmark resources: %v", err)
	}

	labelSelector := fmt.Sprintf("%s=%s", labelJobIDKey, sanitizeLabelValue(evaluation.Resource.ID))
	jobs, err := helper.clientset.BatchV1().Jobs(getTestNamespace(t)).List(context.Background(), metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		t.Fatalf("failed to list jobs: %v", err)
	}
	if len(jobs.Items) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs.Items))
	}
	job := jobs.Items[0]
	container := job.Spec.Template.Spec.Containers[0]

	var foundVolume bool
	for _, volume := range job.Spec.Template.Spec.Volumes {
		if volume.Name == modelAuthVolumeName {
			foundVolume = true
			if volume.VolumeSource.Secret == nil || volume.VolumeSource.Secret.SecretName != "model-auth-secret" {
				t.Fatalf("expected model auth secret volume to reference %q", "model-auth-secret")
			}
		}
	}
	if !foundVolume {
		t.Fatalf("expected volume %s to be present", modelAuthVolumeName)
	}

	var foundMount bool
	for _, mount := range container.VolumeMounts {
		if mount.Name == modelAuthVolumeName {
			foundMount = true
			if mount.MountPath != modelAuthMountPath {
				t.Fatalf("expected mount path %q, got %q", modelAuthMountPath, mount.MountPath)
			}
		}
	}
	if !foundMount {
		t.Fatalf("expected volume mount %s to be present", modelAuthVolumeName)
	}

}
