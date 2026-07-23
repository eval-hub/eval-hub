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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
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

func TestBuildInternalModelRefSecretMultiModel(t *testing.T) {
	// Multi-model credential secret: *_api-key keys become refs, *_url keys become the sidecar
	// proxy URL with the original path preserved, ca_cert is excluded (projected directly
	// from the real secret into the adapter).
	data := map[string][]byte{
		"model-1_api-key": []byte("sk-model1"),
		"model-1_url":     []byte("https://api.openai.com/v1"),
		"model-2_api-key": []byte("sk-model2"),
		"model-2_url":     []byte("https://azure.example.com/v1"),
		"judge_url":       []byte("https://my-vllm.example.com/v1"),
		"attacker_url":    []byte("https://attacker.example.com"),
		"ca_cert":         []byte("-----BEGIN CERTIFICATE-----"),
	}
	clientset := fake.NewClientset()
	helper := &KubernetesHelper{clientset: clientset}

	const sidecarURL = "http://localhost:8080"
	secret, err := buildInternalModelRefSecret(context.Background(), "default", "multi-model-ref", data, sidecarURL, nil, helper)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cases := map[string]string{
		"model-1_api-key": "model-1_api-key:ref",
		"model-2_api-key": "model-2_api-key:ref",
		"model-1_url":     "http://localhost:8080/v1",
		"model-2_url":     "http://localhost:8080/v1",
		"judge_url":       "http://localhost:8080/v1",
		"attacker_url":    "http://localhost:8080",
	}
	for k, want := range cases {
		got := string(secret.Data[k])
		if got != want {
			t.Errorf("key %q: want %q, got %q", k, want, got)
		}
	}
	if _, ok := secret.Data["ca_cert"]; ok {
		t.Error("ca_cert must not appear in the internalModelRef secret")
	}
}

func TestBuildInternalModelRefSecretInvalidURL(t *testing.T) {
	data := map[string][]byte{
		"judge_url": []byte("://bad"),
	}
	helper := &KubernetesHelper{clientset: fake.NewClientset()}
	_, err := buildInternalModelRefSecret(context.Background(), "default", "bad-url-ref", data, "http://localhost:8080", nil, helper)
	if err == nil {
		t.Fatal("expected error for invalid *_url value")
	}
}

func TestInspectModelSecretPassthroughOnly(t *testing.T) {
	// Passthrough-only secret (ca_cert + unknown key): inspectModelSecret must report
	// hasCredentialKeys=false so no internalModelRef secret is created and no model proxy starts.
	realSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "tls-only-creds", Namespace: "default"},
		Data: map[string][]byte{
			"ca_cert":        []byte("-----BEGIN CERTIFICATE-----"),
			"some-other-key": []byte("value"),
		},
	}
	clientset := fake.NewClientset(realSecret)
	helper := &KubernetesHelper{clientset: clientset}

	info, err := inspectModelSecret(context.Background(), "default", "tls-only-creds", helper)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.hasCredentialKeys {
		t.Fatal("expected hasCredentialKeys=false for passthrough-only (ca_cert-only) secret")
	}
}

func TestIsModelCredentialKey(t *testing.T) {
	tests := []struct {
		key  string
		want bool
	}{
		{"api-key", true},
		{"model-1_api-key", true},
		{"model-1_url", true},
		{"kfp_sa_token", true},
		{"svc_sa_token", true},
		{"kfp_token", false},
		{"hf-token", false},
		{"ca_cert", false},
		{"some-other-key", false},
	}
	for _, tt := range tests {
		got := isModelCredentialKey(tt.key)
		if got != tt.want {
			t.Errorf("isModelCredentialKey(%q) = %v, want %v", tt.key, got, tt.want)
		}
	}
}

func TestBuildInternalModelRefSecretWithSATokenSuffix(t *testing.T) {
	// buildInternalModelRefSecret maps *_sa_token keys to <key>:ref regardless of the input
	// value (including empty). The adapter sends "Authorization: Bearer kfp_sa_token:ref";
	// empty-value SA injection is handled by the sidecar model proxy, not this layer.
	data := map[string][]byte{
		"kfp_sa_token": []byte(""), // empty is preserved through ref mapping; sidecar injects SA at runtime
		"kfp_url":      []byte("https://kfp-svc"),
	}
	clientset := fake.NewClientset()
	helper := &KubernetesHelper{clientset: clientset}

	const sidecarURL = "http://localhost:8080"
	secret, err := buildInternalModelRefSecret(context.Background(), "default", "kfp-ref", data, sidecarURL, nil, helper)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := string(secret.Data["kfp_sa_token"]); got != "kfp_sa_token:ref" {
		t.Errorf("kfp_sa_token: want %q, got %q", "kfp_sa_token:ref", got)
	}
	if got := string(secret.Data["kfp_url"]); got != sidecarURL {
		t.Errorf("kfp_url: want %q, got %q", sidecarURL, got)
	}
}

