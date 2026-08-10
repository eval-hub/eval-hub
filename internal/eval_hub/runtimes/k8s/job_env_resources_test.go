package k8s

import (
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/pkg/api"
	corev1 "k8s.io/api/core/v1"
)

func TestBuildJobAdapterImagePullPolicy(t *testing.T) {
	base := &jobConfig{
		jobID:          "job-pull",
		resourceGUID:   "guid-pull",
		benchmarkIndex: 0,
		namespace:      "default",
		providerID:     "provider-1",
		benchmarkID:    "bench-1",
		adapterImage:   "adapter:dev",
		defaultEnv:     []api.EnvVar{},
	}

	t.Run("uses IfNotPresent from job config", func(t *testing.T) {
		cfg := *base
		cfg.adapterPullPolicy = corev1.PullIfNotPresent
		job, err := buildJob(&cfg)
		if err != nil {
			t.Fatalf("buildJob: %v", err)
		}
		if got := job.Spec.Template.Spec.Containers[0].ImagePullPolicy; got != corev1.PullIfNotPresent {
			t.Fatalf("adapter ImagePullPolicy = %q, want IfNotPresent", got)
		}
	})

	t.Run("honors Always override", func(t *testing.T) {
		cfg := *base
		cfg.adapterPullPolicy = corev1.PullAlways
		job, err := buildJob(&cfg)
		if err != nil {
			t.Fatalf("buildJob: %v", err)
		}
		if got := job.Spec.Template.Spec.Containers[0].ImagePullPolicy; got != corev1.PullAlways {
			t.Fatalf("adapter ImagePullPolicy = %q, want Always", got)
		}
	})
}

func TestBuildJobAdapterEvalHubModeEnv(t *testing.T) {
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

func TestBuildResourcesGPURequest(t *testing.T) {
	cfg := &jobConfig{
		cpuRequest:    "250m",
		memoryRequest: "512Mi",
		cpuLimit:      "1",
		memoryLimit:   "2Gi",
		gpuResource:   "nvidia.com/gpu",
		gpuCount:      2,
	}
	resources, err := buildResources(cfg)
	if err != nil {
		t.Fatalf("buildResources returned error: %v", err)
	}
	gpu, ok := resources.Requests[corev1.ResourceName("nvidia.com/gpu")]
	if !ok {
		t.Fatalf("expected GPU resource %q in requests", "nvidia.com/gpu")
	}
	if gpu.Value() != 2 {
		t.Fatalf("expected GPU request 2, got %v", gpu.Value())
	}
	gpuLimit, ok := resources.Limits[corev1.ResourceName("nvidia.com/gpu")]
	if !ok {
		t.Fatalf("expected GPU resource %q in limits", "nvidia.com/gpu")
	}
	if gpuLimit.Value() != 2 {
		t.Fatalf("expected GPU limit 2 (must equal request), got %v", gpuLimit.Value())
	}
}

func TestBuildResourcesAMDGPURequest(t *testing.T) {
	cfg := &jobConfig{
		cpuRequest:    "250m",
		memoryRequest: "512Mi",
		cpuLimit:      "1",
		memoryLimit:   "2Gi",
		gpuResource:   "amd.com/gpu",
		gpuCount:      1,
	}
	resources, err := buildResources(cfg)
	if err != nil {
		t.Fatalf("buildResources returned error: %v", err)
	}
	if _, ok := resources.Requests[corev1.ResourceName("amd.com/gpu")]; !ok {
		t.Fatalf("expected amd.com/gpu in requests")
	}
	if _, ok := resources.Requests[corev1.ResourceName("nvidia.com/gpu")]; ok {
		t.Fatalf("expected no nvidia.com/gpu when amd.com/gpu specified")
	}
}

func TestBuildResourcesNoGPURequest(t *testing.T) {
	cfg := &jobConfig{
		cpuRequest:    "250m",
		memoryRequest: "512Mi",
		cpuLimit:      "1",
		memoryLimit:   "2Gi",
		gpuResource:   "",
		gpuCount:      0,
	}
	resources, err := buildResources(cfg)
	if err != nil {
		t.Fatalf("buildResources returned error: %v", err)
	}
	for name := range resources.Requests {
		if strings.HasSuffix(string(name), "/gpu") || strings.HasSuffix(string(name), ".gpu") {
			t.Fatalf("expected no GPU resource in requests, found %q", name)
		}
	}
}

func TestBuildJobGPUResourcesPropagated(t *testing.T) {
	cfg := &jobConfig{
		jobID:          "gpu-job",
		resourceGUID:   "guid-gpu",
		benchmarkIndex: 0,
		namespace:      "default",
		providerID:     "gpu-provider",
		benchmarkID:    "bench-gpu",
		adapterImage:   "adapter:latest",
		defaultEnv:     []api.EnvVar{},
		gpuResource:    "nvidia.com/gpu",
		gpuCount:       1,
	}
	job, err := buildJob(cfg)
	if err != nil {
		t.Fatalf("buildJob: %v", err)
	}
	adapter := findContainer(job.Spec.Template.Spec.Containers, adapterContainerName)
	if adapter == nil {
		t.Fatalf("adapter container not found")
	}
	if _, ok := adapter.Resources.Requests[corev1.ResourceName("nvidia.com/gpu")]; !ok {
		t.Fatalf("expected nvidia.com/gpu in adapter container requests")
	}
	if _, ok := adapter.Resources.Limits[corev1.ResourceName("nvidia.com/gpu")]; !ok {
		t.Fatalf("expected nvidia.com/gpu in adapter container limits")
	}
}

func TestBuildJobNodeSelector(t *testing.T) {
	sel := map[string]string{"nvidia.com/gpu.product": "NVIDIA-H100-SXM5-80GB"}
	cfg := &jobConfig{
		jobID:          "ns-job",
		resourceGUID:   "guid-ns",
		benchmarkIndex: 0,
		namespace:      "default",
		providerID:     "p",
		benchmarkID:    "b",
		adapterImage:   "adapter:latest",
		defaultEnv:     []api.EnvVar{},
		gpuResource:    "nvidia.com/gpu",
		gpuCount:       1,
		nodeSelector:   sel,
	}
	job, err := buildJob(cfg)
	if err != nil {
		t.Fatalf("buildJob: %v", err)
	}
	if job.Spec.Template.Spec.NodeSelector["nvidia.com/gpu.product"] != "NVIDIA-H100-SXM5-80GB" {
		t.Errorf("pod NodeSelector = %v, want H100 label", job.Spec.Template.Spec.NodeSelector)
	}
}

func TestBuildJobNoNodeSelectorWhenAbsent(t *testing.T) {
	cfg := &jobConfig{
		jobID:          "no-ns-job",
		resourceGUID:   "guid-no-ns",
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
	if len(job.Spec.Template.Spec.NodeSelector) != 0 {
		t.Errorf("expected empty NodeSelector, got %v", job.Spec.Template.Spec.NodeSelector)
	}
}

// TestBuildJobSATokenSidecarOnly verifies that:
//   - pod-level AutomountServiceAccountToken is explicitly disabled
//   - the evalhub-sa-token projected volume exists on the pod and is mounted in the sidecar
//   - the adapter container has no evalhub-sa-token mount
//   - the adapter has the pod-namespace DownwardAPI volume mounted at k8sSAMountPath
