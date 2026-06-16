package features

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cucumber/godog"
	"github.com/eval-hub/eval-hub/internal/eval_hub/runtimes/k8s"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	envTestHardwareProfile              = "TEST_HARDWARE_PROFILE"
	envTestHardwareProfileCPURequest    = "TEST_HARDWARE_PROFILE_CPU_REQUEST"
	envTestHardwareProfileMemoryRequest = "TEST_HARDWARE_PROFILE_MEMORY_REQUEST"
	envTestHardwareProfileCPULimit      = "TEST_HARDWARE_PROFILE_CPU_LIMIT"
	envTestHardwareProfileMemoryLimit   = "TEST_HARDWARE_PROFILE_MEMORY_LIMIT"
	fvtHardwareProfileJobWaitTimeout    = 2 * time.Minute
)

var testHardwareProfileRequiredEnv = []string{
	envTestHardwareProfile,
	envTestHardwareProfileCPURequest,
	envTestHardwareProfileMemoryRequest,
	envTestHardwareProfileCPULimit,
	envTestHardwareProfileMemoryLimit,
}

type hardwareProfileScenarioState struct {
	helper *k8s.KubernetesHelper
}

func (s *hardwareProfileScenarioState) reset() {
	s.helper = nil
}

func (tc *scenarioConfig) tenantNamespace() string {
	namespace := tc.reqHeaders["X-Tenant"]
	if namespace == "" {
		namespace = "test-tenant"
	}
	return namespace
}

func (s *hardwareProfileScenarioState) initHelper() error {
	if s.helper != nil {
		return nil
	}
	helper, err := k8s.NewKubernetesHelper()
	if err != nil {
		return fmt.Errorf("create kubernetes helper: %w", err)
	}
	s.helper = helper
	return nil
}

func (tc *scenarioConfig) testHardwareProfileIsConfigured() error {
	var missing []string
	for _, name := range testHardwareProfileRequiredEnv {
		if strings.TrimSpace(os.Getenv(name)) == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		tc.logDebug(
			"Skipping scenario: missing hardware profile test env: %s (pipeline should verify the HardwareProfile CRD, create or select a profile, and export name plus expected adapter resources)\n",
			strings.Join(missing, ", "),
		)
		return godog.ErrSkip
	}
	logDebug(
		"Using test hardware profile %q with adapter resources cpu=%s/%s memory=%s/%s\n",
		os.Getenv(envTestHardwareProfile),
		os.Getenv(envTestHardwareProfileCPURequest),
		os.Getenv(envTestHardwareProfileCPULimit),
		os.Getenv(envTestHardwareProfileMemoryRequest),
		os.Getenv(envTestHardwareProfileMemoryLimit),
	)
	return nil
}

func (tc *scenarioConfig) waitForKubernetesEvaluationJob(state *hardwareProfileScenarioState) error {
	if tc.lastId == "" {
		return tc.logError(fmt.Errorf("no evaluation job ID found"))
	}
	if err := state.initHelper(); err != nil {
		return tc.logError(err)
	}

	namespace := tc.tenantNamespace()
	labelSelector := fmt.Sprintf("job_id=%s", tc.lastId)
	ctx, cancel := context.WithTimeout(context.Background(), fvtHardwareProfileJobWaitTimeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return tc.logError(fmt.Errorf("timeout waiting for Kubernetes Job for evaluation job %s in namespace %s", tc.lastId, namespace))
		case <-ticker.C:
			jobs, err := state.helper.ListJobs(context.Background(), namespace, labelSelector)
			if err != nil {
				return tc.logError(fmt.Errorf("list jobs for evaluation job %s: %w", tc.lastId, err))
			}
			if len(jobs) >= 1 {
				logDebug("Kubernetes Job created for evaluation job %s: %s\n", tc.lastId, jobs[0].Name)
				return nil
			}
		}
	}
}

func findAdapterContainer(job *batchv1.Job) (*corev1.Container, error) {
	if job == nil {
		return nil, fmt.Errorf("job is required")
	}
	for i := range job.Spec.Template.Spec.Containers {
		if job.Spec.Template.Spec.Containers[i].Name == "adapter" {
			return &job.Spec.Template.Spec.Containers[i], nil
		}
	}
	return nil, fmt.Errorf("adapter container not found in job %s", job.Name)
}

