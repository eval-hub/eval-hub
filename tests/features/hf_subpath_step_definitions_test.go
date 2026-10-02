package features

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/cucumber/godog"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const hfSubpathBenchmarkID = "truthfulqa_mc1"

type hfSubpathScenarioState struct {
	k8s *fvtK8sClient
}

func (s *hfSubpathScenarioState) reset() {
	s.k8s = nil
}

func (s *hfSubpathScenarioState) initHelper() error {
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

func validateHFSubpathRelativePath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || path.IsAbs(value) {
		return "", fmt.Errorf("path must be a non-empty relative path: %q", value)
	}
	clean := path.Clean(value)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("path must stay below /test_data: %q", value)
	}
	return clean, nil
}

func (tc *scenarioConfig) hfSubpathFileShouldExist(state *hfSubpathScenarioState, expectedPath string) error {
	return tc.checkHFSubpathPath(state, expectedPath, true)
}

func (tc *scenarioConfig) hfSubpathPathShouldNotExist(state *hfSubpathScenarioState, excludedPath string) error {
	return tc.checkHFSubpathPath(state, excludedPath, false)
}

func (tc *scenarioConfig) hfSubpathShouldStageOnlySelectedFiles(state *hfSubpathScenarioState, expectedPath, excludedPath string) error {
	if tc.lastId == "" {
		return tc.logError(fmt.Errorf("no evaluation job ID found"))
	}
	if err := state.initHelper(); err != nil {
		return tc.logError(err)
	}
	expectedPath, err := tc.getValue(expectedPath)
	if err != nil {
		return tc.logError(err)
	}
	expectedPath, err = validateHFSubpathRelativePath(expectedPath)
	if err != nil {
		return tc.logError(err)
	}
	excludedPath, err = tc.getValue(excludedPath)
	if err != nil {
		return tc.logError(err)
	}
	excludedPath, err = validateHFSubpathRelativePath(excludedPath)
	if err != nil {
		return tc.logError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	command := []string{
		"sh",
		"-c",
		`test -f "$1" && test ! -e "$2"`,
		"sh",
		path.Join(testDataMountPath, expectedPath),
		path.Join(testDataMountPath, excludedPath),
	}
	labelSelector := fmt.Sprintf("job_id=%s,benchmark_id=%s", tc.lastId, hfSubpathBenchmarkID)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	var lastExecErr error
	for {
		pods, listErr := state.k8s.clientset.CoreV1().Pods(tc.tenantNamespace()).List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
		if listErr != nil {
			return tc.logError(fmt.Errorf("list HF sub-path benchmark pods: %w", listErr))
		}
		for _, pod := range pods.Items {
			if pod.Status.Phase == corev1.PodFailed {
				if lastExecErr != nil {
					return tc.logError(fmt.Errorf("HF sub-path benchmark pod %s failed before path inspection: %w", pod.Name, lastExecErr))
				}
				return tc.logError(fmt.Errorf("HF sub-path benchmark pod %s failed before path inspection", pod.Name))
			}
			if pod.Status.Phase == corev1.PodSucceeded {
				return tc.logError(fmt.Errorf("HF sub-path benchmark pod %s completed before path inspection; the /test_data volume is no longer exec-accessible", pod.Name))
			}
			if pod.Status.Phase != corev1.PodRunning || !adapterRunning(pod.Status.ContainerStatuses) {
				continue
			}

			_, stderr, execErr := state.k8s.execInPod(ctx, tc.tenantNamespace(), pod.Name, adapterContainerName, command)
			if execErr == nil {
				logDebug("Verified HF sub-path staging: %s exists and %s does not exist in pod %s/%s\n", expectedPath, excludedPath, tc.tenantNamespace(), pod.Name)
				return nil
			}
			lastExecErr = fmt.Errorf("pod %s: %w (stderr: %s)", pod.Name, execErr, strings.TrimSpace(stderr))
		}

		select {
		case <-ctx.Done():
			if lastExecErr != nil {
				return tc.logError(fmt.Errorf("timed out verifying HF sub-path staging under %s: %w", testDataMountPath, lastExecErr))
			}
			return tc.logError(fmt.Errorf("timed out verifying HF sub-path staging under %s", testDataMountPath))
		case <-ticker.C:
		}
	}
}

func (tc *scenarioConfig) checkHFSubpathPath(state *hfSubpathScenarioState, relativePath string, shouldExist bool) error {
	if tc.lastId == "" {
		return tc.logError(fmt.Errorf("no evaluation job ID found"))
	}
	if err := state.initHelper(); err != nil {
		return tc.logError(err)
	}
	relativePath, err := tc.getValue(relativePath)
	if err != nil {
		return tc.logError(err)
	}
	relativePath, err = validateHFSubpathRelativePath(relativePath)
	if err != nil {
		return tc.logError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	command := []string{"test"}
	if shouldExist {
		command = append(command, "-f")
	} else {
		command = append(command, "!", "-e")
	}
	command = append(command, path.Join(testDataMountPath, relativePath))

	labelSelector := fmt.Sprintf("job_id=%s,benchmark_id=%s", tc.lastId, hfSubpathBenchmarkID)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	var lastExecErr error
	for {
		pods, listErr := state.k8s.clientset.CoreV1().Pods(tc.tenantNamespace()).List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
		if listErr != nil {
			return tc.logError(fmt.Errorf("list HF sub-path benchmark pods: %w", listErr))
		}
		for _, pod := range pods.Items {
			if pod.Status.Phase == corev1.PodFailed {
				if lastExecErr != nil {
					return tc.logError(fmt.Errorf("HF sub-path benchmark pod %s failed before path inspection: %w", pod.Name, lastExecErr))
				}
				return tc.logError(fmt.Errorf("HF sub-path benchmark pod %s failed before path inspection", pod.Name))
			}
			if pod.Status.Phase == corev1.PodSucceeded {
				return tc.logError(fmt.Errorf("HF sub-path benchmark pod %s completed before path inspection; the /test_data volume is no longer exec-accessible", pod.Name))
			}
			if pod.Status.Phase != corev1.PodRunning || !adapterRunning(pod.Status.ContainerStatuses) {
				continue
			}

			_, stderr, execErr := state.k8s.execInPod(ctx, tc.tenantNamespace(), pod.Name, adapterContainerName, command)
			if execErr == nil {
				logDebug("Verified HF sub-path path %s %s in pod %s/%s\n", relativePath, map[bool]string{true: "exists", false: "does not exist"}[shouldExist], tc.tenantNamespace(), pod.Name)
				return nil
			}
			lastExecErr = fmt.Errorf("pod %s: %w (stderr: %s)", pod.Name, execErr, strings.TrimSpace(stderr))
		}

		select {
		case <-ctx.Done():
			condition := "exist"
			if !shouldExist {
				condition = "not exist"
			}
			if lastExecErr != nil {
				return tc.logError(fmt.Errorf("timed out checking whether %s should %s under %s: %w", relativePath, condition, testDataMountPath, lastExecErr))
			}
			return tc.logError(fmt.Errorf("timed out checking whether %s should %s under %s", relativePath, condition, testDataMountPath))
		case <-ticker.C:
		}
	}
}

func adapterRunning(statuses []corev1.ContainerStatus) bool {
	for _, status := range statuses {
		if status.Name == adapterContainerName {
			return status.State.Running != nil
		}
	}
	return false
}

func InitializeHFSubpathSteps(ctx *godog.ScenarioContext, tc *scenarioConfig) {
	state := &hfSubpathScenarioState{}
	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		state.reset()
		return ctx, nil
	})
	ctx.Step(`^the HF sub-path benchmark should contain file "([^"]*)" under "/test_data"$`, func(expectedPath string) error {
		return tc.hfSubpathFileShouldExist(state, expectedPath)
	})
	ctx.Step(`^the HF sub-path benchmark should not contain path "([^"]*)" under "/test_data"$`, func(excludedPath string) error {
		return tc.hfSubpathPathShouldNotExist(state, excludedPath)
	})
	ctx.Step(`^the HF sub-path benchmark should contain file "([^"]*)" and not contain path "([^"]*)" under "/test_data"$`, func(expectedPath, excludedPath string) error {
		return tc.hfSubpathShouldStageOnlySelectedFiles(state, expectedPath, excludedPath)
	})
}
