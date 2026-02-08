package k8s

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/pkg/api"
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
	jobID := "1936da05-2f27-4fd4-b000-ebcb71af1fbe"
	benchmarkID := "arc_easy"
	benchmarkIDTwo := "arc"
	runtime := &K8sRuntime{
		logger: logger,
		helper: helper,
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

	namespace := "default"
	benchmarkIDs := []string{benchmarkID, benchmarkIDTwo}
	t.Cleanup(func() {
		propagationPolicy := metav1.DeletePropagationBackground
		deleteOptions := metav1.DeleteOptions{PropagationPolicy: &propagationPolicy}
		for _, id := range benchmarkIDs {
			jobName := jobName(jobID, id)
			configMapName := configMapName(jobID, id)
			ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
			_ = helper.clientset.BatchV1().Jobs(namespace).Delete(ctx, jobName, deleteOptions)
			_ = helper.clientset.CoreV1().ConfigMaps(namespace).Delete(ctx, configMapName, metav1.DeleteOptions{})
			cancel()
		}
	})
	for _, id := range benchmarkIDs {
		configMapName := configMapName(jobID, id)
		jobName := jobName(jobID, id)
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

	cmName := configMapName(evaluation.Resource.ID, evaluation.Benchmarks[0].ID)
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