func (tc *scenarioConfig) jobAdapterContainerShouldHaveResourceRequest(state *hardwareProfileScenarioState, resourceName, expectedValue string) error {
	return tc.jobAdapterContainerShouldHaveResource(state, "request", resourceName, expectedValue)
}

func (tc *scenarioConfig) jobAdapterContainerShouldHaveResourceLimit(state *hardwareProfileScenarioState, resourceName, expectedValue string) error {
	return tc.jobAdapterContainerShouldHaveResource(state, "limit", resourceName, expectedValue)
}

func (tc *scenarioConfig) jobAdapterContainerShouldHaveResource(
	state *hardwareProfileScenarioState,
	kind, resourceName, expectedValue string,
) error {
	if tc.lastId == "" {
		return tc.logError(fmt.Errorf("no evaluation job ID found"))
	}
	expectedValue, err := tc.getValue(expectedValue)
	if err != nil {
		return tc.logError(err)
	}
	if err := state.initHelper(); err != nil {
		return tc.logError(err)
	}

	namespace := tc.tenantNamespace()
	labelSelector := fmt.Sprintf("job_id=%s", tc.lastId)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	jobs, err := state.helper.ListJobs(ctx, namespace, labelSelector)
	if err != nil {
		return tc.logError(fmt.Errorf("list jobs for evaluation job %s: %w", tc.lastId, err))
	}
	if len(jobs) == 0 {
		return tc.logError(fmt.Errorf("no Kubernetes Job found for evaluation job %s in namespace %s", tc.lastId, namespace))
	}

	adapter, err := findAdapterContainer(&jobs[0])
	if err != nil {
		return tc.logError(err)
	}

	expectedQty, err := resource.ParseQuantity(expectedValue)
	if err != nil {
		return tc.logError(fmt.Errorf("parse expected %s quantity %q: %w", resourceName, expectedValue, err))
	}

	var actualQty resource.Quantity
	switch kind {
	case "request":
		switch resourceName {
		case "cpu":
			actualQty = *adapter.Resources.Requests.Cpu()
		case "memory":
			actualQty = *adapter.Resources.Requests.Memory()
		default:
			return tc.logError(fmt.Errorf("unsupported request resource %q", resourceName))
		}
	case "limit":
		switch resourceName {
		case "cpu":
			actualQty = *adapter.Resources.Limits.Cpu()
		case "memory":
			actualQty = *adapter.Resources.Limits.Memory()
		default:
			return tc.logError(fmt.Errorf("unsupported limit resource %q", resourceName))
		}
	default:
		return tc.logError(fmt.Errorf("unsupported resource kind %q", kind))
	}

	if actualQty.Cmp(expectedQty) != 0 {
		return tc.logError(fmt.Errorf(
			"expected adapter %s %s to be %s, got %s (job %s)",
			kind, resourceName, expectedQty.String(), actualQty.String(), jobs[0].Name,
		))
	}
	logDebug("Verified adapter %s %s = %s on job %s\n", kind, resourceName, actualQty.String(), jobs[0].Name)
	return nil
}

func InitializeHardwareProfileSteps(ctx *godog.ScenarioContext, tc *scenarioConfig) {
	state := &hardwareProfileScenarioState{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		for _, t := range sc.Tags {
			if strings.TrimPrefix(t.Name, "@") == "hardware_profile" {
				state.reset()
				break
			}
		}
		return ctx, nil
	})

	ctx.Step(`^the test hardware profile is configured$`, tc.testHardwareProfileIsConfigured)
	ctx.Step(`^I wait for the Kubernetes evaluation Job to be created$`, func() error {
		return tc.waitForKubernetesEvaluationJob(state)
	})
	ctx.Step(`^the Job adapter container should have CPU request "([^"]*)"$`, func(expected string) error {
		return tc.jobAdapterContainerShouldHaveResourceRequest(state, "cpu", expected)
	})
	ctx.Step(`^the Job adapter container should have memory request "([^"]*)"$`, func(expected string) error {
		return tc.jobAdapterContainerShouldHaveResourceRequest(state, "memory", expected)
	})
	ctx.Step(`^the Job adapter container should have CPU limit "([^"]*)"$`, func(expected string) error {
		return tc.jobAdapterContainerShouldHaveResourceLimit(state, "cpu", expected)
	})
	ctx.Step(`^the Job adapter container should have memory limit "([^"]*)"$`, func(expected string) error {
		return tc.jobAdapterContainerShouldHaveResourceLimit(state, "memory", expected)
	})
}
