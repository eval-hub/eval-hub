package k8s

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/google/uuid"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func listJobsByJobID(t *testing.T, clientset *fake.Clientset, jobID string) []batchv1.Job {
	t.Helper()
	labelSelector := fmt.Sprintf("%s=%s", labelJobIDKey, sanitizeLabelValue(jobID))
	jobs, err := clientset.BatchV1().Jobs(defaultNamespace).List(context.Background(), metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		t.Fatalf("failed to list jobs: %v", err)
	}
	return jobs.Items
}

func listConfigMapsByJobID(t *testing.T, clientset *fake.Clientset, jobID string) []corev1.ConfigMap {
	t.Helper()
	labelSelector := fmt.Sprintf("%s=%s", labelJobIDKey, sanitizeLabelValue(jobID))
	configMaps, err := clientset.CoreV1().ConfigMaps(defaultNamespace).List(context.Background(), metav1.ListOptions{LabelSelector: labelSelector})
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
				ProviderConfigInternal: api.ProviderConfigInternal{
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

func TestCreateBenchmarkResourcesReturnsErrorWhenConfigMapExists(t *testing.T) {
	// Unit test: resource creation fails if ConfigMap already exists.
	t.Setenv("SERVICE_URL", "http://eval-hub")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	clientset := fake.NewSimpleClientset()
	clientset.PrependReactor("create", "configmaps", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewAlreadyExists(corev1.Resource("configmaps"), "job-spec")
	})
	runtime := &K8sRuntime{
		logger: logger,
		helper: &KubernetesHelper{clientset: clientset},
		providers: map[string]api.ProviderResource{
			"lm_evaluation_harness": {
				Resource: api.Resource{ID: "lm_evaluation_harness"},
				ProviderConfigInternal: api.ProviderConfigInternal{
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
			Resource: api.Resource{ID: "job-invalid"},
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{
				URL:  "http://model",
				Name: "model",
			},
			Benchmarks: []api.BenchmarkConfig{
				{
					Ref:        api.Ref{ID: "bench-1"},
					ProviderID: "lm_evaluation_harness",
					Parameters: map[string]any{
						"num_examples": 1,
						"max_tokens":   64,
					},
				},
				{
					Ref:        api.Ref{ID: "bench-2"},
					ProviderID: "lm_evaluation_harness",
					Parameters: map[string]any{
						"num_examples": 2,
						"temperature":  0.3,
					},
				},
			},
		},
	}

	if err := runtime.createBenchmarkResources(context.Background(), logger, evaluation, &evaluation.Benchmarks[0], 0); err == nil {
		t.Fatalf("expected error but got nil")
	} else if !apierrors.IsAlreadyExists(err) {
		t.Fatalf("expected already exists error, got %v", err)
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
		providers: map[string]api.ProviderResource{
			"lm_evaluation_harness": {
				Resource: api.Resource{ID: "lm_evaluation_harness"},
				ProviderConfigInternal: api.ProviderConfigInternal{
					Runtime: &api.Runtime{
						K8s: &api.K8sRuntime{
							Image: "docker.io/library/busybox:1.36",
						},
					},
				},
			},
			"lighteval": {
				Resource: api.Resource{ID: "lighteval"},
				ProviderConfigInternal: api.ProviderConfigInternal{
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

	jobs := listJobsByJobID(t, helper.clientset.(*fake.Clientset), evaluation.Resource.ID)
	configMaps := listConfigMapsByJobID(t, helper.clientset.(*fake.Clientset), evaluation.Resource.ID)
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
		providers: map[string]api.ProviderResource{
			"lm_evaluation_harness": {
				Resource: api.Resource{ID: "lm_evaluation_harness"},
				ProviderConfigInternal: api.ProviderConfigInternal{
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

	configMaps := listConfigMapsByJobID(t, helper.clientset.(*fake.Clientset), evaluation.Resource.ID)
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

	jobs := listJobsByJobID(t, helper.clientset.(*fake.Clientset), evaluation.Resource.ID)
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

func TestRunEvaluationJobReturnsNilOnCreateFailure(t *testing.T) {
	// Unit test: RunEvaluationJob returns immediately; create failures happen in goroutines.
	t.Setenv("SERVICE_URL", "http://eval-hub")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	clientset := fake.NewSimpleClientset()
	clientset.PrependReactor("create", "configmaps", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewAlreadyExists(corev1.Resource("configmaps"), "eval-job-job-invalid-bench-1-spec")
	})

	runtime := &K8sRuntime{
		logger: logger,
		ctx:    context.Background(),
		helper: &KubernetesHelper{clientset: clientset},
		providers: map[string]api.ProviderResource{
			"lm_evaluation_harness": {
				Resource: api.Resource{ID: "lm_evaluation_harness"},
				ProviderConfigInternal: api.ProviderConfigInternal{
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
			Resource: api.Resource{ID: "job-invalid"},
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{
				URL:  "http://model",
				Name: "model",
			},
			Benchmarks: []api.BenchmarkConfig{
				{
					Ref:        api.Ref{ID: "bench-1"},
					ProviderID: "lm_evaluation_harness",
					Parameters: map[string]any{
						"num_examples": 1,
						"max_tokens":   64,
					},
				},
			},
		},
	}

	var storageNil = (*abstractions.Storage)(nil)
	if err := runtime.RunEvaluationJob(evaluation, storageNil); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if err := runtime.createBenchmarkResources(context.Background(), logger, evaluation, &evaluation.Benchmarks[0], 0); err == nil {
		t.Fatalf("expected create error but got nil")
	}
}

func TestDeleteEvaluationJobResourcesDeletesJobsAndConfigMaps(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	clientset := fake.NewSimpleClientset()
	runtime := &K8sRuntime{
		logger: logger,
		ctx:    context.Background(),
		helper: &KubernetesHelper{clientset: clientset},
	}

	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{ID: "job-delete"},
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Benchmarks: []api.BenchmarkConfig{
				{Ref: api.Ref{ID: "bench-1"}, ProviderID: "provider-1"},
				{Ref: api.Ref{ID: "bench-2"}, ProviderID: "provider-2"},
			},
		},
	}

	for range evaluation.Benchmarks {
		guid := uuid.NewString()
		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      jobName(evaluation.Resource.ID, guid),
				Namespace: defaultNamespace,
				Labels: map[string]string{
					labelJobIDKey: sanitizeLabelValue(evaluation.Resource.ID),
				},
			},
		}
		if _, err := clientset.BatchV1().Jobs(defaultNamespace).Create(context.Background(), job, metav1.CreateOptions{}); err != nil {
			t.Fatalf("failed to seed job: %v", err)
		}

		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMapName(evaluation.Resource.ID, guid),
				Namespace: defaultNamespace,
				Labels: map[string]string{
					labelJobIDKey: sanitizeLabelValue(evaluation.Resource.ID),
				},
			},
		}
		if _, err := clientset.CoreV1().ConfigMaps(defaultNamespace).Create(context.Background(), configMap, metav1.CreateOptions{}); err != nil {
			t.Fatalf("failed to seed configmap: %v", err)
		}
	}

	if err := runtime.DeleteEvaluationJobResources(evaluation); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	jobs := listJobsByJobID(t, clientset, evaluation.Resource.ID)
	configMaps := listConfigMapsByJobID(t, clientset, evaluation.Resource.ID)
	if len(jobs) != 0 {
		t.Fatalf("expected jobs to be deleted for %d benchmarks, got %d", len(evaluation.Benchmarks), len(jobs))
	}
	if len(configMaps) != 0 {
		t.Fatalf("expected configmaps to be deleted for %d benchmarks, got %d", len(evaluation.Benchmarks), len(configMaps))
	}
}

func TestDeleteEvaluationJobResourcesReturnsJoinedErrors(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	clientset := fake.NewSimpleClientset()
	errJob := errors.New("job delete failed")
	errConfig := errors.New("configmap delete failed")

	clientset.PrependReactor("delete", "jobs", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errJob
	})
	clientset.PrependReactor("delete", "configmaps", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errConfig
	})

	runtime := &K8sRuntime{
		logger: logger,
		ctx:    context.Background(),
		helper: &KubernetesHelper{clientset: clientset},
	}

	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{ID: "job-delete-errors"},
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Benchmarks: []api.BenchmarkConfig{
				{Ref: api.Ref{ID: "bench-1"}, ProviderID: "provider-1"},
				{Ref: api.Ref{ID: "bench-2"}, ProviderID: "provider-2"},
			},
		},
	}

	guid := uuid.NewString()
	_, errJobCreate := clientset.BatchV1().Jobs(defaultNamespace).Create(context.Background(), &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName(evaluation.Resource.ID, guid),
			Namespace: defaultNamespace,
			Labels: map[string]string{
				labelJobIDKey: sanitizeLabelValue(evaluation.Resource.ID),
			},
		},
	}, metav1.CreateOptions{})
	if errJobCreate != nil {
		t.Fatalf("failed to seed job: %v", errJobCreate)
	}
	_, errConfigCreate := clientset.CoreV1().ConfigMaps(defaultNamespace).Create(context.Background(), &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName(evaluation.Resource.ID, guid),
			Namespace: defaultNamespace,
			Labels: map[string]string{
				labelJobIDKey: sanitizeLabelValue(evaluation.Resource.ID),
			},
		},
	}, metav1.CreateOptions{})
	if errConfigCreate != nil {
		t.Fatalf("failed to seed configmap: %v", errConfigCreate)
	}

	err := runtime.DeleteEvaluationJobResources(evaluation)
	if err == nil {
		t.Fatalf("expected error but got nil")
	}
	if !errors.Is(err, errJob) {
		t.Fatalf("expected job delete error to be joined")
	}
	if !errors.Is(err, errConfig) {
		t.Fatalf("expected configmap delete error to be joined")
	}
}
