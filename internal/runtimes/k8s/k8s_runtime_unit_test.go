package k8s

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"github.com/eval-hub/eval-hub/internal/http_wrappers"
	"github.com/eval-hub/eval-hub/pkg/api"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

type fakeStorage struct {
	called bool
}

func (f *fakeStorage) GetDatasourceName() string  { return "fake" }
func (f *fakeStorage) Ping(_ time.Duration) error { return nil }
func (f *fakeStorage) CreateEvaluationJob(_ *executioncontext.ExecutionContext, _ *api.EvaluationJobConfig) (*api.EvaluationJobResource, error) {
	return nil, nil
}
func (f *fakeStorage) GetEvaluationJob(_ *executioncontext.ExecutionContext, _ string) (*api.EvaluationJobResource, error) {
	return nil, nil
}
func (f *fakeStorage) GetEvaluationJobs(_ *executioncontext.ExecutionContext, _ http_wrappers.RequestWrapper, _ int, _ int, _ string) (*api.EvaluationJobResourceList, error) {
	return nil, nil
}
func (f *fakeStorage) DeleteEvaluationJob(_ *executioncontext.ExecutionContext, _ string, _ bool) error {
	return nil
}
func (f *fakeStorage) UpdateEvaluationJobStatus(_ *executioncontext.ExecutionContext, _ string, _ *api.EvaluationJobStatus) error {
	f.called = true
	return nil
}
func (f *fakeStorage) CreateCollection(_ *executioncontext.ExecutionContext, _ *api.CollectionResource) error {
	return nil
}
func (f *fakeStorage) GetCollection(_ *executioncontext.ExecutionContext, _ string, _ bool) (*api.CollectionResource, error) {
	return nil, nil
}
func (f *fakeStorage) GetCollections(_ *executioncontext.ExecutionContext, _ int, _ int) (*api.CollectionResourceList, error) {
	return nil, nil
}
func (f *fakeStorage) UpdateCollection(_ *executioncontext.ExecutionContext, _ *api.CollectionResource) error {
	return nil
}
func (f *fakeStorage) DeleteCollection(_ *executioncontext.ExecutionContext, _ string) error {
	return nil
}
func (f *fakeStorage) Close() error { return nil }

func TestPersistJobFailureNoStorage(t *testing.T) {
	runtime := &K8sRuntime{}
	runtime.persistJobFailure(nil, nil, context.Canceled)
}

func TestPersistJobFailureUpdatesStatus(t *testing.T) {
	storage := &fakeStorage{}
	var store abstractions.Storage = storage
	runtime := &K8sRuntime{logger: nil}
	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{ID: "job-1"},
		},
	}
	runtime.persistJobFailure(&store, evaluation, context.Canceled)
	if !storage.called {
		t.Fatalf("expected UpdateEvaluationJobStatus to be called")
	}
}

func TestK8sRuntimeName(t *testing.T) {
	runtime := &K8sRuntime{}
	if runtime.Name() != "kubernetes" {
		t.Fatalf("expected Name to be kubernetes")
	}
}

func TestCreateBenchmarkResourcesSetsConfigMapOwner(t *testing.T) {
	t.Setenv("SERVICE_URL", "http://service.example")
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)

	clientset := fake.NewSimpleClientset()
	runtime := &K8sRuntime{
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper:    &KubernetesHelper{clientset: clientset},
		providers: sampleProviders(providerID),
	}

	err := runtime.createBenchmarkResources(context.Background(), evaluation, &evaluation.Benchmarks[0])
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	cmName := configMapName(evaluation.Resource.ID, evaluation.Benchmarks[0].ID)
	cm, err := clientset.CoreV1().ConfigMaps(defaultNamespace).Get(context.Background(), cmName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected configmap to exist, got %v", err)
	}
	if len(cm.OwnerReferences) != 1 {
		t.Fatalf("expected 1 owner reference, got %d", len(cm.OwnerReferences))
	}
	owner := cm.OwnerReferences[0]
	if owner.Kind != "Job" || owner.APIVersion != "batch/v1" {
		t.Fatalf("expected owner to be batch/v1 Job, got %s %s", owner.APIVersion, owner.Kind)
	}
	if owner.Name != jobName(evaluation.Resource.ID, evaluation.Benchmarks[0].ID) {
		t.Fatalf("expected owner name to match job name, got %q", owner.Name)
	}
	if owner.Controller == nil || !*owner.Controller {
		t.Fatalf("expected owner reference to be controller")
	}
}

func TestCreateBenchmarkResourcesDeletesConfigMapOnJobFailure(t *testing.T) {
	t.Setenv("SERVICE_URL", "http://service.example")
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)

	clientset := fake.NewSimpleClientset()
	clientset.PrependReactor("create", "jobs", func(action k8stesting.Action) (bool, k8sruntime.Object, error) {
		return true, nil, fmt.Errorf("job create failed")
	})

	runtime := &K8sRuntime{
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper:    &KubernetesHelper{clientset: clientset},
		providers: sampleProviders(providerID),
	}

	err := runtime.createBenchmarkResources(context.Background(), evaluation, &evaluation.Benchmarks[0])
	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	cmName := configMapName(evaluation.Resource.ID, evaluation.Benchmarks[0].ID)
	_, err = clientset.CoreV1().ConfigMaps(defaultNamespace).Get(context.Background(), cmName, metav1.GetOptions{})
	if err == nil || !apierrors.IsNotFound(err) {
		t.Fatalf("expected configmap to be deleted, got %v", err)
	}
}

func sampleEvaluation(providerID string) *api.EvaluationJobResource {
	return &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{ID: "job-1"},
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{
				URL:  "http://model.example",
				Name: "model-1",
			},
			Benchmarks: []api.BenchmarkConfig{
				{
					Ref: api.Ref{ID: "bench-1"},
					Parameters: map[string]any{
						"foo":          "bar",
						"num_examples": 5,
					},
					ProviderID: providerID,
				},
			},
			Experiment: api.ExperimentConfig{
				Name: "exp-1",
			},
		},
	}
}

func sampleProviders(providerID string) map[string]api.ProviderResource {
	return map[string]api.ProviderResource{
		providerID: {
			ProviderID: providerID,
			Runtime: &api.Runtime{
				K8s: &api.K8sRuntime{
					Image: "quay.io/eval-hub/adapter:latest",
				},
			},
		},
	}
}
