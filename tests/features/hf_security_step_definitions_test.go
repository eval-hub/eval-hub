package features

import (
	"context"
	"fmt"
	"time"

	"github.com/cucumber/godog"
	corev1 "k8s.io/api/core/v1"
)

const (
	hfAuthVolumeName     = "test-data-hf-auth"
	testDataVolumeName   = "test-data"
	testDataMountPath    = "/test_data"
	hfInitContainerName  = "init"
	adapterContainerName = "adapter"
)

type hfSecurityScenarioState struct {
	k8s *fvtK8sClient
}

func (s *hfSecurityScenarioState) reset() {
	s.k8s = nil
}

func (s *hfSecurityScenarioState) initHelper() error {
	if s.k8s != nil {
		return nil
	}
	client, err := newFVTK8sClient()
	if err != nil {
		return fmt.Errorf("create fvt kubernetes client: %w", err)
	}
	s.k8s = client
	return nil
}

func findContainerByName(containers []corev1.Container, name string) (*corev1.Container, error) {
	for i := range containers {
		if containers[i].Name == name {
			return &containers[i], nil
		}
	}
	return nil, fmt.Errorf("container %q not found", name)
}

func containerMountsVolume(container *corev1.Container, volumeName, mountPath string) bool {
	for _, mount := range container.VolumeMounts {
		if mount.Name == volumeName && (mountPath == "" || mount.MountPath == mountPath) {
			return true
		}
	}
	return false
}

func (tc *scenarioConfig) hfSecretShouldBeMountedOnlyInInitContainer(state *hfSecurityScenarioState, expectedSecret string) error {
	if tc.lastId == "" {
		return tc.logError(fmt.Errorf("no evaluation job ID found"))
	}
	if err := state.initHelper(); err != nil {
		return tc.logError(err)
	}
	expectedSecret, err := tc.getValue(expectedSecret)
	if err != nil {
		return tc.logError(err)
	}

	namespace := tc.tenantNamespace()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	jobs, err := state.k8s.listJobs(ctx, namespace, fmt.Sprintf("job_id=%s,benchmark_id=arc_easy", tc.lastId))
	if err != nil {
		return tc.logError(fmt.Errorf("list jobs for evaluation job %s: %w", tc.lastId, err))
	}
	if len(jobs) == 0 {
		return tc.logError(fmt.Errorf("no Kubernetes Job found for evaluation job %s in namespace %s", tc.lastId, namespace))
	}
	if len(jobs) != 1 {
		return tc.logError(fmt.Errorf("expected exactly one Kubernetes Job for evaluation job %s and benchmark arc_easy, found %d", tc.lastId, len(jobs)))
	}
	job := &jobs[0]
	podSpec := job.Spec.Template.Spec

	var authVolume *corev1.Volume
	for i := range podSpec.Volumes {
		if podSpec.Volumes[i].Name == hfAuthVolumeName {
			authVolume = &podSpec.Volumes[i]
			break
		}
	}
	if authVolume == nil || authVolume.Secret == nil {
		return tc.logError(fmt.Errorf("Job %s does not define Secret volume %q", job.Name, hfAuthVolumeName))
	}
	if authVolume.Secret.SecretName != expectedSecret {
		return tc.logError(fmt.Errorf("HF Secret volume %q references %q, expected %q", hfAuthVolumeName, authVolume.Secret.SecretName, expectedSecret))
	}

	initContainer, err := findContainerByName(podSpec.InitContainers, hfInitContainerName)
	if err != nil {
		return tc.logError(fmt.Errorf("Job %s: %w", job.Name, err))
	}
	if !containerMountsVolume(initContainer, hfAuthVolumeName, "") {
		return tc.logError(fmt.Errorf("HF init container does not mount Secret volume %q", hfAuthVolumeName))
	}

	for _, container := range podSpec.InitContainers {
		if container.Name != hfInitContainerName && containerMountsVolume(&container, hfAuthVolumeName, "") {
			return tc.logError(fmt.Errorf("container %q must not mount HF Secret volume %q", container.Name, hfAuthVolumeName))
		}
	}
	for _, container := range podSpec.Containers {
		if containerMountsVolume(&container, hfAuthVolumeName, "") {
			return tc.logError(fmt.Errorf("container %q must not mount HF Secret volume %q", container.Name, hfAuthVolumeName))
		}
	}

	adapter, err := findContainerByName(podSpec.Containers, adapterContainerName)
	if err != nil {
		return tc.logError(fmt.Errorf("Job %s: %w", job.Name, err))
	}
	if !containerMountsVolume(adapter, testDataVolumeName, testDataMountPath) {
		return tc.logError(fmt.Errorf("adapter container does not mount volume %q at %s", testDataVolumeName, testDataMountPath))
	}

	logDebug("Verified HF Secret %q is mounted only in init container and test data is mounted at %s in adapter for Job %s\n", expectedSecret, testDataMountPath, job.Name)
	return nil
}

func InitializeHFSecuritySteps(ctx *godog.ScenarioContext, tc *scenarioConfig) {
	state := &hfSecurityScenarioState{}
	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		state.reset()
		return ctx, nil
	})
	ctx.Step(`^the HF Secret "([^"]*)" should be mounted only in the HF init container$`, func(secret string) error {
		return tc.hfSecretShouldBeMountedOnlyInInitContainer(state, secret)
	})
}
