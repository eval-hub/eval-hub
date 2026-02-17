package k8s

import (
	"context"
	"errors"
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
	providerID := "lm_evaluation_harness"
	benchmarkID := "arc_easy"
	benchmarkIDTwo := "arc"
	runtime := &K8sRuntime{
		logger: logger,
		helper: helper,
		ctx:    context.Background(),
		providers: map[string]api.ProviderResource{
			"lm_evaluation_harness": {
				ID: "lm_evaluation_harness",
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

	benchmarkIDs := []string{benchmarkID, benchmarkIDTwo}
	t.Cleanup(func() {
		_ = runtime.DeleteEvaluationJobResources(evaluation)
	})
	namespace := "default"
	for _, id := range benchmarkIDs {
		configMapName := configMapName(jobID, providerID, id)
		jobName := jobName(jobID, providerID, id)
		found := false
		deadline := time.Now().Add(apiTimeout)
		for time.Now().Before(deadline) {
			if _, err := helper.clientset.CoreV1().ConfigMaps(namespace).Get(context.Background(), configMapName, metav1.GetOptions{}); err == nil {
				if _, err := helper.clientset.BatchV1().Jobs(namespace).Get(context.Background(), jobName, metav1.GetOptions{}); err == nil {
					found = true
					break
				}
			}
			time.Sleep(200 * time.Millisecond)
		}
		if !found {
			t.Fatalf("expected configmap/job to be created for %s", id)
		}
	}
}

func TestCreateBenchmarkResourcesReturnsErrorWhenConfigMapExists(t *testing.T) {
	// Unit test: resource creation fails if ConfigMap already exists.
	t.Setenv("SERVICE_URL", "http://eval-hub")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	clientset := fake.NewSimpleClientset()
	runtime := &K8sRuntime{
		logger: logger,
		helper: &KubernetesHelper{clientset: clientset},
		providers: map[string]api.ProviderResource{
			"lm_evaluation_harness": {
				ID: "lm_evaluation_harness",
				Runtime: &api.Runtime{
					K8s: &api.K8sRuntime{
						Image: "docker.io/library/busybox:1.36",
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

	cmName := configMapName(evaluation.Resource.ID, evaluation.Benchmarks[0].ProviderID, evaluation.Benchmarks[0].ID)
	_, err := clientset.CoreV1().ConfigMaps(defaultNamespace).Create(context.Background(), &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: defaultNamespace,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to seed configmap: %v", err)
	}

	if err := runtime.createBenchmarkResources(context.Background(), logger, evaluation, &evaluation.Benchmarks[0]); err == nil {
		t.Fatalf("expected error but got nil")
	} else if !apierrors.IsAlreadyExists(err) {
		t.Fatalf("expected already exists error, got %v", err)
	}
}

func TestCreateBenchmarkResourcesDuplicateBenchmarkIDCollides(t *testing.T) {
	// Integration test: repro name collision in a real cluster.
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
				ID: "lm_evaluation_harness",
				Runtime: &api.Runtime{
					K8s: &api.K8sRuntime{
						Image: "docker.io/library/busybox:1.36",
					},
				},
			},
			"lighteval": {
				ID: "lighteval",
				Runtime: &api.Runtime{
					K8s: &api.K8sRuntime{
						Image: "docker.io/library/busybox:1.36",
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

	firstJobName := jobName(evaluation.Resource.ID, evaluation.Benchmarks[0].ProviderID, evaluation.Benchmarks[0].ID)
	firstConfigMapName := configMapName(evaluation.Resource.ID, evaluation.Benchmarks[0].ProviderID, evaluation.Benchmarks[0].ID)
	secondJobName := jobName(evaluation.Resource.ID, evaluation.Benchmarks[1].ProviderID, evaluation.Benchmarks[1].ID)
	secondConfigMapName := configMapName(evaluation.Resource.ID, evaluation.Benchmarks[1].ProviderID, evaluation.Benchmarks[1].ID)
	t.Cleanup(func() {
		_ = runtime.helper.DeleteJob(context.Background(), defaultNamespace, firstJobName, metav1.DeleteOptions{})
		_ = runtime.helper.DeleteConfigMap(context.Background(), defaultNamespace, firstConfigMapName)
		_ = runtime.helper.DeleteJob(context.Background(), defaultNamespace, secondJobName, metav1.DeleteOptions{})
		_ = runtime.helper.DeleteConfigMap(context.Background(), defaultNamespace, secondConfigMapName)
	})

	if err := runtime.createBenchmarkResources(context.Background(), logger, evaluation, &evaluation.Benchmarks[0]); err != nil {
		t.Logf("first createBenchmarkResources error: %v", err)
		t.Fatalf("unexpected error creating first benchmark resources: %v", err)
	}

	if err := runtime.createBenchmarkResources(context.Background(), logger, evaluation, &evaluation.Benchmarks[1]); err != nil {
		t.Fatalf("unexpected error creating second benchmark resources: %v", err)
	}

	if _, err := helper.clientset.BatchV1().Jobs(defaultNamespace).Get(context.Background(), firstJobName, metav1.GetOptions{}); err != nil {
		t.Fatalf("expected first job to exist, got %v", err)
	}
	if _, err := helper.clientset.BatchV1().Jobs(defaultNamespace).Get(context.Background(), secondJobName, metav1.GetOptions{}); err != nil {
		t.Fatalf("expected second job to exist, got %v", err)
	}
	if _, err := helper.clientset.CoreV1().ConfigMaps(defaultNamespace).Get(context.Background(), firstConfigMapName, metav1.GetOptions{}); err != nil {
		t.Fatalf("expected first configmap to exist, got %v", err)
	}
	if _, err := helper.clientset.CoreV1().ConfigMaps(defaultNamespace).Get(context.Background(), secondConfigMapName, metav1.GetOptions{}); err != nil {
		t.Fatalf("expected second configmap to exist, got %v", err)
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
				ID: "lm_evaluation_harness",
				Runtime: &api.Runtime{
					K8s: &api.K8sRuntime{
						Image: "docker.io/library/busybox:1.36",
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

	if err := runtime.createBenchmarkResources(context.Background(), logger, evaluation, &evaluation.Benchmarks[0]); err == nil {
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

	for _, bench := range evaluation.Benchmarks {
		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      jobName(evaluation.Resource.ID, bench.ProviderID, bench.ID),
				Namespace: defaultNamespace,
			},
		}
		if _, err := clientset.BatchV1().Jobs(defaultNamespace).Create(context.Background(), job, metav1.CreateOptions{}); err != nil {
			t.Fatalf("failed to seed job: %v", err)
		}

		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMapName(evaluation.Resource.ID, bench.ProviderID, bench.ID),
				Namespace: defaultNamespace,
			},
		}
		if _, err := clientset.CoreV1().ConfigMaps(defaultNamespace).Create(context.Background(), configMap, metav1.CreateOptions{}); err != nil {
			t.Fatalf("failed to seed configmap: %v", err)
		}
	}

	if err := runtime.DeleteEvaluationJobResources(evaluation); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	for _, bench := range evaluation.Benchmarks {
		if _, err := clientset.BatchV1().Jobs(defaultNamespace).Get(context.Background(), jobName(evaluation.Resource.ID, bench.ProviderID, bench.ID), metav1.GetOptions{}); err == nil || !apierrors.IsNotFound(err) {
			t.Fatalf("expected job to be deleted for %s", bench.ID)
		}
		if _, err := clientset.CoreV1().ConfigMaps(defaultNamespace).Get(context.Background(), configMapName(evaluation.Resource.ID, bench.ProviderID, bench.ID), metav1.GetOptions{}); err == nil || !apierrors.IsNotFound(err) {
			t.Fatalf("expected configmap to be deleted for %s", bench.ID)
		}
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
