package k8s

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_hub/handlers"
	"github.com/eval-hub/eval-hub/pkg/api"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

type fakeStorage struct {
	logger            *slog.Logger
	called            bool
	ctx               context.Context
	runStatus         *api.StatusEvent
	runStatusChan     chan *api.StatusEvent
	updateErr         error
	tenant            api.Tenant
	owner             api.User
	providerConfigs   map[string]api.ProviderResource
	collectionConfigs map[string]api.CollectionResource
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
func (f *fakeStorage) GetCollection(id string) (*api.CollectionResource, error) {
	if cr, ok := f.collectionConfigs[id]; ok {
		return &cr, nil
	}
	return nil, fmt.Errorf("collection %q not found", id)
}
func (f *fakeStorage) GetCollections(_ *abstractions.QueryFilter) (*abstractions.QueryResults[api.CollectionResource], error) {
	return nil, nil
}
func (f *fakeStorage) UpdateCollection(_ string, _ *api.CollectionConfig) (*api.CollectionResource, error) {
	return nil, nil
}
func (f *fakeStorage) PatchCollection(_ string, _ *api.Patch) (*api.CollectionResource, error) {
	return nil, nil
}
func (f *fakeStorage) DeleteCollection(_ string) error {
	return nil
}
func (f *fakeStorage) CreateProvider(_ *api.ProviderResource) error {
	return nil
}
func (f *fakeStorage) GetProvider(id string) (*api.ProviderResource, error) {
	if pr, ok := f.providerConfigs[id]; ok {
		return &pr, nil
	}
	return nil, fmt.Errorf("provider %q not found", id)
}
func (f *fakeStorage) DeleteProvider(_ string) error {
	return nil
}
func (f *fakeStorage) GetProviders(_ *abstractions.QueryFilter) (*abstractions.QueryResults[api.ProviderResource], error) {
	return nil, nil
}
func (f *fakeStorage) UpdateProvider(_ string, _ *api.ProviderConfig) (*api.ProviderResource, error) {
	return nil, nil
}
func (f *fakeStorage) PatchProvider(_ string, _ *api.Patch) (*api.ProviderResource, error) {
	return nil, nil
}
func (f *fakeStorage) Close() error { return nil }
func (f *fakeStorage) LoadSystemResources(_ map[string]api.CollectionResource, _ map[string]api.ProviderResource) error {
	return nil
}

func (f *fakeStorage) WithLogger(logger *slog.Logger) abstractions.Storage {
	return &fakeStorage{
		logger:            logger,
		ctx:               f.ctx,
		runStatusChan:     f.runStatusChan,
		updateErr:         f.updateErr,
		tenant:            f.tenant,
		owner:             f.owner,
		providerConfigs:   f.providerConfigs,
		collectionConfigs: f.collectionConfigs,
	}
}

func (f *fakeStorage) WithContext(ctx context.Context) abstractions.Storage {
	return &fakeStorage{
		logger:            f.logger,
		ctx:               ctx,
		runStatusChan:     f.runStatusChan,
		updateErr:         f.updateErr,
		tenant:            f.tenant,
		owner:             f.owner,
		providerConfigs:   f.providerConfigs,
		collectionConfigs: f.collectionConfigs,
	}
}

func (f *fakeStorage) WithTenant(tenant api.Tenant) abstractions.Storage {
	return &fakeStorage{
		logger:            f.logger,
		ctx:               f.ctx,
		runStatusChan:     f.runStatusChan,
		updateErr:         f.updateErr,
		tenant:            tenant,
		owner:             f.owner,
		providerConfigs:   f.providerConfigs,
		collectionConfigs: f.collectionConfigs,
	}
}

func (f *fakeStorage) WithOwner(owner api.User) abstractions.Storage {
	return &fakeStorage{
		logger:            f.logger,
		ctx:               f.ctx,
		runStatusChan:     f.runStatusChan,
		updateErr:         f.updateErr,
		tenant:            f.tenant,
		owner:             owner,
		providerConfigs:   f.providerConfigs,
		collectionConfigs: f.collectionConfigs,
	}
}

func TestK8sRuntimeName(t *testing.T) {
	runtime := &K8sRuntime{}
	if runtime.Name() != "kubernetes" {
		t.Fatalf("expected Name to be kubernetes")
	}
}

func TestCreateBenchmarkResourcesSetsConfigMapOwner(t *testing.T) {
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)

	clientset := fake.NewClientset()
	runtime := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: &KubernetesHelper{clientset: clientset},
		serviceConfig: &config.Config{
			Service: &config.ServiceConfig{
				EvalInitImage: "eval-init-image",
			},
		},
	}

	storage := &fakeStorage{providerConfigs: sampleProviders(providerID)}
	err := runtime.createBenchmarkResources(context.Background(), runtime.logger, evaluation, &evaluation.Benchmarks[0], 0, storage)
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
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)

	clientset := fake.NewClientset()
	runtime := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: &KubernetesHelper{clientset: clientset},
		serviceConfig: &config.Config{
			Service: &config.ServiceConfig{
				EvalInitImage: "eval-init-image",
			},
		},
	}

	storage := &fakeStorage{providerConfigs: sampleProviders(providerID)}
	err := runtime.createBenchmarkResources(context.Background(), runtime.logger, evaluation, &evaluation.Benchmarks[0], 0, storage)
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
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)
	evaluation.Model.Auth = &api.ModelAuth{SecretRef: "model-auth-secret"}

	clientset := fake.NewClientset()
	runtime := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: &KubernetesHelper{clientset: clientset},
		serviceConfig: &config.Config{
			Service: &config.ServiceConfig{
				EvalInitImage: "eval-init-image",
			},
		},
	}

	storage := &fakeStorage{providerConfigs: sampleProviders(providerID)}
	err := runtime.createBenchmarkResources(context.Background(), runtime.logger, evaluation, &evaluation.Benchmarks[0], 0, storage)
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
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)
	evaluation.Benchmarks[0].TestDataRef = &api.TestDataRef{
		S3: &api.S3TestDataRef{
			Bucket:    "bucket-1",
			Key:       "/a/b",
			SecretRef: "s3-secret",
		},
	}

	clientset := fake.NewClientset()
	runtime := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: &KubernetesHelper{clientset: clientset},
		serviceConfig: &config.Config{
			Service: &config.ServiceConfig{
				EvalInitImage: "eval-init-image",
			},
		},
	}

	storage := &fakeStorage{providerConfigs: sampleProviders(providerID)}
	err := runtime.createBenchmarkResources(context.Background(), runtime.logger, evaluation, &evaluation.Benchmarks[0], 0, storage)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	jobs := listJobsByJobID(t, clientset, evaluation.Resource.ID)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	job := jobs[0]
	initContainer := findContainer(job.Spec.Template.Spec.InitContainers, initContainerName)
	if initContainer == nil {
		t.Fatal("expected test-data init container")
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
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)

	clientset := fake.NewClientset()
	clientset.PrependReactor("create", "jobs", func(action k8stesting.Action) (bool, k8sruntime.Object, error) {
		return true, nil, fmt.Errorf("job create failed")
	})

	runtime := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: &KubernetesHelper{clientset: clientset},
		serviceConfig: &config.Config{
			Service: &config.ServiceConfig{
				EvalInitImage: "eval-init-image",
			},
		},
	}

	storage := &fakeStorage{providerConfigs: sampleProviders(providerID)}
	err := runtime.createBenchmarkResources(context.Background(), runtime.logger, evaluation, &evaluation.Benchmarks[0], 0, storage)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	configMaps := listConfigMapsByJobID(t, clientset, evaluation.Resource.ID)
	if len(configMaps) != 0 {
		t.Fatalf("expected configmap to be deleted, got %d", len(configMaps))
	}
}

