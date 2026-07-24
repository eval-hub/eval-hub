package features

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// ============================================================================
// Container Steps
// ============================================================================

// firstContainer returns the first pod-template container on the current Job.
func (tc *testContext) firstContainer() (*corev1.Container, error) {
	if tc.currentJob == nil {
		return nil, fmt.Errorf("no current Job")
	}
	if len(tc.currentJob.Spec.Template.Spec.Containers) == 0 {
		return nil, fmt.Errorf("Job %s has no containers", tc.currentJob.Name)
	}
	return &tc.currentJob.Spec.Template.Spec.Containers[0], nil
}

func (tc *testContext) jobPodTemplateShouldHaveContainer(containerName string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}

	for _, container := range tc.currentJob.Spec.Template.Spec.Containers {
		if container.Name == containerName {
			return nil
		}
	}

	return fmt.Errorf("Job %s does not have container named %s", tc.currentJob.Name, containerName)
}

func (tc *testContext) containerShouldHaveValue(field, value string) error {
	container, err := tc.firstContainer()
	if err != nil {
		return err
	}

	if field == "imagePullPolicy" {
		actualValue := string(container.ImagePullPolicy)
		if actualValue != value {
			return fmt.Errorf("Container %s imagePullPolicy expected %s, got %s", container.Name, value, actualValue)
		}
		return nil
	}

	return fmt.Errorf("unknown container field %s", field)
}

func (tc *testContext) containerShouldHaveImage() error {
	container, err := tc.firstContainer()
	if err != nil {
		return err
	}
	if container.Image == "" {
		return fmt.Errorf("Container %s has no image", container.Name)
	}
	return nil
}

func (tc *testContext) containerSecurityContextShouldHaveBoolValue(field, value string) error {
	container, err := tc.firstContainer()
	if err != nil {
		return err
	}
	if container.SecurityContext == nil {
		return fmt.Errorf("Container %s has no securityContext", container.Name)
	}

	var expectedValue bool
	switch value {
	case "true":
		expectedValue = true
	case "false":
		expectedValue = false
	default:
		return fmt.Errorf("invalid boolean value %q, expected \"true\" or \"false\"", value)
	}

	if field == "allowPrivilegeEscalation" {
		if container.SecurityContext.AllowPrivilegeEscalation == nil {
			return fmt.Errorf("Container %s securityContext has no %s", container.Name, field)
		}
		if *container.SecurityContext.AllowPrivilegeEscalation != expectedValue {
			return fmt.Errorf("Container %s %s expected %v, got %v", container.Name, field, expectedValue, *container.SecurityContext.AllowPrivilegeEscalation)
		}
		return nil
	}

	if field == "runAsNonRoot" {
		if container.SecurityContext.RunAsNonRoot == nil {
			return fmt.Errorf("Container %s securityContext has no %s", container.Name, field)
		}
		if *container.SecurityContext.RunAsNonRoot != expectedValue {
			return fmt.Errorf("Container %s %s expected %v, got %v", container.Name, field, expectedValue, *container.SecurityContext.RunAsNonRoot)
		}
		return nil
	}

	return fmt.Errorf("unknown securityContext field %s", field)
}

func (tc *testContext) containerSecurityContextCapabilitiesShouldDrop(capability string) error {
	container, err := tc.firstContainer()
	if err != nil {
		return err
	}
	if container.SecurityContext == nil || container.SecurityContext.Capabilities == nil {
		return fmt.Errorf("Container %s has no capabilities", container.Name)
	}

	for _, cap := range container.SecurityContext.Capabilities.Drop {
		if string(cap) == capability {
			return nil
		}
	}

	return fmt.Errorf("Container %s does not drop capability %s", container.Name, capability)
}

func (tc *testContext) containerSecurityContextSeccompProfile(profileType string) error {
	container, err := tc.firstContainer()
	if err != nil {
		return err
	}
	if container.SecurityContext == nil || container.SecurityContext.SeccompProfile == nil {
		return fmt.Errorf("Container %s has no seccomp profile", container.Name)
	}

	actualType := string(container.SecurityContext.SeccompProfile.Type)
	if actualType != profileType {
		return fmt.Errorf("Container %s seccomp profile type expected %s, got %s", container.Name, profileType, actualType)
	}

	return nil
}

func (tc *testContext) containerShouldHaveCPURequestSet() error {
	container, err := tc.firstContainer()
	if err != nil {
		return err
	}
	cpuRequest := container.Resources.Requests.Cpu()
	if cpuRequest == nil || cpuRequest.IsZero() {
		return fmt.Errorf("Container %s has no CPU request", container.Name)
	}
	return nil
}

func (tc *testContext) containerShouldHaveMemoryRequestSet() error {
	container, err := tc.firstContainer()
	if err != nil {
		return err
	}
	memRequest := container.Resources.Requests.Memory()
	if memRequest == nil || memRequest.IsZero() {
		return fmt.Errorf("Container %s has no memory request", container.Name)
	}
	return nil
}

func (tc *testContext) containerShouldHaveCPULimitSet() error {
	container, err := tc.firstContainer()
	if err != nil {
		return err
	}
	cpuLimit := container.Resources.Limits.Cpu()
	if cpuLimit == nil || cpuLimit.IsZero() {
		return fmt.Errorf("Container %s has no CPU limit", container.Name)
	}
	return nil
}

func (tc *testContext) containerShouldHaveMemoryLimitSet() error {
	container, err := tc.firstContainer()
	if err != nil {
		return err
	}
	memLimit := container.Resources.Limits.Memory()
	if memLimit == nil || memLimit.IsZero() {
		return fmt.Errorf("Container %s has no memory limit", container.Name)
	}
	return nil
}

func (tc *testContext) jobPodTemplateShouldHaveServiceAccountFromSA() error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}
	_, err := tc.instanceNameFromServiceAccount()
	return err
}

func (tc *testContext) volumeShouldReferenceConfigMapFromSA(volumeName string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}
	envValue, err := tc.instanceNameFromServiceAccount()
	if err != nil {
		return err
	}
	expected := envValue + "-service-ca"
	return tc.volumeShouldReferenceConfigMapByName(volumeName, expected)
}

func (tc *testContext) instanceNameFromServiceAccount() (string, error) {
	if tc.currentJob == nil {
		return "", fmt.Errorf("no current Job")
	}
	serviceAccount := tc.currentJob.Spec.Template.Spec.ServiceAccountName
	// SA format is "{instanceName}-{namespace}-job"
	suffix := "-" + tc.namespace + "-job"
	if !strings.HasSuffix(serviceAccount, suffix) {
		return "", fmt.Errorf("unable to derive instance name from serviceAccountName %q", serviceAccount)
	}
	return strings.TrimSuffix(serviceAccount, suffix), nil
}
