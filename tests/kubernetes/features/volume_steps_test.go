package features

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// ============================================================================
// Volume & Mount Steps
// ============================================================================

func (tc *testContext) jobPodTemplateShouldHaveConfigMapVolume(volumeName string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}

	for _, vol := range tc.currentJob.Spec.Template.Spec.Volumes {
		if vol.Name == volumeName && vol.ConfigMap != nil {
			return nil
		}
	}

	return fmt.Errorf("Job %s does not have ConfigMap volume %s", tc.currentJob.Name, volumeName)
}

func (tc *testContext) jobPodTemplateShouldHaveEmptyDirVolume(volumeName string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}

	for _, vol := range tc.currentJob.Spec.Template.Spec.Volumes {
		if vol.Name == volumeName && vol.EmptyDir != nil {
			return nil
		}
	}

	return fmt.Errorf("Job %s does not have EmptyDir volume %s", tc.currentJob.Name, volumeName)
}

func (tc *testContext) volumeShouldReferenceConfigMap(volumeName, suffix string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}

	for _, vol := range tc.currentJob.Spec.Template.Spec.Volumes {
		if vol.Name == volumeName && vol.ConfigMap != nil {
			if strings.HasSuffix(vol.ConfigMap.Name, suffix) {
				return nil
			}
		}
	}

	return fmt.Errorf("Job %s volume %s does not reference ConfigMap with suffix %s", tc.currentJob.Name, volumeName, suffix)
}

func (tc *testContext) containerShouldHaveVolumeMount(mountName, path string) error {
	container, err := tc.firstContainer()
	if err != nil {
		return err
	}
	for _, mount := range container.VolumeMounts {
		if mount.Name == mountName && mount.MountPath == path {
			return nil
		}
	}

	return fmt.Errorf("Container %s does not have volumeMount %s at path %s", container.Name, mountName, path)
}

func (tc *testContext) containerShouldHaveVolumeMountWithSubPath(mountName, path, subPath string) error {
	container, err := tc.firstContainer()
	if err != nil {
		return err
	}
	for _, mount := range container.VolumeMounts {
		if mount.Name == mountName && mount.MountPath == path && mount.SubPath == subPath {
			return nil
		}
	}
	return fmt.Errorf("Container %s does not have volumeMount %s at path %s with subPath %s", container.Name, mountName, path, subPath)
}

func (tc *testContext) sidecarContainerShouldHaveVolumeMount(mountName, path string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}
	var sidecar *corev1.Container
	for i := range tc.currentJob.Spec.Template.Spec.Containers {
		if tc.currentJob.Spec.Template.Spec.Containers[i].Name == "sidecar" {
			sidecar = &tc.currentJob.Spec.Template.Spec.Containers[i]
			break
		}
	}
	if sidecar == nil {
		return fmt.Errorf("Job %s has no container named %q", tc.currentJob.Name, "sidecar")
	}
	for _, mount := range sidecar.VolumeMounts {
		if mount.Name == mountName && mount.MountPath == path {
			return nil
		}
	}
	return fmt.Errorf("sidecar container does not have volumeMount %s at path %s", mountName, path)
}

func (tc *testContext) sidecarContainerShouldHaveVolumeMountWithSubPath(mountName, path, subPath string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}
	var sidecar *corev1.Container
	for i := range tc.currentJob.Spec.Template.Spec.Containers {
		if tc.currentJob.Spec.Template.Spec.Containers[i].Name == "sidecar" {
			sidecar = &tc.currentJob.Spec.Template.Spec.Containers[i]
			break
		}
	}
	if sidecar == nil {
		return fmt.Errorf("Job %s has no container named %q", tc.currentJob.Name, "sidecar")
	}
	for _, mount := range sidecar.VolumeMounts {
		if mount.Name == mountName && mount.MountPath == path && mount.SubPath == subPath {
			return nil
		}
	}
	return fmt.Errorf("sidecar container does not have volumeMount %s at path %s with subPath %s", mountName, path, subPath)
}

func (tc *testContext) volumeMountShouldHaveSubPath(mountName, subPath string) error {
	container, err := tc.firstContainer()
	if err != nil {
		return err
	}
	for _, mount := range container.VolumeMounts {
		if mount.Name == mountName {
			if mount.SubPath != subPath {
				return fmt.Errorf("VolumeMount %s subPath expected %s, got %s", mountName, subPath, mount.SubPath)
			}
			return nil
		}
	}

	return fmt.Errorf("VolumeMount %s not found", mountName)
}

func (tc *testContext) volumeMountShouldBeReadOnly(mountName string) error {
	container, err := tc.firstContainer()
	if err != nil {
		return err
	}
	for _, mount := range container.VolumeMounts {
		if mount.Name == mountName {
			if !mount.ReadOnly {
				return fmt.Errorf("VolumeMount %s is not readOnly", mountName)
			}
			return nil
		}
	}

	return fmt.Errorf("VolumeMount %s not found", mountName)
}

func (tc *testContext) volumeShouldReferenceConfigMapByName(volumeName, configMapName string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}

	for _, vol := range tc.currentJob.Spec.Template.Spec.Volumes {
		if vol.Name == volumeName && vol.ConfigMap != nil {
			if vol.ConfigMap.Name == configMapName {
				return nil
			}
		}
	}

	return fmt.Errorf("Job %s volume %s does not reference ConfigMap %s", tc.currentJob.Name, volumeName, configMapName)
}