func TestCreateBenchmarkResourcesAppliesHardwareProfile(t *testing.T) {
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)
	evaluation.Benchmarks[0].HardwareConfig = &api.BenchmarkHardwareConfig{
		HardwareProfileRef: api.HardwareProfileRef{Name: "cpu-profile"},
	}

	profile := testHardwareProfileUnstructured("default", "cpu-profile")
	profile.Object["spec"] = map[string]any{
		"identifiers": []any{
			map[string]any{
				"identifier":   "cpu",
				"resourceType": "CPU",
				"defaultCount": int64(4),
				"maxCount":     int64(8),
			},
			map[string]any{
				"identifier":   "memory",
				"resourceType": "Memory",
				"defaultCount": "2Gi",
				"maxCount":     "4Gi",
			},
		},
	}

	clientset := fake.NewClientset()
	runtime := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: &KubernetesHelper{
			clientset:     clientset,
			dynamicClient: dynamicfake.NewSimpleDynamicClient(k8sruntime.NewScheme(), profile),
		},
		serviceConfig: &config.Config{
			Service: &config.ServiceConfig{EvalInitImage: "eval-init-image"},
		},
	}

	storage := &fakeStorage{providerConfigs: sampleProviders(providerID)}
	err := runtime.createBenchmarkResources(context.Background(), runtime.logger, evaluation, &evaluation.Benchmarks[0], 0, storage)
	if err != nil {
		t.Fatalf("createBenchmarkResources returned error: %v", err)
	}

	jobs := listJobsByJobID(t, clientset, evaluation.Resource.ID)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	adapter, err := adapterContainerFromJob(&jobs[0])
	if err != nil {
		t.Fatalf("adapter container: %v", err)
	}
	if cpu := adapter.Resources.Requests.Cpu().String(); cpu != "4" {
		t.Fatalf("cpu request = %q, want 4", cpu)
	}
	if memory := adapter.Resources.Requests.Memory().String(); memory != "2Gi" {
		t.Fatalf("memory request = %q, want 2Gi", memory)
	}
	if cpuLimit := adapter.Resources.Limits.Cpu().String(); cpuLimit != "8" {
		t.Fatalf("cpu limit = %q, want 8", cpuLimit)
	}
	if memoryLimit := adapter.Resources.Limits.Memory().String(); memoryLimit != "4Gi" {
		t.Fatalf("memory limit = %q, want 4Gi", memoryLimit)
	}
}

