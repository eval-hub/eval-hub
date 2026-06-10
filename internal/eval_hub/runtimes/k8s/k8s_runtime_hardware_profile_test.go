package k8s

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/handlers"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/google/uuid"
	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

func hardwareProfileCRDExists(t *testing.T, dynamicClient dynamic.Interface) bool {
	t.Helper()
	_, err := dynamicClient.Resource(schema.GroupVersionResource{
		Group:    "apiextensions.k8s.io",
		Version:  "v1",
		Resource: "customresourcedefinitions",
	}).Get(context.Background(), "hardwareprofiles.infrastructure.opendatahub.io", metav1.GetOptions{})
	return err == nil
}

func createTestHardwareProfile(t *testing.T, helper *KubernetesHelper, namespace, name string) {
	t.Helper()
	profile := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": hardwareProfileAPIGroup + "/" + hardwareProfileAPIVersion,
			"kind":       "HardwareProfile",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]any{
				"identifiers": []any{
					map[string]any{
						"identifier":   "cpu",
						"displayName":  "CPU",
						"resourceType": "CPU",
						"minCount":     int64(1),
						"defaultCount": int64(4),
						"maxCount":     int64(8),
					},
					map[string]any{
						"identifier":   "memory",
						"displayName":  "Memory",
						"resourceType": "Memory",
						"minCount":     "1Gi",
						"defaultCount": "2Gi",
						"maxCount":     "4Gi",
					},
				},
			},
		},
	}
	_, err := helper.dynamicClient.Resource(hardwareProfileGVR).Namespace(namespace).Create(context.Background(), profile, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create test hardware profile: %v", err)
	}
	t.Cleanup(func() {
		_ = helper.DeleteHardwareProfile(context.Background(), namespace, name)
	})
}

func TestRunEvaluationJobWithHardwareProfileCreatesResources(t *testing.T) {
	if os.Getenv("K8S_INTEGRATION_TEST") != "1" {
		t.Skip("set K8S_INTEGRATION_TEST=1 to run against a real cluster")
	}
	const apiTimeout = 15 * time.Second

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	helper, err := NewKubernetesHelper()
	if err != nil {
		t.Fatalf("failed to create kubernetes helper: %v", err)
	}
	if !hardwareProfileCRDExists(t, helper.dynamicClient) {
		t.Skip("hardwareprofiles.infrastructure.opendatahub.io CRD is not installed")
	}

	namespace := getTestNamespace(t)
	profileName := fmt.Sprintf("evalhub-test-hwp-%s", uuid.NewString()[:8])
	createTestHardwareProfile(t, helper, namespace, profileName)

	jobID := uuid.NewString()
	benchmarkID := "arc_easy"
	runtime := &K8sRuntime{
		logger: logger,
		helper: helper,
		ctx:    context.Background(),
	}
	providers := map[string]api.ProviderResource{
		"lm_evaluation_harness": {
			Resource: api.Resource{ID: "lm_evaluation_harness"},
			ProviderConfig: api.ProviderConfig{
				Runtime: &api.Runtime{
					K8s: &api.K8sRuntime{
						Image:         "docker.io/library/busybox:1.36",
						Entrypoint:    []string{"/bin/sh", "-c", "echo hello"},
						CPURequest:    "100m",
						MemoryRequest: "128Mi",
						CPULimit:      "500m",
						MemoryLimit:   "512Mi",
					},
				},
			},
		},
	}
	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{ID: jobID, Tenant: api.Tenant(namespace)},
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{
				URL:  "http://model",
				Name: "model",
			},
			Benchmarks: []api.EvaluationBenchmarkConfig{
				{
					Ref:        api.Ref{ID: benchmarkID},
					ProviderID: "lm_evaluation_harness",
					HardwareConfig: &api.BenchmarkHardwareConfig{
						HardwareProfileRef: profileName,
					},
				},
			},
		},
	}
	storage := &fakeStorage{providerConfigs: providers}

	benchmarks, err := handlers.GetJobBenchmarks(evaluation, nil)
	if err != nil {
		t.Fatalf("GetJobBenchmarks failed: %v", err)
	}
	if err := runtime.RunEvaluationJob(evaluation, benchmarks, storage); err != nil {
		t.Fatalf("RunEvaluationJob returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = runtime.DeleteEvaluationJobResources(evaluation)
	})

	labelSelector := fmt.Sprintf("%s=%s", labelJobIDKey, sanitizeLabelValue(jobID))
	var createdJob *batchv1.Job
	deadline := time.Now().Add(apiTimeout)
	for time.Now().Before(deadline) {
		jobs, err := helper.ListJobs(context.Background(), namespace, labelSelector)
		if err != nil {
			t.Fatalf("failed to list jobs: %v", err)
		}
		if len(jobs) == 1 {
			createdJob = &jobs[0]
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if createdJob == nil {
		t.Fatal("timed out waiting for evaluation job to be created")
	}
	if len(createdJob.Spec.Template.Spec.Containers) == 0 {
		t.Fatal("expected adapter container in created job")
	}
	requests := createdJob.Spec.Template.Spec.Containers[0].Resources.Requests
	if cpu := requests.Cpu().String(); cpu != "4" {
		t.Fatalf("adapter cpu request = %q, want 4", cpu)
	}
	if memory := requests.Memory().String(); memory != "2Gi" {
		t.Fatalf("adapter memory request = %q, want 2Gi", memory)
	}
}

func TestGetHardwareProfileNotFound(t *testing.T) {
	if os.Getenv("K8S_INTEGRATION_TEST") != "1" {
		t.Skip("set K8S_INTEGRATION_TEST=1 to run against a real cluster")
	}
	helper, err := NewKubernetesHelper()
	if err != nil {
		t.Fatalf("failed to create kubernetes helper: %v", err)
	}
	if !hardwareProfileCRDExists(t, helper.dynamicClient) {
		t.Skip("hardwareprofiles.infrastructure.opendatahub.io CRD is not installed")
	}

	namespace := getTestNamespace(t)
	_, err = helper.GetHardwareProfile(context.Background(), namespace, "missing-hardware-profile")
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected not found error, got: %v", err)
	}
}
