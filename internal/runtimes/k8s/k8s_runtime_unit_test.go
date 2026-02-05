package k8s

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/pkg/api"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

type fakeStorage struct {
	logger        *slog.Logger
	called        bool
	ctx           context.Context
	runStatus     *api.RunStatusInternal
	runStatusChan chan *api.RunStatusInternal
	updateErr     error
}

// UpdateEvaluationJob implements [abstractions.Storage].
func (f *fakeStorage) UpdateEvaluationJob(id string, runStatus *api.RunStatusInternal) error {
	f.called = true
	f.runStatus = runStatus
	if f.runStatusChan != nil {
		select {
		case f.runStatusChan <- runStatus:
		default:
		}
	}
	return f.updateErr
}

func (f *fakeStorage) GetDatasourceName() string  { return "fake" }
func (f *fakeStorage) Ping(_ time.Duration) error { return nil }
func (f *fakeStorage) CreateEvaluationJob(_ *api.EvaluationJobConfig) (*api.EvaluationJobResource, error) {
	return nil, nil
}
func (f *fakeStorage) GetEvaluationJob(_ string) (*api.EvaluationJobResource, error) {
	return nil, nil
}
func (f *fakeStorage) GetEvaluationJobs(int, _ int, _ string) (*abstractions.QueryResults[api.EvaluationJobResource], error) {
	return nil, nil
}
func (f *fakeStorage) DeleteEvaluationJob(_ string, _ bool) error {
	return nil
}
func (f *fakeStorage) UpdateEvaluationJobStatus(_ string, _ *api.StatusEvent) error {
	f.called = true
	return nil
}
func (f *fakeStorage) CreateCollection(_ *api.CollectionResource) error {
	return nil
}
func (f *fakeStorage) GetCollection(_ string, _ bool) (*api.CollectionResource, error) {
	return nil, nil
}
func (f *fakeStorage) GetCollections(_ int, _ int) (*abstractions.QueryResults[api.CollectionResource], error) {
	return nil, nil
}
func (f *fakeStorage) UpdateCollection(_ *api.CollectionResource) error {
	return nil
}
func (f *fakeStorage) DeleteCollection(_ string) error {
	return nil
}
func (f *fakeStorage) Close() error { return nil }

func (f *fakeStorage) WithLogger(logger *slog.Logger) abstractions.Storage {
	return &fakeStorage{
		logger:        logger,
		ctx:           f.ctx,
		runStatusChan: f.runStatusChan,
		updateErr:     f.updateErr,
	}
}
func (f *fakeStorage) WithContext(ctx context.Context) abstractions.Storage {
	return &fakeStorage{
		logger:        f.logger,
		ctx:           ctx,
		runStatusChan: f.runStatusChan,
		updateErr:     f.updateErr,
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

	err := runtime.createBenchmarkResources(context.Background(), runtime.logger, evaluation, &evaluation.Benchmarks[0])
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

	err := runtime.createBenchmarkResources(context.Background(), runtime.logger, evaluation, &evaluation.Benchmarks[0])
	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	cmName := configMapName(evaluation.Resource.ID, evaluation.Benchmarks[0].ID)
	_, err = clientset.CoreV1().ConfigMaps(defaultNamespace).Get(context.Background(), cmName, metav1.GetOptions{})
	if err == nil || !apierrors.IsNotFound(err) {
		t.Fatalf("expected configmap to be deleted, got %v", err)
	}
}

func TestRunEvaluationJobMarksBenchmarkFailedOnCreateError(t *testing.T) {
	t.Setenv("SERVICE_URL", "http://service.example")
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)

	clientset := fake.NewSimpleClientset()
	clientset.PrependReactor("create", "configmaps", func(action k8stesting.Action) (bool, k8sruntime.Object, error) {
		return true, nil, fmt.Errorf("configmap create failed")
	})

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	runtime := &K8sRuntime{
		logger:    logger,
		helper:    &KubernetesHelper{clientset: clientset},
		providers: sampleProviders(providerID),
		ctx:       context.Background(),
	}

	statusCh := make(chan *api.RunStatusInternal, 1)
	storage := &fakeStorage{logger: logger, ctx: context.Background(), runStatusChan: statusCh}
	var store abstractions.Storage = storage

	if err := runtime.RunEvaluationJob(evaluation, &store); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	select {
	case runStatus := <-statusCh:
		if runStatus == nil {
			t.Fatalf("expected run status, got nil")
		}
		if runStatus.StatusEvent.Status != api.StateFailed {
			t.Fatalf("expected status failed, got %s", runStatus.StatusEvent.Status)
		}
		if runStatus.StatusEvent.BenchmarkID != evaluation.Benchmarks[0].ID {
			t.Fatalf("expected benchmark ID %q, got %q", evaluation.Benchmarks[0].ID, runStatus.StatusEvent.BenchmarkID)
		}
		if runStatus.StatusEvent.ProviderID != evaluation.Benchmarks[0].ProviderID {
			t.Fatalf("expected provider ID %q, got %q", evaluation.Benchmarks[0].ProviderID, runStatus.StatusEvent.ProviderID)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("expected UpdateEvaluationJob to be called")
	}
}

func TestRunEvaluationJobHandlesUpdateFailure(t *testing.T) {
	t.Setenv("SERVICE_URL", "http://service.example")
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)

	clientset := fake.NewSimpleClientset()
	clientset.PrependReactor("create", "configmaps", func(action k8stesting.Action) (bool, k8sruntime.Object, error) {
		return true, nil, fmt.Errorf("configmap create failed")
	})

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	runtime := &K8sRuntime{
		logger:    logger,
		helper:    &KubernetesHelper{clientset: clientset},
		providers: sampleProviders(providerID),
		ctx:       context.Background(),
	}

	statusCh := make(chan *api.RunStatusInternal, 1)
	storage := &fakeStorage{
		logger:        logger,
		ctx:           context.Background(),
		runStatusChan: statusCh,
		updateErr:     fmt.Errorf("update failed"),
	}
	var store abstractions.Storage = storage

	if err := runtime.RunEvaluationJob(evaluation, &store); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	select {
	case runStatus := <-statusCh:
		if runStatus == nil {
			t.Fatalf("expected run status, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("expected UpdateEvaluationJob to be called")
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
