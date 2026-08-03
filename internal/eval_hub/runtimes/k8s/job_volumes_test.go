package k8s

import (
	"testing"

	"github.com/eval-hub/eval-hub/pkg/api"
	corev1 "k8s.io/api/core/v1"
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
	var foundVolume bool
	for _, v := range job.Spec.Template.Spec.Volumes {
		if v.Name == ociCredentialsVolumeName {
			foundVolume = true
			if v.Secret == nil {
				t.Fatalf("expected secret volume source for %s", ociCredentialsVolumeName)
			}
			if v.Secret.SecretName != "my-pull-secret" {
				t.Fatalf("expected secret name %q, got %q", "my-pull-secret", v.Secret.SecretName)
			}
		}
	}
	if !foundVolume {
		t.Fatalf("expected volume %s to be present", ociCredentialsVolumeName)
	}

	// Check volume mount exists with correct path and subPath
	container := job.Spec.Template.Spec.Containers[0]
	var foundMount bool
	for _, m := range container.VolumeMounts {
		if m.Name == ociCredentialsVolumeName {
			foundMount = true
			if m.MountPath != ociCredentialsMountPath {
				t.Fatalf("expected mount path %q, got %q", ociCredentialsMountPath, m.MountPath)
			}
			if m.SubPath != ociCredentialsSubPath {
				t.Fatalf("expected sub path %q, got %q", ociCredentialsSubPath, m.SubPath)
			}
			if !m.ReadOnly {
				t.Fatalf("expected mount to be read-only")
			}
		}
	}
	if !foundMount {
		t.Fatalf("expected volume mount %s to be present", ociCredentialsVolumeName)
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
	var foundVol bool
	for _, v := range job.Spec.Template.Spec.Volumes {
		if v.Name == terminationFileVolumeName {
			foundVol = true
			if v.EmptyDir == nil {
				t.Fatalf("expected EmptyDir for %s", terminationFileVolumeName)
			}
		}
	}
	if !foundVol {
		t.Fatalf("expected volume %q", terminationFileVolumeName)
	}
	adapter := job.Spec.Template.Spec.Containers[0]
	var adapterMount bool
	for _, m := range adapter.VolumeMounts {
		if m.Name == terminationFileVolumeName && m.MountPath == adapterTerminationSharedMountPath {
			adapterMount = true
			break
		}
	}
	if !adapterMount {
		t.Fatalf("adapter should mount %q at %q", terminationFileVolumeName, adapterTerminationSharedMountPath)
	}
	sidecar := findContainer(job.Spec.Template.Spec.InitContainers, sidecarContainerName)
	if sidecar == nil {
		t.Fatal("expected sidecar init container")
	}
	var sidecarMount bool
	for _, m := range sidecar.VolumeMounts {
		if m.Name == terminationFileVolumeName && m.MountPath == adapterTerminationSharedMountPath {
			sidecarMount = true
			break
		}
	}
	if !sidecarMount {
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
	var foundVolume *corev1.Volume
	for i := range job.Spec.Template.Spec.Volumes {
		if job.Spec.Template.Spec.Volumes[i].Name == modelInternalAuthVolumeName {
			foundVolume = &job.Spec.Template.Spec.Volumes[i]
			break
		}
	}
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
	var foundMount bool
	for _, m := range container.VolumeMounts {
		if m.Name == modelInternalAuthVolumeName {
			foundMount = true
			if m.MountPath != modelAuthMountPath {
				t.Fatalf("expected mount path %q, got %q", modelAuthMountPath, m.MountPath)
			}
			if !m.ReadOnly {
				t.Fatal("expected mount to be read-only")
			}
		}
	}
	if !foundMount {
		t.Fatalf("expected volume mount %s to be present on adapter", modelInternalAuthVolumeName)
	}

	// Raw secret volume must not be mounted on the adapter container (it belongs to the sidecar).
	for _, m := range container.VolumeMounts {
		if m.Name == modelAuthVolumeName {
			t.Fatalf("unexpected raw secret mount %s on adapter container; direct-mount path is gone", modelAuthVolumeName)
		}
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
		if v.Name == modelAuthVolumeName {
			t.Fatalf("expected no %s volume when modelAuthSecretRef is empty", modelAuthVolumeName)
		}
	}
	container := job.Spec.Template.Spec.Containers[0]
	for _, e := range container.Env {
		if e.Name == "MODEL_AUTH_API_KEY_PATH" || e.Name == "MODEL_AUTH_CA_CERT_PATH" {
			t.Fatalf("expected no model auth env vars, found %s", e.Name)
		}
	}
}

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
	var foundPodVolume bool
	for _, v := range job.Spec.Template.Spec.Volumes {
		if v.Name == evalhubSATokenVolumeName {
			foundPodVolume = true
			if v.Projected == nil {
				t.Fatal("evalhub-sa-token volume must be a projected volume")
			}
			hasSAToken := false
			for _, src := range v.Projected.Sources {
				if src.ServiceAccountToken != nil {
					hasSAToken = true
				}
			}
			if !hasSAToken {
				t.Fatal("evalhub-sa-token projected volume must contain a ServiceAccountToken source")
			}
		}
	}
	if !foundPodVolume {
		t.Fatalf("expected pod volume %q", evalhubSATokenVolumeName)
	}

	// Sidecar must mount evalhub-sa-token at the standard SA token path.
	sidecar := findContainer(job.Spec.Template.Spec.InitContainers, sidecarContainerName)
	if sidecar == nil {
		t.Fatal("sidecar init container not found")
	}
	var foundSidecarMount bool
	for _, m := range sidecar.VolumeMounts {
		if m.Name == evalhubSATokenVolumeName {
			foundSidecarMount = true
			if m.MountPath != k8sSAMountPath {
				t.Errorf("sidecar SA token mount path: got %q, want %q", m.MountPath, k8sSAMountPath)
			}
			if !m.ReadOnly {
				t.Error("sidecar SA token mount must be read-only")
			}
		}
	}
	if !foundSidecarMount {
		t.Fatalf("sidecar must mount %q", evalhubSATokenVolumeName)
	}

	// Adapter must NOT mount evalhub-sa-token.
	adapter := findContainer(job.Spec.Template.Spec.Containers, adapterContainerName)
	if adapter == nil {
		t.Fatal("adapter container not found")
	}
	for _, m := range adapter.VolumeMounts {
		if m.Name == evalhubSATokenVolumeName {
			t.Fatalf("adapter must not have %q volume mount", evalhubSATokenVolumeName)
		}
	}

	// Adapter must have the pod-namespace DownwardAPI volume mounted at k8sSAMountPath
	// so the SDK can read the namespace file to set X-Tenant on sidecar requests.
	var foundNamespaceVolume bool
	for _, v := range job.Spec.Template.Spec.Volumes {
		if v.Name == adapterNamespaceVolumeName {
			foundNamespaceVolume = true
			if v.Projected == nil {
				t.Fatal("pod-namespace volume must be a projected volume")
			}
			hasDownwardAPI := false
			for _, src := range v.Projected.Sources {
				if src.DownwardAPI != nil {
					hasDownwardAPI = true
				}
			}
			if !hasDownwardAPI {
				t.Fatal("pod-namespace projected volume must contain a DownwardAPI source")
			}
		}
	}
	if !foundNamespaceVolume {
		t.Fatalf("expected pod-namespace DownwardAPI volume %q on pod", adapterNamespaceVolumeName)
	}
	var foundNamespaceMount bool
	for _, m := range adapter.VolumeMounts {
		if m.Name == adapterNamespaceVolumeName {
			foundNamespaceMount = true
			if m.MountPath != k8sSAMountPath {
				t.Errorf("adapter namespace mount path: got %q, want %q", m.MountPath, k8sSAMountPath)
			}
			if !m.ReadOnly {
				t.Error("adapter namespace mount must be read-only")
			}
		}
	}
	if !foundNamespaceMount {
		t.Fatalf("adapter must mount %q at %q", adapterNamespaceVolumeName, k8sSAMountPath)
	}
}
