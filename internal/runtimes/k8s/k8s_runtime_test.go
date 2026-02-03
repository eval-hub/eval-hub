package k8s

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/pkg/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestRunEvaluationJobCreatesResources(t *testing.T) {
	// Integration test: creates one ConfigMap and Job per benchmark in a real cluster.
	if os.Getenv("K8S_INTEGRATION_TEST") != "1" {
		t.Skip("set K8S_INTEGRATION_TEST=1 to run against a real cluster")
	}
	t.Setenv("SERVICE_URL", "http://eval-hub")
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	helper, err := NewKubernetesHelper()
	if err != nil {
		t.Fatalf("failed to create kubernetes helper: %v", err)
	}
	jobID := "1936da05-2f27-4fd4-b000-ebcb71af1fbe"
	benchmarkID := "mmlu"
	benchmarkIDTwo := "arc"
	runtime := &K8sRuntime{
		logger: logger,
		helper: helper,
		providers: map[string]api.ProviderResource{
			"lm_evaluation_harness": {
				ProviderID: "lm_evaluation_harness",
				Runtime: &api.Runtime{
					K8s: &api.K8sRuntime{
						Image:       "docker.io/library/busybox:1.36",
						Entrypoint:  "/bin/sh",
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
	for _, id := range benchmarkIDs {
		configMapName := "eval-job-" + jobID + "-" + id + "-spec"
		jobName := "eval-job-" + jobID + "-" + id

		if _, err := helper.clientset.CoreV1().ConfigMaps(namespace).Get(context.Background(), configMapName, metav1.GetOptions{}); err != nil {
			t.Fatalf("expected configmap to be created for %s: %v", id, err)
		}
		if _, err := helper.clientset.BatchV1().Jobs(namespace).Get(context.Background(), jobName, metav1.GetOptions{}); err != nil {
			t.Fatalf("expected job to be created for %s: %v", id, err)
		}
	}
	for _, id := range benchmarkIDs {
		jobName := "eval-job-" + jobID + "-" + id
		configMapName := "eval-job-" + jobID + "-" + id + "-spec"
		_ = helper.clientset.BatchV1().Jobs(namespace).Delete(context.Background(), jobName, metav1.DeleteOptions{})
		_ = helper.clientset.CoreV1().ConfigMaps(namespace).Delete(context.Background(), configMapName, metav1.DeleteOptions{})
	}
}

func TestRunEvaluationJobReturnsErrorForInvalidConfig(t *testing.T) {
	// Unit test: validates config errors are surfaced and aggregated across benchmarks.
	t.Setenv("SERVICE_URL", "http://eval-hub")
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	runtime := &K8sRuntime{
		logger: logger,
		helper: nil,
		providers: map[string]api.ProviderResource{
			"lm_evaluation_harness": {
				ProviderID: "lm_evaluation_harness",
				Runtime: &api.Runtime{
					K8s: &api.K8sRuntime{
						Image: "",
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

	var storageNil = (*abstractions.Storage)(nil)
	err := runtime.RunEvaluationJob(evaluation, storageNil)
	if err == nil {
		t.Fatalf("expected error but got nil")
	}
	if !strings.Contains(err.Error(), "runtime adapter image is required") {
		t.Fatalf("expected runtime adapter image error, got %v", err)
	}
}
