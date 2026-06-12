package features

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cucumber/godog"
	"github.com/eval-hub/eval-hub/internal/eval_hub/runtimes/k8s"
	"github.com/google/uuid"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	hardwareProfileFVTTag            = "hardware_profile"
	hardwareProfileNameValueKey      = "hardware_profile_name"
	fvtHardwareProfileJobWaitTimeout = 2 * time.Minute
)

var (
	fvtHardwareProfileGVR = schema.GroupVersionResource{
		Group:    "infrastructure.opendatahub.io",
		Version:  "v1",
		Resource: "hardwareprofiles",
	}
	fvtCRDGVR = schema.GroupVersionResource{
		Group:    "apiextensions.k8s.io",
		Version:  "v1",
		Resource: "customresourcedefinitions",
	}
)

type hardwareProfileScenarioState struct {
	helper        *k8s.KubernetesHelper
	dynamicClient dynamic.Interface
	namespace     string
	name          string
}

func (s *hardwareProfileScenarioState) reset() {
	s.helper = nil
	s.dynamicClient = nil
	s.namespace = ""
	s.name = ""
}

func loadFVTKubernetesConfig() (*rest.Config, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
		configOverrides := &clientcmd.ConfigOverrides{}
		config, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			loadingRules,
			configOverrides,
		).ClientConfig()
		if err != nil {
			return nil, err
		}
	}
	return config, nil
}

func (s *hardwareProfileScenarioState) initDynamicClient() error {
	if s.dynamicClient != nil {
		return nil
	}
	config, err := loadFVTKubernetesConfig()
	if err != nil {
		return fmt.Errorf("load kubernetes config: %w", err)
	}
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("create dynamic kubernetes client: %w", err)
	}
	s.dynamicClient = dynamicClient
	return nil
}

func createFVTHardwareProfile(ctx context.Context, dynamicClient dynamic.Interface, namespace string, profile *unstructured.Unstructured) error {
	if dynamicClient == nil {
		return fmt.Errorf("dynamic kubernetes client is required")
	}
	if namespace == "" {
		return fmt.Errorf("namespace is required")
	}
	if profile == nil {
		return fmt.Errorf("hardware profile is required")
	}
	_, err := dynamicClient.Resource(fvtHardwareProfileGVR).Namespace(namespace).Create(ctx, profile, metav1.CreateOptions{})
	return err
}

func deleteFVTHardwareProfile(ctx context.Context, dynamicClient dynamic.Interface, namespace, name string) error {
	if dynamicClient == nil {
		return fmt.Errorf("dynamic kubernetes client is required")
	}
	return dynamicClient.Resource(fvtHardwareProfileGVR).Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

func fvtHardwareProfileCRDInstalled(ctx context.Context, dynamicClient dynamic.Interface) (bool, error) {
	if dynamicClient == nil {
		return false, fmt.Errorf("dynamic kubernetes client is required")
	}
	_, err := dynamicClient.Resource(fvtCRDGVR).Get(ctx, "hardwareprofiles.infrastructure.opendatahub.io", metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *hardwareProfileScenarioState) cleanup() {
	if s.dynamicClient == nil || s.namespace == "" || s.name == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := deleteFVTHardwareProfile(ctx, s.dynamicClient, s.namespace, s.name); err != nil {
		logDebug("WARNING: failed to delete test HardwareProfile %s in %s: %v\n", s.name, s.namespace, err)
	}
}

func scenarioHasTag(sc *godog.Scenario, tag string) bool {
	for _, t := range sc.Tags {
		if strings.TrimPrefix(t.Name, "@") == tag {
			return true
		}
	}
	return false
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

func buildFVTHardwareProfile(namespace, name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "infrastructure.opendatahub.io/v1",
			"kind":       "HardwareProfile",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]any{
				"identifiers": []any{
					map[string]any{
						"identifier":   "cpu",
						"displayName":  "CPU",
						"resourceType": "CPU",
						"minCount":     int64(1),
						"defaultCount": int64(1),
						"maxCount":     int64(2),
					},
					map[string]any{
						"identifier":   "memory",
						"displayName":  "Memory",
						"resourceType": "Memory",
						"minCount":     "1Gi",
						"defaultCount": "1Gi",
						"maxCount":     "2Gi",
					},
				},
			},
		},
	}
}

func (tc *scenarioConfig) hardwareProfileCRDIsInstalled(state *hardwareProfileScenarioState) error {
	if err := state.initDynamicClient(); err != nil {
		return tc.logError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	installed, err := fvtHardwareProfileCRDInstalled(ctx, state.dynamicClient)
	if err != nil {
		return tc.logError(fmt.Errorf("check HardwareProfile CRD: %w", err))
	}
	if !installed {
		tc.logDebug("Skipping scenario: hardwareprofiles.infrastructure.opendatahub.io CRD is not installed\n")
		return godog.ErrSkip
	}
	return nil
}

func (tc *scenarioConfig) createTestHardwareProfileInTenantNamespace(state *hardwareProfileScenarioState) error {
	if err := state.initDynamicClient(); err != nil {
		return tc.logError(err)
	}
	namespace := tc.tenantNamespace()
	name := fmt.Sprintf("fvt-hwp-%s", uuid.NewString()[:8])
	profile := buildFVTHardwareProfile(namespace, name)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := createFVTHardwareProfile(ctx, state.dynamicClient, namespace, profile); err != nil {
		return tc.logError(fmt.Errorf("create test HardwareProfile %q in namespace %q: %w", name, namespace, err))
	}

	state.namespace = namespace
	state.name = name
	tc.saveValue(hardwareProfileNameValueKey, name)
	logDebug("Created test HardwareProfile %s in namespace %s\n", name, namespace)
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
	for _, container := range job.Spec.Template.Spec.Containers {
		if container.Name == "adapter" {
			return &container, nil
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
		if !scenarioHasTag(sc, hardwareProfileFVTTag) {
			return ctx, nil
		}
		state.reset()
		return ctx, nil
	})

	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if scenarioHasTag(sc, hardwareProfileFVTTag) {
			state.cleanup()
		}
		return ctx, err
	})

	ctx.Step(`^the HardwareProfile CRD is installed on the cluster$`, func() error {
		return tc.hardwareProfileCRDIsInstalled(state)
	})
	ctx.Step(`^a test HardwareProfile is created in the tenant namespace$`, func() error {
		return tc.createTestHardwareProfileInTenantNamespace(state)
	})
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