// TestCreateBenchmarkResourcesPassthroughOnlyModelAuth verifies that when the model credential
// secret contains only ca_cert (SA token auth, no api-key), the sidecar proxy is still started:
// the adapter URL is redirected to the sidecar, the sidecar gets the real secret mount for TLS,
// TestCreateBenchmarkResourcesOpenModelRoutedThroughSidecar verifies that even when no
// model.auth is configured, the adapter URL is redirected to the sidecar and the sidecar

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

func TestCreateBenchmarkResourcesMountsPVCTestData(t *testing.T) {
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)
	evaluation.Benchmarks[0].TestDataRef = &api.TestDataRef{
		PVC: &api.PVCTestDataRef{
			ClaimName: "eval-datasets-pvc",
			SubPath:   "benchmark-a/v1",
		},
	}

	clientset := fake.NewClientset()
	runtime := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: &KubernetesHelper{clientset: clientset},
		serviceConfig: &config.Config{
			Service: &config.ServiceConfig{},
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

	// No data-fetch init container expected for PVC-backed jobs (sidecar runs as native init container).
	if findContainer(job.Spec.Template.Spec.InitContainers, initContainerName) != nil {
		t.Fatal("expected no data-fetch init container for PVC test data")
	}

	// PVC volume must be present with correct claimName and readOnly.
	var foundPVCVolume bool
	for _, volume := range job.Spec.Template.Spec.Volumes {
		if volume.Name == testDataVolumeName {
			if volume.VolumeSource.PersistentVolumeClaim == nil {
				t.Fatalf("expected PVC volume source for %q", testDataVolumeName)
			}
			if volume.VolumeSource.PersistentVolumeClaim.ClaimName != "eval-datasets-pvc" {
				t.Fatalf("expected claimName %q, got %q", "eval-datasets-pvc", volume.VolumeSource.PersistentVolumeClaim.ClaimName)
			}
			if !volume.VolumeSource.PersistentVolumeClaim.ReadOnly {
				t.Fatal("expected PVC volume to be read-only")
			}
			foundPVCVolume = true
		}
	}
	if !foundPVCVolume {
		t.Fatalf("expected PVC volume %q in pod spec", testDataVolumeName)
	}

	// Adapter container must have read-only mount at /test_data with sub_path.
	var foundAdapterMount bool
	for _, mount := range job.Spec.Template.Spec.Containers[0].VolumeMounts {
		if mount.Name == testDataVolumeName && mount.MountPath == testDataMountPath {
			if !mount.ReadOnly {
				t.Fatal("expected adapter test-data mount to be read-only")
			}
			if mount.SubPath != "benchmark-a/v1" {
				t.Fatalf("expected SubPath %q, got %q", "benchmark-a/v1", mount.SubPath)
			}
			foundAdapterMount = true
		}
	}
	if !foundAdapterMount {
		t.Fatalf("expected adapter container to mount %s", testDataMountPath)
	}
}

