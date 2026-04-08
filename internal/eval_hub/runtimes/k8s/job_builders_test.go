package k8s

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_hub/runtimes/shared"
	"github.com/eval-hub/eval-hub/pkg/api"
	corev1 "k8s.io/api/core/v1"
)

func findContainer(containers []corev1.Container, name string) *corev1.Container {
	for i := range containers {
		if containers[i].Name == name {
			return &containers[i]
		}
	}
	return nil
}

func TestBuildConfigMap(t *testing.T) {

	cfg := &jobConfig{
		jobID:          "job-123",
		benchmarkIndex: 0,
		namespace:      "default",
		providerID:     "provider-1",
		benchmarkID:    "bench-1",
		jobSpec:        shared.JobSpec{},
		resourceGUID:   "guid-123",
	}

	configMap, err := buildConfigMap(cfg)
	if err != nil {
		t.Fatalf("buildConfigMap returned error: %v", err)
	}
	expectedName := configMapName(cfg.jobID, cfg.resourceGUID)
	if configMap.Name != expectedName {
		t.Fatalf("expected configmap name %s, got %s", expectedName, configMap.Name)
	}

	annotations := configMap.Annotations
	if annotations[annotationJobIDKey] != cfg.jobID {
		t.Fatalf("expected job_id annotation %q, got %q", cfg.jobID, annotations[annotationJobIDKey])
	}
	if annotations[annotationProviderIDKey] != cfg.providerID {
		t.Fatalf("expected provider_id annotation %q, got %q", cfg.providerID, annotations[annotationProviderIDKey])
	}
	if annotations[annotationBenchmarkIDKey] != cfg.benchmarkID {
		t.Fatalf("expected benchmark_id annotation %q, got %q", cfg.benchmarkID, annotations[annotationBenchmarkIDKey])
	}
	if _, ok := configMap.Data[sidecarConfigFileName]; !ok {
		t.Fatalf("expected ConfigMap data key %q", sidecarConfigFileName)
	}
	if configMap.Data[sidecarConfigFileName] == "" {
		t.Fatalf("sidecar_config.json should be non-empty")
	}
	var sidecar map[string]any
	if err := json.Unmarshal([]byte(configMap.Data[sidecarConfigFileName]), &sidecar); err != nil {
		t.Fatalf("sidecar_config.json invalid JSON: %v", err)
	}
}

func TestBuildConfigMapSidecarConfigJSONContent(t *testing.T) {
	cfg := &jobConfig{
		jobID:          "job-123",
		benchmarkIndex: 0,
		namespace:      "default",
		providerID:     "provider-1",
		benchmarkID:    "bench-1",
		jobSpec:        shared.JobSpec{},
		resourceGUID:   "guid-123",
		sidecarConfig: &config.SidecarConfig{
			Port:    8081,
			BaseURL: "http://localhost:8081",
		},
	}
	cm, err := buildConfigMap(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(cm.Data[sidecarConfigFileName]), &m); err != nil {
		t.Fatal(err)
	}
	if m["port"].(float64) != 8081 {
		t.Fatalf("port: %v", m["port"])
	}
}

func TestBuildK8sNameSanitizes(t *testing.T) {
	name := buildK8sName("Job-123", "Guid-ABC", "")
	if !strings.HasPrefix(name, "job-123-") {
		t.Fatalf("expected sanitized name to start with %q, got %q", "job-123-", name)
	}
}

func TestBuildK8sNameDiffersAcrossGUIDs(t *testing.T) {
	jobID := "job-123"
	name1 := buildK8sName(jobID, "guid-1", "")
	name2 := buildK8sName(jobID, "guid-2", "")
	if name1 == name2 {
		t.Fatalf("expected different names for different GUIDs, got %q", name1)
	}
}

func TestJobLabelsNilConfig(t *testing.T) {
	labels := jobLabels(nil)
	if len(labels) != 0 {
		t.Fatalf("expected empty labels for nil cfg, got %v", labels)
	}
}

func TestJobLabelsSanitizeBenchmarkID(t *testing.T) {
	labels := jobLabels(&jobConfig{jobID: "job-123", providerID: "lighteval", benchmarkID: "arc:easy", benchmarkIndex: 0})
	if labels[labelBenchmarkIDKey] != "arc-easy" {
		t.Fatalf("expected benchmark label to be sanitized, got %q", labels[labelBenchmarkIDKey])
	}
	if labels[labelBenchmarkIndexKey] != "0" {
		t.Fatalf("expected benchmark_index label %q, got %q", "0", labels[labelBenchmarkIndexKey])
	}
}

