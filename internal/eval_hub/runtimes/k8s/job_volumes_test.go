package k8s

import (
	"testing"

	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestBuildJobWithOCICredentials(t *testing.T) {
	cfg := &jobConfig{
		jobID:                "job-oci",
		benchmarkIndex:       0,
		resourceGUID:         "guid-oci",
		namespace:            "default",
		providerID:           "provider-1",
		benchmarkID:          "bench-1",
		adapterImage:         "adapter:latest",
		defaultEnv:           []api.EnvVar{},
		ociCredentialsSecret: "my-pull-secret",
	}

	job, err := buildJob(cfg)
	if err != nil {
		t.Fatalf("buildJob returned error: %v", err)
	}

	// Check volume exists with correct secret name
	foundVolume := findVolume(job.Spec.Template.Spec.Volumes, ociCredentialsVolumeName)
	if foundVolume == nil {
		t.Fatalf("expected volume %s to be present", ociCredentialsVolumeName)
	}
	if foundVolume.Secret == nil {
		t.Fatalf("expected secret volume source for %s", ociCredentialsVolumeName)
	}
	if foundVolume.Secret.SecretName != "my-pull-secret" {
		t.Fatalf("expected secret name %q, got %q", "my-pull-secret", foundVolume.Secret.SecretName)
	}

	// Check volume mount exists with correct path and subPath
	container := job.Spec.Template.Spec.Containers[0]
	foundMount := findVolumeMount(container.VolumeMounts, ociCredentialsVolumeName)
	if foundMount == nil {
		t.Fatalf("expected volume mount %s to be present", ociCredentialsVolumeName)
	}
	if foundMount.MountPath != ociCredentialsMountPath {
		t.Fatalf("expected mount path %q, got %q", ociCredentialsMountPath, foundMount.MountPath)
	}
	if foundMount.SubPath != ociCredentialsSubPath {
		t.Fatalf("expected sub path %q, got %q", ociCredentialsSubPath, foundMount.SubPath)
	}
	if !foundMount.ReadOnly {
		t.Fatalf("expected mount to be read-only")
	}

	// Check env var exists
	var foundEnv bool
	for _, e := range container.Env {
		if e.Name == envOCIAuthConfigPathName {
			foundEnv = true
			if e.Value != ociCredentialsMountPath {
				t.Fatalf("expected env value %q, got %q", ociCredentialsMountPath, e.Value)
			}
		}
	}
	if !foundEnv {
		t.Fatalf("expected env var %s to be present", envOCIAuthConfigPathName)
	}
}

func TestBuildJobTerminationFileVolume(t *testing.T) {
	cfg := &jobConfig{
		jobID:          "job-term-vol",
		resourceGUID:   "guid-tv",
		benchmarkIndex: 0,
		namespace:      "default",
		providerID:     "provider-1",
		benchmarkID:    "bench-1",
		adapterImage:   "adapter:latest",
		defaultEnv:     []api.EnvVar{},
	}
	job, err := buildJob(cfg)
	if err != nil {
		t.Fatalf("buildJob: %v", err)
	}
	foundVol := findVolume(job.Spec.Template.Spec.Volumes, terminationFileVolumeName)
	if foundVol == nil {
		t.Fatalf("expected volume %q", terminationFileVolumeName)
	}
	if foundVol.EmptyDir == nil {
		t.Fatalf("expected EmptyDir for %s", terminationFileVolumeName)
	}
	adapter := job.Spec.Template.Spec.Containers[0]
	adapterMount := findVolumeMount(adapter.VolumeMounts, terminationFileVolumeName)
	if adapterMount == nil || adapterMount.MountPath != adapterTerminationSharedMountPath {
		t.Fatalf("adapter should mount %q at %q", terminationFileVolumeName, adapterTerminationSharedMountPath)
	}
	sidecar := findContainer(job.Spec.Template.Spec.InitContainers, sidecarContainerName)
	if sidecar == nil {
		t.Fatal("expected sidecar init container")
	}
	sidecarMount := findVolumeMount(sidecar.VolumeMounts, terminationFileVolumeName)
	if sidecarMount == nil || sidecarMount.MountPath != adapterTerminationSharedMountPath {
		t.Fatalf("sidecar should mount %q at %q", terminationFileVolumeName, adapterTerminationSharedMountPath)
	}
}

func TestBuildJobSidecarDoesNotUseEvalhubConfigVolume(t *testing.T) {
	cfg := &jobConfig{
		jobID:          "job-sidecar-vol",
		resourceGUID:   "guid-sc",
		benchmarkIndex: 0,
		namespace:      "default",
		providerID:     "provider-1",
		benchmarkID:    "bench-1",
		adapterImage:   "adapter:latest",
		defaultEnv:     []api.EnvVar{},
	}
	job, err := buildJob(cfg)
	if err != nil {
		t.Fatalf("buildJob: %v", err)
	}
	for _, v := range job.Spec.Template.Spec.Volumes {
		if v.ConfigMap != nil && v.ConfigMap.Name == "evalhub-config" {
			t.Fatalf("job pod must not reference evalhub-config ConfigMap volume, got volume %q", v.Name)
		}
	}
	sidecar := findContainer(job.Spec.Template.Spec.InitContainers, sidecarContainerName)
	if sidecar == nil {
		t.Fatal("expected sidecar init container")
	}
	for _, m := range sidecar.VolumeMounts {
		if m.MountPath == "/etc/evalhub/config" {
			t.Fatalf("sidecar must not mount evalhub-config at /etc/evalhub/config")
		}
	}
	if len(sidecar.Env) > 0 {
		t.Fatalf("sidecar should have no env vars, got %d", len(sidecar.Env))
	}
}

func TestBuildJobWithoutOCICredentials(t *testing.T) {
	cfg := &jobConfig{
		jobID:          "job-no-oci",
		resourceGUID:   "guid-no-oci",
		benchmarkIndex: 0,
		namespace:      "default",
		providerID:     "provider-1",
		benchmarkID:    "bench-1",
		adapterImage:   "adapter:latest",
		defaultEnv:     []api.EnvVar{},
	}

	job, err := buildJob(cfg)
	if err != nil {
		t.Fatalf("buildJob returned error: %v", err)
	}

	for _, v := range job.Spec.Template.Spec.Volumes {
		if v.Name == ociCredentialsVolumeName {
			t.Fatalf("expected no %s volume when ociCredentialsSecret is empty", ociCredentialsVolumeName)
		}
	}
	container := job.Spec.Template.Spec.Containers[0]
	for _, e := range container.Env {
		if e.Name == envOCIAuthConfigPathName {
			t.Fatalf("expected no %s env var when ociCredentialsSecret is empty", envOCIAuthConfigPathName)
		}
	}
}

func TestBuildJobWithS3TestData(t *testing.T) {
	cfg := &jobConfig{
		jobID:             "job-s3",
		resourceGUID:      "guid-s3",
		benchmarkIndex:    0,
		namespace:         "default",
		providerID:        "provider-1",
		benchmarkID:       "bench-1",
		adapterImage:      "adapter:latest",
		defaultEnv:        []api.EnvVar{},
		testDataInitImage: "quay.io/evalhub/evalhub:test",
		testDataS3: s3TestDataConfig{
			bucket:    "bucket-1",
			key:       "/a/b",
			secretRef: "s3-secret",
		},
	}

	job, err := buildJob(cfg)
	if err != nil {
		t.Fatalf("buildJob returned error: %v", err)
	}

	initContainer := findContainer(job.Spec.Template.Spec.InitContainers, initContainerName)
	if initContainer == nil {
		t.Fatal("expected test-data init container")
	}
	if initContainer.Image != "quay.io/evalhub/evalhub:test" {
		t.Fatalf("expected init container image %q, got %q", "quay.io/evalhub/evalhub:test", initContainer.Image)
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
	for _, v := range job.Spec.Template.Spec.Volumes {
		if v.Name == testDataVolumeName {
			foundTestDataVolume = true
		}
		if v.Name == testDataSecretVolumeName {
			foundSecretVolume = true
			if v.Secret == nil || v.Secret.SecretName != "s3-secret" {
				t.Fatalf("expected secret volume %q with secret %q", testDataSecretVolumeName, "s3-secret")
			}
		}
	}
	if !foundTestDataVolume || !foundSecretVolume {
		t.Fatalf("expected test data and secret volumes to be present")
	}

	var foundTestDataMount bool
	for _, m := range job.Spec.Template.Spec.Containers[0].VolumeMounts {
		if m.Name == testDataVolumeName && m.MountPath == testDataMountPath {
			foundTestDataMount = true
		}
	}
	if !foundTestDataMount {
		t.Fatalf("expected adapter to mount %s", testDataMountPath)
	}
}

func TestBuildJobWithS3TestDataSkipsEmptyNormalizedKey(t *testing.T) {
	cfg := &jobConfig{
		jobID:          "job-s3-empty",
		resourceGUID:   "guid-s3-empty",
		benchmarkIndex: 0,
		namespace:      "default",
		providerID:     "provider-1",
		benchmarkID:    "bench-1",
		adapterImage:   "adapter:latest",
		defaultEnv:     []api.EnvVar{},
		testDataS3: s3TestDataConfig{
			bucket:    "bucket-1",
			key:       "/",
			secretRef: "s3-secret",
		},
	}

	job, err := buildJob(cfg)
	if err != nil {
		t.Fatalf("buildJob returned error: %v", err)
	}

	// Only the sidecar init container should be present (no test-data init container)
	if findContainer(job.Spec.Template.Spec.InitContainers, initContainerName) != nil {
		t.Fatalf("expected no test-data init container when normalized key is empty")
	}
	for _, v := range job.Spec.Template.Spec.Volumes {
		if v.Name == testDataVolumeName || v.Name == testDataSecretVolumeName {
			t.Fatalf("expected no test data volumes when normalized key is empty")
		}
	}
}

// TestBuildJobWithModelAuthSecret verifies that when only modelAuthSecretRef is set (sidecar-proxy
// path, SA token auth), the adapter receives a projected volume with passthrough keys only
// (hf-token, ca_cert, both optional). There is no direct-mount path — the sidecar is always
// active when model auth is configured.
func TestBuildJobWithModelAuthSecret(t *testing.T) {
	cfg := &jobConfig{
		jobID:              "job-auth",
		benchmarkIndex:     0,
		resourceGUID:       "guid-auth",
		namespace:          "default",
		providerID:         "provider-1",
		benchmarkID:        "bench-1",
		adapterImage:       "adapter:latest",
		defaultEnv:         []api.EnvVar{},
		modelAuthSecretRef: "model-auth-secret",
	}

	job, err := buildJob(cfg)
	if err != nil {
		t.Fatalf("buildJob returned error: %v", err)
	}

	// Adapter must have the projected passthrough volume, not the raw secret volume.
	foundVolume := findVolume(job.Spec.Template.Spec.Volumes, modelInternalAuthVolumeName)
	if foundVolume == nil {
		t.Fatalf("expected projected volume %s on adapter", modelInternalAuthVolumeName)
	}
	if foundVolume.Projected == nil {
		t.Fatalf("expected projected volume source for %s", modelInternalAuthVolumeName)
	}
	if len(foundVolume.Projected.Sources) != 1 {
		t.Fatalf("expected exactly 1 projected source (passthrough only), got %d", len(foundVolume.Projected.Sources))
	}
	src := foundVolume.Projected.Sources[0]
	if src.Secret == nil || src.Secret.Name != "model-auth-secret" {
		t.Fatalf("expected projected source from real secret %q, got %+v", "model-auth-secret", src)
	}
	if src.Secret.Optional == nil || !*src.Secret.Optional {
		t.Fatal("expected passthrough projection to be optional:true")
	}

	container := job.Spec.Template.Spec.Containers[0]
	foundMount := findVolumeMount(container.VolumeMounts, modelInternalAuthVolumeName)
	if foundMount == nil {
		t.Fatalf("expected volume mount %s to be present on adapter", modelInternalAuthVolumeName)
	}
	if foundMount.MountPath != modelAuthMountPath {
		t.Fatalf("expected mount path %q, got %q", modelAuthMountPath, foundMount.MountPath)
	}
	if !foundMount.ReadOnly {
		t.Fatal("expected mount to be read-only")
	}

	// Raw secret volume must not be mounted on the adapter container (it belongs to the sidecar).
	if findVolumeMount(container.VolumeMounts, modelAuthVolumeName) != nil {
		t.Fatalf("unexpected raw secret mount %s on adapter container; direct-mount path is gone", modelAuthVolumeName)
	}
}

func TestBuildJobWithoutModelAuthSecret(t *testing.T) {
	cfg := &jobConfig{
		jobID:          "job-no-auth",
		resourceGUID:   "guid-no-auth",
		benchmarkIndex: 0,
		namespace:      "default",
		providerID:     "provider-1",
		benchmarkID:    "bench-1",
		adapterImage:   "adapter:latest",
		defaultEnv:     []api.EnvVar{},
	}

	job, err := buildJob(cfg)
	if err != nil {
		t.Fatalf("buildJob returned error: %v", err)
	}

	for _, v := range job.Spec.Template.Spec.Volumes {
		if v.Name == modelAuthVolumeName || v.Name == modelInternalAuthVolumeName {
			t.Fatalf("expected no %s volume when modelAuthSecretRef is empty", v.Name)
		}
	}
	container := job.Spec.Template.Spec.Containers[0]
	for _, e := range container.Env {
		if e.Name == "MODEL_AUTH_API_KEY_PATH" || e.Name == "MODEL_AUTH_CA_CERT_PATH" {
			t.Fatalf("expected no model auth env vars, found %s", e.Name)
		}
	}
}

// TestBuildJobSATokenSidecarOnly verifies that:
//   - pod-level AutomountServiceAccountToken is explicitly disabled
//   - the evalhub-sa-token projected volume exists on the pod and is mounted in the sidecar
//   - the adapter container has no evalhub-sa-token mount
//   - the adapter has the pod-namespace DownwardAPI volume mounted at k8sSAMountPath
func TestBuildJobSATokenSidecarOnly(t *testing.T) {
	cfg := &jobConfig{
		jobID:          "sa-token-job",
		resourceGUID:   "guid-sa",
		benchmarkIndex: 0,
		namespace:      "default",
		providerID:     "p",
		benchmarkID:    "b",
		adapterImage:   "adapter:latest",
		defaultEnv:     []api.EnvVar{},
	}
	job, err := buildJob(cfg)
	if err != nil {
		t.Fatalf("buildJob: %v", err)
	}

	// Pod must disable auto-mount so SA token is not injected into adapter.
	if job.Spec.Template.Spec.AutomountServiceAccountToken == nil || *job.Spec.Template.Spec.AutomountServiceAccountToken {
		t.Fatal("expected AutomountServiceAccountToken=false on PodSpec")
	}

	// Pod volumes must contain the evalhub-sa-token projected volume.
	foundPodVolume := findVolume(job.Spec.Template.Spec.Volumes, evalhubSATokenVolumeName)
	if foundPodVolume == nil {
		t.Fatalf("expected pod volume %q", evalhubSATokenVolumeName)
	}
	if foundPodVolume.Projected == nil {
		t.Fatal("evalhub-sa-token volume must be a projected volume")
	}
	hasSAToken := false
	for _, src := range foundPodVolume.Projected.Sources {
		if src.ServiceAccountToken != nil {
			hasSAToken = true
		}
	}
	if !hasSAToken {
		t.Fatal("evalhub-sa-token projected volume must contain a ServiceAccountToken source")
	}

	// Sidecar must mount evalhub-sa-token at the standard SA token path.
	sidecar := findContainer(job.Spec.Template.Spec.InitContainers, sidecarContainerName)
	if sidecar == nil {
		t.Fatal("sidecar init container not found")
	}
	foundSidecarMount := findVolumeMount(sidecar.VolumeMounts, evalhubSATokenVolumeName)
	if foundSidecarMount == nil {
		t.Fatalf("sidecar must mount %q", evalhubSATokenVolumeName)
	}
	if foundSidecarMount.MountPath != k8sSAMountPath {
		t.Errorf("sidecar SA token mount path: got %q, want %q", foundSidecarMount.MountPath, k8sSAMountPath)
	}
	if !foundSidecarMount.ReadOnly {
		t.Error("sidecar SA token mount must be read-only")
	}

	// Adapter must NOT mount evalhub-sa-token.
	adapter := findContainer(job.Spec.Template.Spec.Containers, adapterContainerName)
	if adapter == nil {
		t.Fatal("adapter container not found")
	}
	if findVolumeMount(adapter.VolumeMounts, evalhubSATokenVolumeName) != nil {
		t.Fatalf("adapter must not have %q volume mount", evalhubSATokenVolumeName)
	}

	// Adapter must have the pod-namespace DownwardAPI volume mounted at k8sSAMountPath
	// so the SDK can read the namespace file to set X-Tenant on sidecar requests.
	foundNamespaceVolume := findVolume(job.Spec.Template.Spec.Volumes, adapterNamespaceVolumeName)
	if foundNamespaceVolume == nil {
		t.Fatalf("expected pod-namespace DownwardAPI volume %q on pod", adapterNamespaceVolumeName)
	}
	if foundNamespaceVolume.Projected == nil {
		t.Fatal("pod-namespace volume must be a projected volume")
	}
	hasDownwardAPI := false
	for _, src := range foundNamespaceVolume.Projected.Sources {
		if src.DownwardAPI != nil {
			hasDownwardAPI = true
		}
	}
	if !hasDownwardAPI {
		t.Fatal("pod-namespace projected volume must contain a DownwardAPI source")
	}
	foundNamespaceMount := findVolumeMount(adapter.VolumeMounts, adapterNamespaceVolumeName)
	if foundNamespaceMount == nil {
		t.Fatalf("adapter must mount %q at %q", adapterNamespaceVolumeName, k8sSAMountPath)
	}
	if foundNamespaceMount.MountPath != k8sSAMountPath {
		t.Errorf("adapter namespace mount path: got %q, want %q", foundNamespaceMount.MountPath, k8sSAMountPath)
	}
	if !foundNamespaceMount.ReadOnly {
		t.Error("adapter namespace mount must be read-only")
	}
}