func TestCreateBenchmarkResourcesMountsPVCTestDataNoSubPath(t *testing.T) {
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)
	evaluation.Benchmarks[0].TestDataRef = &api.TestDataRef{
		PVC: &api.PVCTestDataRef{
			ClaimName: "my-pvc",
		},
	}

	clientset := fake.NewClientset()
	runtime := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: &KubernetesHelper{clientset: clientset},
		serviceConfig: &config.Config{
			Service: &config.ServiceConfig{},
		},
	}

	storage := &fakeStorage{providerConfigs: sampleProviders(providerID)}
	err := runtime.createBenchmarkResources(context.Background(), runtime.logger, evaluation, &evaluation.Benchmarks[0], 0, storage)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	jobs := listJobsByJobID(t, clientset, evaluation.Resource.ID)
	job := jobs[0]

	if findContainer(job.Spec.Template.Spec.InitContainers, initContainerName) != nil {
		t.Fatal("expected no data-fetch init container for PVC test data")
	}

	for _, mount := range job.Spec.Template.Spec.Containers[0].VolumeMounts {
		if mount.Name == testDataVolumeName {
			if mount.SubPath != "" {
				t.Fatalf("expected empty SubPath when not set, got %q", mount.SubPath)
			}
			return
		}
	}
	t.Fatalf("expected adapter container to mount %s", testDataMountPath)
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
		if runStatus.BenchmarkStatusEvent.ErrorMessage == nil {
			t.Fatal("expected error message on failed benchmark status")
		}
		if runStatus.BenchmarkStatusEvent.ErrorMessage.MessageOrigin != api.MessageOriginServer {
			t.Fatalf("expected server error origin, got %q", runStatus.BenchmarkStatusEvent.ErrorMessage.MessageOrigin)
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

// TestCreateBenchmarkResourcesDeletesOrphanedJobWhenConfigMapDeletedMidCreation verifies
// that when SetConfigMapOwner fails with NotFound (the ConfigMap was hard-deleted by a
// concurrent DELETE request between Job creation and owner-reference setup), the orphaned
// K8s Job is cleaned up so it cannot remain stuck in a permanent FailedMount loop.
func TestCreateBenchmarkResourcesDeletesOrphanedJobWhenConfigMapDeletedMidCreation(t *testing.T) {
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)

	clientset := fake.NewClientset()
	// Simulate the race: ConfigMap GET inside SetConfigMapOwner returns NotFound,
	// as if the ConfigMap was deleted by a concurrent hard_delete after Job creation.
	clientset.PrependReactor("get", "configmaps", func(action k8stesting.Action) (bool, k8sruntime.Object, error) {
		return true, nil, apierrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, action.(k8stesting.GetAction).GetName())
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
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// The orphaned K8s Job must be deleted to prevent it getting stuck in FailedMount.
	jobs := listJobsByJobID(t, clientset, evaluation.Resource.ID)
	if len(jobs) != 0 {
		t.Fatalf("expected orphaned job to be deleted, got %d job(s)", len(jobs))
	}
}

func TestCreateBenchmarkResourcesReturnsErrorWhenOrphanedJobDeletionFails(t *testing.T) {
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)

	clientset := fake.NewClientset()
	clientset.PrependReactor("get", "configmaps", func(action k8stesting.Action) (bool, k8sruntime.Object, error) {
		return true, nil, apierrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, action.(k8stesting.GetAction).GetName())
	})
	clientset.PrependReactor("delete", "jobs", func(action k8stesting.Action) (bool, k8sruntime.Object, error) {
		return true, nil, fmt.Errorf("job delete failed")
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
		t.Fatal("expected error when orphaned job deletion fails, got nil")
	}
}

func TestDeleteEvaluationJobResourcesDeletesRefSecrets(t *testing.T) {
	evaluation := sampleEvaluation("provider-1")
	jobID := evaluation.Resource.ID
	namespace := "default"
	labelKey := labelJobIDKey
	labelVal := sanitizeLabelValue(jobID)

	// Pre-create a Job, ConfigMap, and ref Secret all carrying the job-ID label,
	// as they would exist after a successful createBenchmarkResources call.
	clientset := fake.NewClientset(
		&batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "eval-job-1",
				Namespace: namespace,
				Labels:    map[string]string{labelKey: labelVal},
			},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "eval-cm-1",
				Namespace: namespace,
				Labels:    map[string]string{labelKey: labelVal},
			},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "eval-ref-secret-1",
				Namespace: namespace,
				Labels:    map[string]string{labelKey: labelVal},
			},
		},
	)

	runtime := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: &KubernetesHelper{clientset: clientset},
		ctx:    context.Background(),
	}

	err := runtime.DeleteEvaluationJobResources(evaluation)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	secrets, listErr := clientset.CoreV1().Secrets(namespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", labelKey, labelVal),
	})
	if listErr != nil {
		t.Fatalf("failed to list secrets: %v", listErr)
	}
	if len(secrets.Items) != 0 {
		t.Fatalf("expected ref secret to be deleted, got %d secret(s)", len(secrets.Items))
	}
}

func TestDeleteEvaluationJobResourcesIgnoresMissingRefSecret(t *testing.T) {
	evaluation := sampleEvaluation("provider-1")

	// No pre-created resources — everything is already gone (e.g. GC already ran).
	clientset := fake.NewClientset()
	runtime := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: &KubernetesHelper{clientset: clientset},
		ctx:    context.Background(),
	}

	err := runtime.DeleteEvaluationJobResources(evaluation)
	if err != nil {
		t.Fatalf("expected no error when resources already gone, got %v", err)
	}
}