func TestCreateBenchmarkResourcesHardwareProfileUsesExplicitNamespace(t *testing.T) {
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)
	evaluation.Resource.Tenant = "tenant-a"
	evaluation.Benchmarks[0].HardwareConfig = &api.BenchmarkHardwareConfig{
		HardwareProfileRef: api.HardwareProfileRef{
			Name:      "cpu-profile",
			Namespace: "custom-ns",
		},
	}

	profile := testHardwareProfileUnstructured("custom-ns", "cpu-profile")
	clientset := fake.NewClientset()
	runtime := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: &KubernetesHelper{
			clientset:     clientset,
			dynamicClient: dynamicfake.NewSimpleDynamicClient(k8sruntime.NewScheme(), profile),
		},
		serviceConfig: &config.Config{
			Service: &config.ServiceConfig{EvalInitImage: "eval-init-image"},
		},
	}

	storage := &fakeStorage{providerConfigs: sampleProviders(providerID)}
	if err := runtime.createBenchmarkResources(context.Background(), runtime.logger, evaluation, &evaluation.Benchmarks[0], 0, storage); err != nil {
		t.Fatalf("createBenchmarkResources returned error: %v", err)
	}
}

func TestCreateBenchmarkResourcesHardwareProfileNotFound(t *testing.T) {
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)
	evaluation.Benchmarks[0].HardwareConfig = &api.BenchmarkHardwareConfig{
		HardwareProfileRef: api.HardwareProfileRef{Name: "missing-profile"},
	}

	clientset := fake.NewClientset()
	runtime := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: &KubernetesHelper{
			clientset:     clientset,
			dynamicClient: dynamicfake.NewSimpleDynamicClient(k8sruntime.NewScheme()),
		},
		serviceConfig: &config.Config{
			Service: &config.ServiceConfig{EvalInitImage: "eval-init-image"},
		},
	}

	storage := &fakeStorage{providerConfigs: sampleProviders(providerID)}
	err := runtime.createBenchmarkResources(context.Background(), runtime.logger, evaluation, &evaluation.Benchmarks[0], 0, storage)
	if err == nil {
		t.Fatal("expected error when hardware profile is missing")
	}
	if !strings.Contains(err.Error(), "fetch hardware profile") {
		t.Fatalf("expected fetch hardware profile error, got: %v", err)
	}
}

func TestCreateBenchmarkResourcesInvalidHardwareProfileSpec(t *testing.T) {
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)
	evaluation.Benchmarks[0].HardwareConfig = &api.BenchmarkHardwareConfig{
		HardwareProfileRef: api.HardwareProfileRef{Name: "bad-profile"},
	}

	profile := testHardwareProfileUnstructured("default", "bad-profile")
	profile.Object["spec"] = map[string]any{
		"identifiers": []any{
			map[string]any{
				"identifier":   "nvidia.com/gpu",
				"resourceType": "Accelerator",
				"defaultCount": "not-a-number",
			},
		},
	}

	clientset := fake.NewClientset()
	runtime := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: &KubernetesHelper{
			clientset:     clientset,
			dynamicClient: dynamicfake.NewSimpleDynamicClient(k8sruntime.NewScheme(), profile),
		},
		serviceConfig: &config.Config{
			Service: &config.ServiceConfig{EvalInitImage: "eval-init-image"},
		},
	}

	storage := &fakeStorage{providerConfigs: sampleProviders(providerID)}
	err := runtime.createBenchmarkResources(context.Background(), runtime.logger, evaluation, &evaluation.Benchmarks[0], 0, storage)
	if err == nil {
		t.Fatal("expected error when hardware profile spec is invalid")
	}
	if !strings.Contains(err.Error(), "parse hardware profile") {
		t.Fatalf("expected parse hardware profile error, got: %v", err)
	}
}