func TestJobLabelsEvalHubInstance(t *testing.T) {
	labels := jobLabels(&jobConfig{jobID: "j", providerID: "p", benchmarkID: "b", benchmarkIndex: 0, evalHubInstanceName: "my-evalhub", evalHubCRNamespace: "prod-ns"})
	if labels[labelEvalHubInstanceNameKey] != "my-evalhub" {
		t.Fatalf("instance-name: got %q", labels[labelEvalHubInstanceNameKey])
	}
	if labels[labelEvalHubInstanceNamespaceKey] != "prod-ns" {
		t.Fatalf("instance-namespace: got %q", labels[labelEvalHubInstanceNamespaceKey])
	}
	empty := jobLabels(&jobConfig{jobID: "j", providerID: "p", benchmarkID: "b", benchmarkIndex: 0})
	if _, ok := empty[labelEvalHubInstanceNameKey]; ok {
		t.Fatal("expected no instance labels when name/namespace empty")
	}
}

func TestJobLabelsKueueQueueName(t *testing.T) {
	labels := jobLabels(&jobConfig{jobID: "j", providerID: "p", benchmarkID: "b", benchmarkIndex: 0, queueKind: "kueue", queueName: "my-queue"})
	if labels[labelKueueQueueNameKey] != "my-queue" {
		t.Fatalf("expected kueue queue label %q, got %q", "my-queue", labels[labelKueueQueueNameKey])
	}
	noQueue := jobLabels(&jobConfig{jobID: "j", providerID: "p", benchmarkID: "b", benchmarkIndex: 0})
	if _, ok := noQueue[labelKueueQueueNameKey]; ok {
		t.Fatal("expected no kueue queue label when queue name is empty")
	}
	nonKueue := jobLabels(&jobConfig{jobID: "j", providerID: "p", benchmarkID: "b", benchmarkIndex: 0, queueKind: "other", queueName: "my-queue"})
	if _, ok := nonKueue[labelKueueQueueNameKey]; ok {
		t.Fatal("expected no kueue queue label when queue kind is not kueue")
	}
}

func TestBuildJobRequiresAdapterImage(t *testing.T) {
	cfg := &jobConfig{
		jobID:          "job-123",
		resourceGUID:   "guid-123",
		benchmarkIndex: 0,
		namespace:      "default",
		providerID:     "provider-1",
		benchmarkID:    "bench-1",
	}

	_, err := buildJob(cfg)
	if err == nil {
		t.Fatalf("expected error for missing adapter image")
	}
}

