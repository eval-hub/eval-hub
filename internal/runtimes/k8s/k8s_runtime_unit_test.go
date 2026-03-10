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
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

type fakeStorage struct {
	logger        *slog.Logger
	called        bool
	ctx           context.Context
	runStatus     *api.StatusEvent
	runStatusChan chan *api.StatusEvent
	updateErr     error
	tenant        api.Tenant
	owner         api.User
}

// UpdateEvaluationJob implements [abstractions.Storage].
func (f *fakeStorage) UpdateEvaluationJob(id string, runStatus *api.StatusEvent) error {
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

func (f *fakeStorage) Ping(_ time.Duration) error { return nil }
func (f *fakeStorage) CreateEvaluationJob(_ *api.EvaluationJobResource) error {
	return nil
}
func (f *fakeStorage) GetEvaluationJob(_ string) (*api.EvaluationJobResource, error) {
	return nil, nil
}
func (f *fakeStorage) GetEvaluationJobs(_ *abstractions.QueryFilter) (*abstractions.QueryResults[api.EvaluationJobResource], error) {
	return nil, nil
}
func (f *fakeStorage) DeleteEvaluationJob(_ string) error {
	return nil
}
func (f *fakeStorage) UpdateEvaluationJobStatus(_ string, _ api.OverallState, _ *api.MessageInfo) error {
	f.called = true
	return nil
}
func (f *fakeStorage) CreateCollection(_ *api.CollectionResource) error {
	return nil
}
func (f *fakeStorage) GetCollection(_ string) (*api.CollectionResource, error) {
	return nil, nil
}
func (f *fakeStorage) GetCollections(_ *abstractions.QueryFilter) (*abstractions.QueryResults[api.CollectionResource], error) {
	return nil, nil
}
func (f *fakeStorage) UpdateCollection(_ *api.CollectionResource) error {
	return nil
}
func (f *fakeStorage) PatchCollection(_ string, _ *api.Patch) error {
	return nil
}
func (f *fakeStorage) DeleteCollection(_ string) error {
	return nil
}
func (f *fakeStorage) CreateProvider(_ *api.ProviderResource) error {
	return nil
}
func (f *fakeStorage) GetProvider(_ string) (*api.ProviderResource, error) {
	return nil, nil
}
func (f *fakeStorage) DeleteProvider(_ string) error {
	return nil
}
func (f *fakeStorage) GetProviders(_ *abstractions.QueryFilter) (*abstractions.QueryResults[api.ProviderResource], error) {
	return nil, nil
}
func (f *fakeStorage) UpdateProvider(_ string, _ *api.ProviderResource) (*api.ProviderResource, error) {
	return nil, nil
}
func (f *fakeStorage) PatchProvider(_ string, _ *api.Patch) (*api.ProviderResource, error) {
	return nil, nil
}
func (f *fakeStorage) Close() error { return nil }

func (f *fakeStorage) WithLogger(logger *slog.Logger) abstractions.Storage {
	return &fakeStorage{
		logger:        logger,
		ctx:           f.ctx,
		runStatusChan: f.runStatusChan,
		updateErr:     f.updateErr,
		tenant:        f.tenant,
		owner:         f.owner,
	}
}

func (f *fakeStorage) WithContext(ctx context.Context) abstractions.Storage {
	return &fakeStorage{
		logger:        f.logger,
		ctx:           ctx,
		runStatusChan: f.runStatusChan,
		updateErr:     f.updateErr,
		tenant:        f.tenant,
		owner:         f.owner,
	}
}

func (f *fakeStorage) WithTenant(tenant api.Tenant) abstractions.Storage {
	return &fakeStorage{
		logger:        f.logger,
		ctx:           f.ctx,
		runStatusChan: f.runStatusChan,
		updateErr:     f.updateErr,
		tenant:        tenant,
		owner:         f.owner,
	}
}

func (f *fakeStorage) WithOwner(owner api.User) abstractions.Storage {
	return &fakeStorage{
		logger:        f.logger,
		ctx:           f.ctx,
		runStatusChan: f.runStatusChan,
		updateErr:     f.updateErr,
		tenant:        f.tenant,
		owner:         owner,
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

	err := runtime.createBenchmarkResources(context.Background(), runtime.logger, evaluation, &evaluation.Benchmarks[0], 0)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	configMaps := listConfigMapsByJobID(t, clientset, evaluation.Resource.ID)
	if len(configMaps) != 1 {
		t.Fatalf("expected 1 configmap, got %d", len(configMaps))
	}
	cm := configMaps[0]
	if len(cm.OwnerReferences) != 1 {
		t.Fatalf("expected 1 owner reference, got %d", len(cm.OwnerReferences))
	}
	owner := cm.OwnerReferences[0]
	if owner.Kind != "Job" || owner.APIVersion != "batch/v1" {
		t.Fatalf("expected owner to be batch/v1 Job, got %s %s", owner.APIVersion, owner.Kind)
	}
	jobs := listJobsByJobID(t, clientset, evaluation.Resource.ID)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if owner.Name != jobs[0].Name {
		t.Fatalf("expected owner name to match job name, got %q", owner.Name)
	}
	if owner.Controller == nil || !*owner.Controller {
		t.Fatalf("expected owner reference to be controller")
	}
}

func TestCreateBenchmarkResourcesSetsAnnotations(t *testing.T) {
	t.Setenv("SERVICE_URL", "http://service.example")
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)

	clientset := fake.NewSimpleClientset()
	runtime := &K8sRuntime{
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper:    &KubernetesHelper{clientset: clientset},
		providers: sampleProviders(providerID),
	}

	err := runtime.createBenchmarkResources(context.Background(), runtime.logger, evaluation, &evaluation.Benchmarks[0], 0)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	configMaps := listConfigMapsByJobID(t, clientset, evaluation.Resource.ID)
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

	jobs := listJobsByJobID(t, clientset, evaluation.Resource.ID)
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

func TestCreateBenchmarkResourcesAddsModelAuthVolumeAndEnv(t *testing.T) {
	t.Setenv("SERVICE_URL", "http://service.example")
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)
	evaluation.Model.Auth = &api.ModelAuth{SecretRef: "model-auth-secret"}

	clientset := fake.NewSimpleClientset()
	runtime := &K8sRuntime{
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper:    &KubernetesHelper{clientset: clientset},
		providers: sampleProviders(providerID),
	}

	err := runtime.createBenchmarkResources(context.Background(), runtime.logger, evaluation, &evaluation.Benchmarks[0], 0)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	jobs := listJobsByJobID(t, clientset, evaluation.Resource.ID)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	job := jobs[0]
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

	envKeys := make(map[string]struct{}, len(container.Env))
	for _, env := range container.Env {
		envKeys[env.Name] = struct{}{}
	}
	legacyModelAuthKeys := []string{
		"MODEL_AUTH_API_KEY_PATH",
		"MODEL_AUTH_CA_CERT_PATH",
	}
	for _, key := range legacyModelAuthKeys {
		if _, found := envKeys[key]; found {
			t.Fatalf("expected env var %s to be absent", key)
		}
	}
}

func TestCreateBenchmarkResourcesAddsInitContainerForS3TestData(t *testing.T) {
	// Sobha TODO: fix this test please
	t.Skip("Sobha TODO: fix this test please")

	t.Setenv("SERVICE_URL", "http://service.example")
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)
	evaluation.Benchmarks[0].TestDataRef = &api.TestDataRef{
		S3: &api.S3TestDataRef{
			Bucket:    "bucket-1",
			Key:       "/a/b",
			SecretRef: "s3-secret",
		},
	}

	clientset := fake.NewSimpleClientset()
	runtime := &K8sRuntime{
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper:    &KubernetesHelper{clientset: clientset},
		providers: sampleProviders(providerID),
	}

	err := runtime.createBenchmarkResources(context.Background(), runtime.logger, evaluation, &evaluation.Benchmarks[0], 0)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	jobs := listJobsByJobID(t, clientset, evaluation.Resource.ID)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	job := jobs[0]
	if len(job.Spec.Template.Spec.InitContainers) != 1 {
		t.Fatalf("expected 1 init container, got %d", len(job.Spec.Template.Spec.InitContainers))
	}

	initContainer := job.Spec.Template.Spec.InitContainers[0]
	if initContainer.Name != initContainerName {
		t.Fatalf("expected init container name %q, got %q", initContainerName, initContainer.Name)
	}
	if len(initContainer.Command) != 1 || initContainer.Command[0] != defaultTestDataInitCmd {
		t.Fatalf("expected init container command %q, got %v", defaultTestDataInitCmd, initContainer.Command)
	}

	var foundBucketEnv, foundKeyEnv bool
	for _, env := range initContainer.Env {
		if env.Name == envTestDataS3BucketName {
			foundBucketEnv = true
			if env.Value != "bucket-1" {
				t.Fatalf("expected bucket env %q, got %q", "bucket-1", env.Value)
			}
		}
		if env.Name == envTestDataS3KeyName {
			foundKeyEnv = true
			if env.Value != "a/b" {
				t.Fatalf("expected key env %q, got %q", "a/b", env.Value)
			}
		}
	}
	if !foundBucketEnv || !foundKeyEnv {
		t.Fatalf("expected bucket/key env vars on init container")
	}

	var foundTestDataVolume, foundSecretVolume bool
	for _, volume := range job.Spec.Template.Spec.Volumes {
		if volume.Name == testDataVolumeName {
			foundTestDataVolume = true
		}
		if volume.Name == testDataSecretVolumeName {
			foundSecretVolume = true
			if volume.VolumeSource.Secret == nil || volume.VolumeSource.Secret.SecretName != "s3-secret" {
				t.Fatalf("expected secret volume %q with secret %q", testDataSecretVolumeName, "s3-secret")
			}
		}
	}
	if !foundTestDataVolume || !foundSecretVolume {
		t.Fatalf("expected test data and secret volumes to be present")
	}

	var foundInitMounts bool
	for _, mount := range initContainer.VolumeMounts {
		if mount.Name == testDataVolumeName && mount.MountPath == testDataMountPath {
			foundInitMounts = true
		}
	}
	if !foundInitMounts {
		t.Fatalf("expected init container to mount %s", testDataMountPath)
	}

	var foundAdapterMount bool
	for _, mount := range job.Spec.Template.Spec.Containers[0].VolumeMounts {
		if mount.Name == testDataVolumeName && mount.MountPath == testDataMountPath {
			foundAdapterMount = true
		}
	}
	if !foundAdapterMount {
		t.Fatalf("expected adapter container to mount %s", testDataMountPath)
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

	err := runtime.createBenchmarkResources(context.Background(), runtime.logger, evaluation, &evaluation.Benchmarks[0], 0)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	configMaps := listConfigMapsByJobID(t, clientset, evaluation.Resource.ID)
	if len(configMaps) != 0 {
		t.Fatalf("expected configmap to be deleted, got %d", len(configMaps))
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

	statusCh := make(chan *api.StatusEvent, 1)
	storage := &fakeStorage{logger: logger, ctx: context.Background(), runStatusChan: statusCh}
	var store abstractions.Storage = storage

	if err := runtime.RunEvaluationJob(evaluation, store); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	select {
	case runStatus := <-statusCh:
		if runStatus == nil {
			t.Fatalf("expected run status, got nil")
		}
		if runStatus.BenchmarkStatusEvent.Status != api.StateFailed {
			t.Fatalf("expected status failed, got %s", runStatus.BenchmarkStatusEvent.Status)
		}
		if runStatus.BenchmarkStatusEvent.ID != evaluation.Benchmarks[0].ID {
			t.Fatalf("expected benchmark ID %q, got %q", evaluation.Benchmarks[0].ID, runStatus.BenchmarkStatusEvent.ID)
		}
		if runStatus.BenchmarkStatusEvent.ProviderID != evaluation.Benchmarks[0].ProviderID {
			t.Fatalf("expected provider ID %q, got %q", evaluation.Benchmarks[0].ProviderID, runStatus.BenchmarkStatusEvent.ProviderID)
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

	statusCh := make(chan *api.StatusEvent, 1)
	storage := &fakeStorage{
		logger:        logger,
		ctx:           context.Background(),
		runStatusChan: statusCh,
		updateErr:     fmt.Errorf("update failed"),
	}
	var store abstractions.Storage = storage

	if err := runtime.RunEvaluationJob(evaluation, store); err != nil {
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

func TestRunEvaluationJobReturnsErrorWhenResolveBenchmarksFails(t *testing.T) {
	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-1"}},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model:      api.ModelRef{URL: "http://model.example", Name: "model-1"},
			Collection: &api.Ref{ID: "coll-1"},
			Benchmarks: nil,
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	runtime := &K8sRuntime{
		logger:    logger,
		helper:    &KubernetesHelper{clientset: fake.NewSimpleClientset()},
		providers: map[string]api.ProviderResource{},
		ctx:       context.Background(),
	}
	err := runtime.RunEvaluationJob(evaluation, nil)
	if err == nil {
		t.Fatal("expected error when ResolveBenchmarks fails (collection set, storage nil), got nil")
	}
	if err.Error() != "collection is set but storage is not available for job job-1" {
		t.Fatalf("expected ResolveBenchmarks error, got %q", err.Error())
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
			Experiment: &api.ExperimentConfig{
				Name: "exp-1",
			},
		},
	}
}

func sampleProviders(providerID string) map[string]api.ProviderResource {
	return map[string]api.ProviderResource{
		providerID: {
			Resource: api.Resource{ID: providerID},
			ProviderConfig: api.ProviderConfig{
				Runtime: &api.Runtime{
					K8s: &api.K8sRuntime{
						Image: "quay.io/evalhub/adapter:latest",
					},
				},
			},
		},
	}
}