// TestCreateBenchmarkResourcesDeletesRefSecretWhenConfigMapDeletedMidCreation verifies that
// when the ConfigMap disappears between Job creation and owner-ref setup (race with hard_delete),
// the ephemeral internalModelRef secret is cleaned up together with the orphaned Job.
func TestCreateBenchmarkResourcesDeletesRefSecretWhenConfigMapDeletedMidCreation(t *testing.T) {
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)
	evaluation.Model.Auth = &api.ModelAuth{SecretRef: "model-auth-secret"}

	realSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "model-auth-secret", Namespace: "default"},
		Data:       map[string][]byte{"api-key": []byte("sk-real-key")},
	}
	clientset := fake.NewClientset(realSecret)
	// Simulate ConfigMap NotFound during SetConfigMapOwner (mid-creation race).
	clientset.PrependReactor("get", "configmaps", func(action k8stesting.Action) (bool, k8sruntime.Object, error) {
		return true, nil, apierrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, action.(k8stesting.GetAction).GetName())
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
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Both the orphaned Job and the internalModelRef secret must be gone.
	jobs := listJobsByJobID(t, clientset, evaluation.Resource.ID)
	if len(jobs) != 0 {
		t.Fatalf("expected orphaned job to be deleted, got %d job(s)", len(jobs))
	}
	secrets, listErr := clientset.CoreV1().Secrets("default").List(context.Background(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", labelJobIDKey, sanitizeLabelValue(evaluation.Resource.ID)),
	})
	if listErr != nil {
		t.Fatalf("failed to list secrets: %v", listErr)
	}
	if len(secrets.Items) != 0 {
		t.Fatalf("expected internalModelRef secret to be deleted after mid-creation race, got %d secret(s)", len(secrets.Items))
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

// findConfigMapVolumeName locates the job-spec ConfigMap volume by iterating volumes by name,
// avoiding a fragile positional index that panics when volume ordering changes.
func findConfigMapVolumeName(t *testing.T, volumes []corev1.Volume) string {
	t.Helper()
	for _, v := range volumes {
		if v.VolumeSource.ConfigMap != nil {
			return v.VolumeSource.ConfigMap.Name
		}
	}
	t.Fatal("expected a ConfigMap-backed volume on the pod")
	return ""
}

// TestModelAuthCombinations is a table-driven end-to-end test covering every meaningful
// combination of model credential secret contents through createBenchmarkResources.
//
// Each case drives the full resource-creation path and asserts the resulting K8s objects:
//   - job.json adapter URL → always localhost (sidecar)
//   - sidecar_config.json model section → real URL always present; auth_secret_mount_path only when secret set
//   - model-auth volume (sidecar)  → present iff secret is configured
//   - model-auth-internal volume (adapter) → present iff secret is configured
//   - projected sources: 2 (ref+real) when api-key present; 1 (real only) when ca_cert-only
//   - ephemeral internalModelRef secret → created iff api-key (or *_api-key) present
//   - ref secret keys → api-key→"api-key:ref", *_api-key→"<k>:ref", *_url→sidecarURL+path
//   - adapter projected passthrough keys → ca_cert / hf-token projected optional when present in real secret
//
// TestModelAuthCombinations is a table-driven end-to-end test covering every meaningful
// combination of model credential secret contents through createBenchmarkResources.
//
// Replaces the three former individual model-auth tests
// (AddsModelAuthVolumeAndEnv, PassthroughOnlyModelAuth, OpenModelRoutedThroughSidecar)
// and adds new cases: hf-token only, secret not found, empty secret,
// url-key without api-key, and SA token with no recognisable keys.
//
// Assertions per case:
//   - adapter job.json URL → sidecar (localhost), never direct model URL
//   - sidecar_config.json → real URL always present; auth_secret_mount_path only when secret set
//   - model-auth volume (sidecar) and model-auth-internal volume (adapter) present/absent
//   - projected source count + ordering: internalRef is sources[0] when api-key present
//   - real-secret projection carries optional:true (absent keys silently skipped at runtime)
//   - ephemeral internalModelRef secret created iff api-key (or *_api-key/*_url) present
//   - ref secret values correct; raw ca_cert / hf-token never in ref secret
//   - adapter volume mounted at modelAuthMountPath
//   - no legacy MODEL_AUTH_* env vars on adapter container
func TestModelAuthCombinations(t *testing.T) {
	type refSecretExpect struct {
		key  string
		want string
	}

	cases := []struct {
		name            string
		secretData      map[string][]byte // nil = open model (no model.auth); use wantErr for missing-secret case
		wantErr         bool
		wantErrContains string // when wantErr=true, assert error message contains this substring

		wantInternalRefSecret     bool
		wantModelAuthVol          bool
		wantModelInternalAuthVol  bool
		wantProjectedSources      int  // 0 = not checked
		wantInternalRefFirstSrc   bool // internalRef must be sources[0]
		wantRealSecretOptional    bool // real-secret projection must have optional:true
		wantAuthMountInSidecarCfg bool
		refKeys                   []refSecretExpect
		passthroughItemKeys       []string // keys that must appear in real-secret Items
	}{
		{
			name:                      "open model — no secret",
			secretData:                nil,
			wantInternalRefSecret:     false,
			wantModelAuthVol:          false,
			wantModelInternalAuthVol:  false,
			wantAuthMountInSidecarCfg: false,
		},
		{
			// secret_ref is set but the secret does not exist → should propagate error
			name:            "secret not found",
			wantErr:         true,
			wantErrContains: "reading model auth secret",
		},
		{
			// blank Model.URL must be caught at job-creation time, not silently misdirect traffic
			name:            "empty model URL",
			wantErr:         true,
			wantErrContains: "model url and name are required",
		},
		{
			name: "api-key only",
			secretData: map[string][]byte{
				"api-key": []byte("sk-real"),
			},
			wantInternalRefSecret:     true,
			wantModelAuthVol:          true,
			wantModelInternalAuthVol:  true,
			wantProjectedSources:      2,
			wantInternalRefFirstSrc:   true,
			wantRealSecretOptional:    true,
			wantAuthMountInSidecarCfg: true,
			refKeys:                   []refSecretExpect{{"api-key", "api-key:ref"}},
		},
		{
			name: "api-key + ca_cert",
			secretData: map[string][]byte{
				"api-key": []byte("sk-real"),
				"ca_cert": []byte("-----BEGIN CERTIFICATE-----"),
			},
			wantInternalRefSecret:     true,
			wantModelAuthVol:          true,
			wantModelInternalAuthVol:  true,
			wantProjectedSources:      2,
			wantInternalRefFirstSrc:   true,
			wantRealSecretOptional:    true,
			wantAuthMountInSidecarCfg: true,
			refKeys:                   []refSecretExpect{{"api-key", "api-key:ref"}},
			passthroughItemKeys:       []string{"ca_cert"},
		},
		{
			name: "api-key + hf-token",
			secretData: map[string][]byte{
				"api-key":  []byte("sk-real"),
				"hf-token": []byte("hf-real"),
			},
			wantInternalRefSecret:     true,
			wantModelAuthVol:          true,
			wantModelInternalAuthVol:  true,
			wantProjectedSources:      2,
			wantInternalRefFirstSrc:   true,
			wantRealSecretOptional:    true,
			wantAuthMountInSidecarCfg: true,
			refKeys:                   []refSecretExpect{{"api-key", "api-key:ref"}},
			passthroughItemKeys:       []string{"hf-token"},
		},
		{
			name: "api-key + ca_cert + hf-token",
			secretData: map[string][]byte{
				"api-key":  []byte("sk-real"),
				"ca_cert":  []byte("-----BEGIN CERTIFICATE-----"),
				"hf-token": []byte("hf-real"),
			},
			wantInternalRefSecret:     true,
			wantModelAuthVol:          true,
			wantModelInternalAuthVol:  true,
			wantProjectedSources:      2,
			wantInternalRefFirstSrc:   true,
			wantRealSecretOptional:    true,
			wantAuthMountInSidecarCfg: true,
			refKeys:                   []refSecretExpect{{"api-key", "api-key:ref"}},
			passthroughItemKeys:       []string{"ca_cert", "hf-token"},
		},
		{
			// hf-token is not a credential key → no internalRef, but volumes still present
			// and hf-token is projected to the adapter; sidecar handles SA token injection.
			name: "hf-token only",
			secretData: map[string][]byte{
				"hf-token": []byte("hf-real"),
			},
			wantInternalRefSecret:     false,
			wantModelAuthVol:          true,
			wantModelInternalAuthVol:  true,
			wantProjectedSources:      1,
			wantRealSecretOptional:    true,
			wantAuthMountInSidecarCfg: true,
			passthroughItemKeys:       []string{"hf-token"},
		},
		{
			// SA token auth: only ca_cert in secret for TLS; sidecar injects SA token.
			name: "SA token / ca_cert only (passthrough)",
			secretData: map[string][]byte{
				"ca_cert": []byte("-----BEGIN CERTIFICATE-----"),
			},
			wantInternalRefSecret:     false,
			wantModelAuthVol:          true,
			wantModelInternalAuthVol:  true,
			wantProjectedSources:      1,
			wantRealSecretOptional:    true,
			wantAuthMountInSidecarCfg: true,
			passthroughItemKeys:       []string{"ca_cert"},
		},
		{
			// Secret set but contains no keys the sidecar recognises.
			name: "SA token — no recognisable keys",
			secretData: map[string][]byte{
				"some-unrelated": []byte("value"),
			},
			wantInternalRefSecret:     false,
			wantModelAuthVol:          true,
			wantModelInternalAuthVol:  true,
			wantProjectedSources:      1,
			wantRealSecretOptional:    true,
			wantAuthMountInSidecarCfg: true,
		},
		{
			// Secret exists but has no data at all.
			name:                      "empty secret",
			secretData:                map[string][]byte{},
			wantInternalRefSecret:     false,
			wantModelAuthVol:          true,
			wantModelInternalAuthVol:  true,
			wantProjectedSources:      1,
			wantRealSecretOptional:    true,
			wantAuthMountInSidecarCfg: true,
		},
		{
			// *_url IS a credential key, so internalRef is created even without an api-key.
			name: "url key without matching api-key",
			secretData: map[string][]byte{
				"model-1_url": []byte("https://api.openai.com/v1"),
			},
			wantInternalRefSecret:     true,
			wantModelAuthVol:          true,
			wantModelInternalAuthVol:  true,
			wantProjectedSources:      2,
			wantInternalRefFirstSrc:   true,
			wantRealSecretOptional:    true,
			wantAuthMountInSidecarCfg: true,
			refKeys:                   []refSecretExpect{{"model-1_url", "http://localhost:8080/v1"}},
		},
		{
			name: "multi-model api-keys + urls",
			secretData: map[string][]byte{
				"model-1_api-key": []byte("sk-m1"),
				"model-1_url":     []byte("https://api.openai.com/v1"),
				"model-2_api-key": []byte("sk-m2"),
				"model-2_url":     []byte("https://azure.example.com/v1"),
			},
			wantInternalRefSecret:     true,
			wantModelAuthVol:          true,
			wantModelInternalAuthVol:  true,
			wantProjectedSources:      2,
			wantInternalRefFirstSrc:   true,
			wantRealSecretOptional:    true,
			wantAuthMountInSidecarCfg: true,
			refKeys: []refSecretExpect{
				{"model-1_api-key", "model-1_api-key:ref"},
				{"model-1_url", "http://localhost:8080/v1"},
				{"model-2_api-key", "model-2_api-key:ref"},
				{"model-2_url", "http://localhost:8080/v1"},
			},
		},
		{
			name: "multi-model api-keys + urls + ca_cert",
			secretData: map[string][]byte{
				"model-1_api-key": []byte("sk-m1"),
				"model-1_url":     []byte("https://api.openai.com/v1"),
				"ca_cert":         []byte("-----BEGIN CERTIFICATE-----"),
			},
			wantInternalRefSecret:     true,
			wantModelAuthVol:          true,
			wantModelInternalAuthVol:  true,
			wantProjectedSources:      2,
			wantInternalRefFirstSrc:   true,
			wantRealSecretOptional:    true,
			wantAuthMountInSidecarCfg: true,
			refKeys: []refSecretExpect{
				{"model-1_api-key", "model-1_api-key:ref"},
				{"model-1_url", "http://localhost:8080/v1"},
			},
			passthroughItemKeys: []string{"ca_cert"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const secretName = "model-auth-secret"
			const modelURL = "https://model.example.com/v1"

			providerID := "provider-1"
			evaluation := sampleEvaluation(providerID)
			evaluation.Model.URL = modelURL

			var clientset *fake.Clientset
			if tc.name == "empty model URL" {
				evaluation.Model.URL = ""
				clientset = fake.NewClientset()
			} else if tc.wantErr {
				// secret_ref set but secret absent from fake store → error expected
				evaluation.Model.Auth = &api.ModelAuth{SecretRef: secretName}
				clientset = fake.NewClientset()
			} else if tc.secretData != nil {
				evaluation.Model.Auth = &api.ModelAuth{SecretRef: secretName}
				clientset = fake.NewClientset(&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: "default"},
					Data:       tc.secretData,
				})
			} else {
				clientset = fake.NewClientset()
			}

			runtime := &K8sRuntime{
				logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
				helper: &KubernetesHelper{clientset: clientset},
				serviceConfig: &config.Config{
					Service: &config.ServiceConfig{EvalInitImage: "eval-init-image"},
				},
			}

			storage := &fakeStorage{providerConfigs: sampleProviders(providerID)}
			err := runtime.createBenchmarkResources(context.Background(), runtime.logger, evaluation, &evaluation.Benchmarks[0], 0, storage)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got nil")
				}
				if tc.wantErrContains != "" && !strings.Contains(err.Error(), tc.wantErrContains) {
					t.Fatalf("expected error containing %q, got: %v", tc.wantErrContains, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("createBenchmarkResources: %v", err)
			}

			jobs := listJobsByJobID(t, clientset, evaluation.Resource.ID)
			job := jobs[0]
			adapterContainer := job.Spec.Template.Spec.Containers[0]

			// adapter URL always routes through the sidecar
			cmName := findConfigMapVolumeName(t, job.Spec.Template.Spec.Volumes)
			cm, err := clientset.CoreV1().ConfigMaps("default").Get(context.Background(), cmName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("get configmap: %v", err)
			}
			if strings.Contains(cm.Data["job.json"], "model.example.com") {
				t.Errorf("job.json must not contain direct model URL; adapter should talk to sidecar")
			}
			if !strings.Contains(cm.Data["job.json"], "localhost") {
				t.Errorf("job.json model URL must point to sidecar (localhost), got: %s", cm.Data["job.json"])
			}

			// sidecar_config.json: real URL always present; auth_secret_mount_path conditional
			sidecarCfg := cm.Data["sidecar_config.json"]
			if !strings.Contains(sidecarCfg, "model.example.com") {
				t.Errorf("sidecar_config.json must always contain the real model URL, got: %s", sidecarCfg)
			}
			if tc.wantAuthMountInSidecarCfg && !strings.Contains(sidecarCfg, "auth_secret_mount_path") {
				t.Errorf("sidecar_config.json must have auth_secret_mount_path when secret is configured")
			}
			if !tc.wantAuthMountInSidecarCfg && strings.Contains(sidecarCfg, "auth_secret_mount_path") {
				t.Errorf("sidecar_config.json must not have auth_secret_mount_path when no secret configured")
			}

			// volume presence
			var modelAuthVol, modelInternalAuthVol *corev1.Volume
			for i := range job.Spec.Template.Spec.Volumes {
				v := &job.Spec.Template.Spec.Volumes[i]
				switch v.Name {
				case modelAuthVolumeName:
					modelAuthVol = v
				case modelInternalAuthVolumeName:
					modelInternalAuthVol = v
				}
			}
			if tc.wantModelAuthVol && modelAuthVol == nil {
				t.Errorf("expected model-auth volume (sidecar real secret mount) to be present")
			}
			if !tc.wantModelAuthVol && modelAuthVol != nil {
				t.Errorf("expected no model-auth volume for this case")
			}
			if tc.wantModelInternalAuthVol && modelInternalAuthVol == nil {
				t.Errorf("expected model-auth-internal volume (adapter projected) to be present")
			}
			if !tc.wantModelInternalAuthVol && modelInternalAuthVol != nil {
				t.Errorf("expected no model-auth-internal volume for this case")
			}

			// adapter volume mount path
			if tc.wantModelInternalAuthVol {
				var foundMount bool
				for _, m := range adapterContainer.VolumeMounts {
					if m.Name == modelInternalAuthVolumeName {
						foundMount = true
						if m.MountPath != modelAuthMountPath {
							t.Errorf("adapter mount path: want %q, got %q", modelAuthMountPath, m.MountPath)
						}
					}
				}
				if !foundMount {
					t.Errorf("expected adapter volume mount %s", modelInternalAuthVolumeName)
				}
			}

			// projected source count, ordering, optional flag
			if tc.wantProjectedSources > 0 && modelInternalAuthVol != nil {
				if modelInternalAuthVol.Projected == nil {
					t.Fatalf("model-auth-internal must be a projected volume")
				}
				sources := modelInternalAuthVol.Projected.Sources
				if got := len(sources); got != tc.wantProjectedSources {
					t.Errorf("projected sources: want %d, got %d", tc.wantProjectedSources, got)
				}
				if tc.wantInternalRefFirstSrc && len(sources) >= 1 {
					if sources[0].Secret == nil {
						t.Errorf("sources[0] must be a Secret source (internalRef)")
					} else if sources[0].Secret.Name == secretName {
						t.Errorf("sources[0] must be the internalRef secret, not the real secret %q", secretName)
					}
				}
				if tc.wantRealSecretOptional && len(sources) > 0 {
					last := sources[len(sources)-1]
					if last.Secret == nil {
						t.Errorf("last projected source must be a Secret source (real secret)")
					} else if last.Secret.Optional == nil || !*last.Secret.Optional {
						t.Errorf("real-secret projection must have optional:true so absent keys are silently skipped")
					}
				}
			}

			// ephemeral internalModelRef secret
			secrets, err := clientset.CoreV1().Secrets("default").List(context.Background(), metav1.ListOptions{})
			if err != nil {
				t.Fatalf("list secrets: %v", err)
			}
			var refSecret *corev1.Secret
			for i := range secrets.Items {
				if strings.Contains(secrets.Items[i].Name, "-model-ref") {
					refSecret = &secrets.Items[i]
					break
				}
			}
			if tc.wantInternalRefSecret && refSecret == nil {
				t.Errorf("expected ephemeral internalModelRef secret to be created")
			}
			if !tc.wantInternalRefSecret && refSecret != nil {
				t.Errorf("expected no ephemeral internalModelRef secret, found %q", refSecret.Name)
			}
			// ref key values
			for _, rk := range tc.refKeys {
				if refSecret == nil {
					t.Errorf("cannot check ref key %q: ref secret not created", rk.key)
					continue
				}
				if got := string(refSecret.Data[rk.key]); got != rk.want {
					t.Errorf("ref secret key %q: want %q, got %q", rk.key, rk.want, got)
				}
			}
			// raw credentials must never appear in the ref secret
			if refSecret != nil {
				for _, forbidden := range []string{"ca_cert", "hf-token"} {
					if _, ok := refSecret.Data[forbidden]; ok {
						t.Errorf("%q must not appear in the internalModelRef secret", forbidden)
					}
				}
			}

			// passthrough keys declared in the real-secret Items projection
			if len(tc.passthroughItemKeys) > 0 && modelInternalAuthVol != nil && modelInternalAuthVol.Projected != nil {
				itemSet := map[string]bool{}
				for _, src := range modelInternalAuthVol.Projected.Sources {
					if src.Secret != nil && src.Secret.Name == secretName {
						for _, item := range src.Secret.Items {
							itemSet[item.Key] = true
						}
					}
				}
				for _, key := range tc.passthroughItemKeys {
					if !itemSet[key] {
						t.Errorf("expected key %q declared in real-secret projection Items", key)
					}
				}
			}

			// no legacy MODEL_AUTH_* env vars on adapter container
			for _, env := range adapterContainer.Env {
				if strings.HasPrefix(env.Name, "MODEL_AUTH_") {
					t.Errorf("legacy env var %q must not be set on adapter container", env.Name)
				}
			}

			// Isolation invariants: adapter must never have the SA token or raw credentials mounts.
			// evalhub-sa-token is projected only to the sidecar so the adapter cannot read it.
			// model-auth (raw real secret) is mounted only in the sidecar; the adapter gets the
			// synthetic model-auth-internal (ref tokens + optional passthrough) instead.
			for _, m := range adapterContainer.VolumeMounts {
				if m.Name == evalhubSATokenVolumeName {
					t.Errorf("adapter must never have the SA token volume mounted (name %q)", m.Name)
				}
				if m.Name == modelAuthVolumeName {
					t.Errorf("adapter must never have the raw model-auth secret mounted (name %q); it should get model-auth-internal instead", m.Name)
				}
			}

			// Verify AutomountServiceAccountToken=false on the pod spec — this is the mechanism
			// that prevents Kubernetes from auto-mounting the default SA token onto the adapter.
			if job.Spec.Template.Spec.AutomountServiceAccountToken == nil || *job.Spec.Template.Spec.AutomountServiceAccountToken {
				t.Errorf("pod AutomountServiceAccountToken must be explicitly false to prevent adapter SA token access")
			}

			// Sidecar always has the SA token mount (needed for eval-hub API callbacks on all jobs;
			// also used for model SA token injection when auth is configured).
			sidecarContainer := findContainer(job.Spec.Template.Spec.InitContainers, sidecarContainerName)
			if sidecarContainer == nil {
				t.Fatalf("expected sidecar init container to be present")
			}
			var sidecarHasSAToken bool
			for _, m := range sidecarContainer.VolumeMounts {
				if m.Name == evalhubSATokenVolumeName {
					sidecarHasSAToken = true
				}
			}
			if !sidecarHasSAToken {
				t.Errorf("sidecar must always have evalhub-sa-token mounted")
			}
			// sidecar must NOT have model-auth-internal (adapter's synthetic volume)
			if tc.wantModelAuthVol {
				for _, m := range sidecarContainer.VolumeMounts {
					if m.Name == modelInternalAuthVolumeName {
						t.Errorf("sidecar must never have the model-auth-internal (adapter) volume mounted")
					}
				}
			}
		})
	}
}