func TestBuildJobAdapterEvalHubModeEnv(t *testing.T) {
	t.Run("k8s when not local mode", func(t *testing.T) {
		cfg := &jobConfig{
			jobID:          "job-mode",
			resourceGUID:   "guid-mode",
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
		adapter := job.Spec.Template.Spec.Containers[0]
		var got string
		var found bool
		for _, e := range adapter.Env {
			if e.Name == envEvalHubModeName {
				found = true
				got = e.Value
				break
			}
		}
		if !found {
			t.Fatalf("adapter missing env %q", envEvalHubModeName)
		}
		if got != "k8s" {
			t.Fatalf("EVALHUB_MODE = %q, want k8s", got)
		}
		if job.Spec.Template.Spec.ServiceAccountName != "" {
			t.Fatalf("expected empty ServiceAccountName when cfg.serviceAccountName is unset, got %q", job.Spec.Template.Spec.ServiceAccountName)
		}
	})
	t.Run("k8s sets ServiceAccountName when configured", func(t *testing.T) {
		cfg := &jobConfig{
			jobID:              "job-sa",
			resourceGUID:       "guid-sa",
			benchmarkIndex:     0,
			namespace:          "tenant-ns",
			providerID:         "provider-1",
			benchmarkID:        "bench-1",
			adapterImage:       "adapter:latest",
			defaultEnv:         []api.EnvVar{},
			localMode:          false,
			serviceAccountName: "evalhub-inst-instns-job",
		}
		job, err := buildJob(cfg)
		if err != nil {
			t.Fatalf("buildJob: %v", err)
		}
		if job.Spec.Template.Spec.ServiceAccountName != "evalhub-inst-instns-job" {
			t.Fatalf("ServiceAccountName = %q, want evalhub-inst-instns-job", job.Spec.Template.Spec.ServiceAccountName)
		}
	})
	t.Run("local when local mode", func(t *testing.T) {
		cfg := &jobConfig{
			jobID:          "job-mode-local",
			resourceGUID:   "guid-mode-l",
			benchmarkIndex: 0,
			namespace:      "default",
			providerID:     "provider-1",
			benchmarkID:    "bench-1",
			adapterImage:   "adapter:latest",
			defaultEnv:     []api.EnvVar{},
			localMode:      true,
		}
		job, err := buildJob(cfg)
		if err != nil {
			t.Fatalf("buildJob: %v", err)
		}
		if len(job.Spec.Template.Spec.Containers) != 1 {
			t.Fatalf("expected single adapter container in local mode, got %d", len(job.Spec.Template.Spec.Containers))
		}
		adapter := job.Spec.Template.Spec.Containers[0]
		var got string
		var found bool
		for _, e := range adapter.Env {
			if e.Name == envEvalHubModeName {
				found = true
				got = e.Value
				break
			}
		}
		if !found {
			t.Fatalf("adapter missing env %q", envEvalHubModeName)
		}
		if got != "local" {
			t.Fatalf("EVALHUB_MODE = %q, want local", got)
		}
	})
}

func TestBuildJobSecurityContext(t *testing.T) {
	cfg := &jobConfig{
		jobID:          "job-123",
		resourceGUID:   "guid-123",
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
	if len(job.Spec.Template.Spec.Containers) == 0 {
		t.Fatalf("expected at least one container in pod spec")
	}
	container := job.Spec.Template.Spec.Containers[0]
	if container.SecurityContext == nil || container.SecurityContext.AllowPrivilegeEscalation == nil {
		t.Fatalf("expected security context with allowPrivilegeEscalation")
	}
	if *container.SecurityContext.AllowPrivilegeEscalation {
		t.Fatalf("expected allowPrivilegeEscalation to be false")
	}
	if container.SecurityContext.RunAsNonRoot == nil || !*container.SecurityContext.RunAsNonRoot {
		t.Fatalf("expected runAsNonRoot to be true")
	}
	// RunAsUser and RunAsGroup are intentionally not set to allow OpenShift SCC to assign them
	// from the allowed range based on the namespace's security constraints
	if container.SecurityContext.RunAsUser != nil {
		t.Fatalf("expected runAsUser to be nil (let OpenShift SCC assign it)")
	}
	if container.SecurityContext.RunAsGroup != nil {
		t.Fatalf("expected runAsGroup to be nil (let OpenShift SCC assign it)")
	}
	if container.SecurityContext.Capabilities == nil || len(container.SecurityContext.Capabilities.Drop) == 0 {
		t.Fatalf("expected dropped capabilities")
	}
	if container.SecurityContext.Capabilities.Drop[0] != "ALL" {
		t.Fatalf("expected ALL capability drop")
	}
	if container.SecurityContext.SeccompProfile == nil || container.SecurityContext.SeccompProfile.Type == "" {
		t.Fatalf("expected seccomp profile to be set")
	}
}

func TestBuildJobAnnotations(t *testing.T) {
	cfg := &jobConfig{
		jobID:          "job-123",
		resourceGUID:   "guid-123",
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

	if job.Annotations[annotationJobIDKey] != cfg.jobID {
		t.Fatalf("expected job_id annotation %q, got %q", cfg.jobID, job.Annotations[annotationJobIDKey])
	}
	if job.Annotations[annotationProviderIDKey] != cfg.providerID {
		t.Fatalf("expected provider_id annotation %q, got %q", cfg.providerID, job.Annotations[annotationProviderIDKey])
	}
	if job.Annotations[annotationBenchmarkIDKey] != cfg.benchmarkID {
		t.Fatalf("expected benchmark_id annotation %q, got %q", cfg.benchmarkID, job.Annotations[annotationBenchmarkIDKey])
	}

	podAnnotations := job.Spec.Template.Annotations
	if podAnnotations[annotationJobIDKey] != cfg.jobID {
		t.Fatalf("expected pod job_id annotation %q, got %q", cfg.jobID, podAnnotations[annotationJobIDKey])
	}
	if podAnnotations[annotationProviderIDKey] != cfg.providerID {
		t.Fatalf("expected pod provider_id annotation %q, got %q", cfg.providerID, podAnnotations[annotationProviderIDKey])
	}
	if podAnnotations[annotationBenchmarkIDKey] != cfg.benchmarkID {
		t.Fatalf("expected pod benchmark_id annotation %q, got %q", cfg.benchmarkID, podAnnotations[annotationBenchmarkIDKey])
	}
}

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
			if v.VolumeSource.Secret == nil {
				t.Fatalf("expected secret volume source for %s", ociCredentialsVolumeName)
			}
			if v.VolumeSource.Secret.SecretName != "my-pull-secret" {
				t.Fatalf("expected secret name %q, got %q", "my-pull-secret", v.VolumeSource.Secret.SecretName)
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
	for _, m := range adapter.VolumeMounts {
		if m.MountPath == sidecarConfigMountPath {
			t.Fatalf("adapter must not mount sidecar config at %q (model proxy base comes from job.json callback_url)", sidecarConfigMountPath)
		}
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
			if v.VolumeSource.Secret == nil || v.VolumeSource.Secret.SecretName != "s3-secret" {
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

	var foundVolume bool
	for _, v := range job.Spec.Template.Spec.Volumes {
		if v.Name == modelAuthVolumeName {
			foundVolume = true
			if v.VolumeSource.Secret == nil {
				t.Fatalf("expected secret volume source for %s", modelAuthVolumeName)
			}
			if v.VolumeSource.Secret.SecretName != "model-auth-secret" {
				t.Fatalf("expected secret name %q, got %q", "model-auth-secret", v.VolumeSource.Secret.SecretName)
			}
		}
	}
	if !foundVolume {
		t.Fatalf("expected volume %s to be present", modelAuthVolumeName)
	}

	container := job.Spec.Template.Spec.Containers[0]
	for _, m := range container.VolumeMounts {
		if m.Name == modelAuthVolumeName {
			t.Fatalf("adapter must not mount %s (sidecar-only)", modelAuthVolumeName)
		}
	}

	if job.Spec.Template.Spec.AutomountServiceAccountToken == nil || *job.Spec.Template.Spec.AutomountServiceAccountToken {
		t.Fatalf("expected AutomountServiceAccountToken false on adapter+sidecar jobs")
	}

	sidecar := findContainer(job.Spec.Template.Spec.InitContainers, sidecarContainerName)
	if sidecar == nil {
		t.Fatalf("expected sidecar init container %q", sidecarContainerName)
	}
	var sidecarModelMount bool
	for _, m := range sidecar.VolumeMounts {
		if m.Name == modelAuthVolumeName {
			sidecarModelMount = true
			if m.MountPath != modelAuthMountPath || !m.ReadOnly {
				t.Fatalf("sidecar: expected read-only model auth mount at %q", modelAuthMountPath)
			}
		}
	}
	if !sidecarModelMount {
		t.Fatalf("expected sidecar to mount volume %s for model proxy auth", modelAuthVolumeName)
	}

	var sidecarSATokenMount bool
	for _, m := range sidecar.VolumeMounts {
		if m.Name == sidecarSATokenVolumeName {
			sidecarSATokenMount = true
			if m.MountPath != sidecarSATokenMountPath || !m.ReadOnly {
				t.Fatalf("sidecar: expected read-only SA token mount at %q", sidecarSATokenMountPath)
			}
		}
	}
	if !sidecarSATokenMount {
		t.Fatalf("expected sidecar to mount %s for eval-hub / model SA token fallback", sidecarSATokenVolumeName)
	}

	for _, e := range container.Env {
		if e.Name == "MODEL_AUTH_API_KEY_PATH" || e.Name == "MODEL_AUTH_CA_CERT_PATH" {
			t.Fatalf("expected no model auth env vars, found %s", e.Name)
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

func TestContainerCommandList(t *testing.T) {
	command := buildContainerCommand([]string{"/bin/sh", "-c", "echo hello"})
	if len(command) != 3 {
		t.Fatalf("expected 3 command parts, got %d", len(command))
	}
	if command[0] != "/bin/sh" || command[1] != "-c" || command[2] != "echo hello" {
		t.Fatalf("unexpected command parts: %v", command)
	}
}

func TestContainerCommandTrimsEmptyItems(t *testing.T) {
	command := buildContainerCommand([]string{"  entrypoint ", "", " "})
	if len(command) != 1 || command[0] != "entrypoint" {
		t.Fatalf("unexpected command: %v", command)
	}
}