func TestCreateBenchmarkResourcesIgnoresEmptyHardwareProfileName(t *testing.T) {
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)
	evaluation.Benchmarks[0].HardwareConfig = &api.BenchmarkHardwareConfig{
		HardwareProfileRef: api.HardwareProfileRef{Name: "   "},
	}

	clientset := fake.NewClientset()
	runtime := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: &KubernetesHelper{
			clientset:     clientset,
			dynamicClient: dynamicfake.NewSimpleDynamicClient(k8sruntime.NewScheme()),
		},
		serviceConfig: &config.Config{
			Service: &config.ServiceConfig{EvalInitImage: "eval-init-image"},
		},
	}

	storage := &fakeStorage{providerConfigs: sampleProviders(providerID)}
	if err := runtime.createBenchmarkResources(context.Background(), runtime.logger, evaluation, &evaluation.Benchmarks[0], 0, storage); err != nil {
		t.Fatalf("createBenchmarkResources returned error: %v", err)
	}
}

func adapterContainerFromJob(job *batchv1.Job) (*corev1.Container, error) {
	for i := range job.Spec.Template.Spec.Containers {
		if job.Spec.Template.Spec.Containers[i].Name == adapterContainerName {
			return &job.Spec.Template.Spec.Containers[i], nil
		}
	}
	return nil, fmt.Errorf("adapter container not found in job %s", job.Name)
}

func TestRunEvaluationJobMarksBenchmarkFailedOnCreateError(t *testing.T) {
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)

	clientset := fake.NewSimpleClientset()
	clientset.PrependReactor("create", "configmaps", func(action k8stesting.Action) (bool, k8sruntime.Object, error) {
		return true, nil, fmt.Errorf("configmap create failed")
	})

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	runtime := &K8sRuntime{
		logger: logger,
		helper: &KubernetesHelper{clientset: clientset},
		ctx:    context.Background(),
		serviceConfig: &config.Config{
			Service: &config.ServiceConfig{
				EvalInitImage: "eval-init-image",
			},
		},
	}

	statusCh := make(chan *api.StatusEvent, 1)
	storage := &fakeStorage{logger: logger, ctx: context.Background(), runStatusChan: statusCh, providerConfigs: sampleProviders(providerID)}
	var store abstractions.Storage = storage

	benchmarks, err := handlers.GetJobBenchmarks(evaluation, nil)
	if err != nil {
		t.Fatalf("RunEvaluationJob failed to resolve benchmarks: %v", err)
	}

	if err := runtime.RunEvaluationJob(evaluation, benchmarks, store); err != nil {
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
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)

	clientset := fake.NewSimpleClientset()
	clientset.PrependReactor("create", "configmaps", func(action k8stesting.Action) (bool, k8sruntime.Object, error) {
		return true, nil, fmt.Errorf("configmap create failed")
	})

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	runtime := &K8sRuntime{
		logger: logger,
		helper: &KubernetesHelper{clientset: clientset},
		ctx:    context.Background(),
		serviceConfig: &config.Config{
			Service: &config.ServiceConfig{
				EvalInitImage: "eval-init-image",
			},
		},
	}

	statusCh := make(chan *api.StatusEvent, 1)
	storage := &fakeStorage{
		logger:          logger,
		ctx:             context.Background(),
		runStatusChan:   statusCh,
		updateErr:       fmt.Errorf("update failed"),
		providerConfigs: sampleProviders(providerID),
	}
	var store abstractions.Storage = storage

	benchmarks, err := handlers.GetJobBenchmarks(evaluation, nil)
	if err != nil {
		t.Fatalf("RunEvaluationJob failed to resolve benchmarks: %v", err)
	}

	if err := runtime.RunEvaluationJob(evaluation, benchmarks, store); err != nil {
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
			Benchmarks: []api.EvaluationBenchmarkConfig{
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
