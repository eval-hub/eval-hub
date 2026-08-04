package k8s

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// mergeVolumesByName merges volume slices by name; first occurrence of each name is kept, later duplicates are skipped.
func mergeVolumesByName(slices ...[]corev1.Volume) []corev1.Volume {
	seen := make(map[string]bool)
	var out []corev1.Volume
	for _, sl := range slices {
		for _, v := range sl {
			if seen[v.Name] {
				continue
			}
			seen[v.Name] = true
			out = append(out, v)
		}
	}
	return out
}

func buildRuntimeContainerVolumesAndMounts(configMap string, cfg *jobConfig) ([]corev1.Volume, []corev1.VolumeMount) {
	volumes := []corev1.Volume{
		{
			Name: jobSpecVolumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: configMap},
				},
			},
		},
		{
			Name: dataVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
		{
			Name: terminationFileVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
		// Pod namespace exposed via DownwardAPI so the adapter SDK can read it to set the
		// X-Tenant header on sidecar requests. Pod-level SA auto-mount is disabled, so the
		// standard /var/run/secrets/kubernetes.io/serviceaccount/namespace file is absent
		// on the adapter; we project it explicitly via DownwardAPI instead.
		{
			Name: adapterNamespaceVolumeName,
			VolumeSource: corev1.VolumeSource{
				Projected: &corev1.ProjectedVolumeSource{
					Sources: []corev1.VolumeProjection{{
						DownwardAPI: &corev1.DownwardAPIProjection{
							Items: []corev1.DownwardAPIVolumeFile{{
								Path: adapterNamespaceFile,
								FieldRef: &corev1.ObjectFieldSelector{
									FieldPath: "metadata.namespace",
								},
							}},
						},
					}},
				},
			},
		},
	}

	// Build volume mounts list
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      jobSpecVolumeName,
			MountPath: jobSpecMountPath,
			SubPath:   jobSpecFileName,
			ReadOnly:  true,
		},
		{
			Name:      dataVolumeName,
			MountPath: dataMountPath,
		},
		{
			Name:      terminationFileVolumeName,
			MountPath: adapterTerminationSharedMountPath,
		},
		{
			Name:      adapterNamespaceVolumeName,
			MountPath: k8sSAMountPath,
			ReadOnly:  true,
		},
	}

	serviceCAConfigMap := cfg.serviceCAConfigMap
	// Ensure service CA volume/mount when configured.
	if serviceCAConfigMap != "" {
		volumes = ensureServiceCAVolume(volumes, serviceCAConfigMap)
		volumeMounts = ensureServiceCAMount(volumeMounts)
	}

	// Add OCI credentials volume/mount when a K8s secret connection is configured.
	if cfg.ociCredentialsSecret != "" {
		volumes = append(volumes, corev1.Volume{
			Name: ociCredentialsVolumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: cfg.ociCredentialsSecret,
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      ociCredentialsVolumeName,
			MountPath: ociCredentialsMountPath,
			SubPath:   ociCredentialsSubPath,
			ReadOnly:  true,
		})
	}

	// Add model auth volume for the adapter. The sidecar is always active when modelAuthSecretRef
	// is set (k8s_runtime.go unconditionally redirects the adapter URL to the sidecar), so there
	// is only one adapter path: a projected volume of passthrough keys (hf-token, ca_cert, both
	// optional) from the real secret. When credential injection is also active
	// (modelInternalRefSecretName set), the internalRef secret is prepended so the adapter
	// receives the synthetic ref tokens alongside the passthrough keys.
	if cfg.modelAuthSecretRef != "" {
		optionalTrue := true
		projectedSources := []corev1.VolumeProjection{
			{
				Secret: &corev1.SecretProjection{
					LocalObjectReference: corev1.LocalObjectReference{Name: cfg.modelAuthSecretRef},
					Items: []corev1.KeyToPath{
						{Key: modelHFTokenKey, Path: modelHFTokenKey},
						{Key: modelCACertKey, Path: modelCACertKey},
					},
					Optional: &optionalTrue,
				},
			},
		}
		if cfg.modelInternalRefSecretName != "" {
			projectedSources = append([]corev1.VolumeProjection{
				{
					Secret: &corev1.SecretProjection{
						LocalObjectReference: corev1.LocalObjectReference{Name: cfg.modelInternalRefSecretName},
					},
				},
			}, projectedSources...)
		}
		volumes = append(volumes, corev1.Volume{
			Name: modelInternalAuthVolumeName,
			VolumeSource: corev1.VolumeSource{
				Projected: &corev1.ProjectedVolumeSource{Sources: projectedSources},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      modelInternalAuthVolumeName,
			MountPath: modelAuthMountPath,
			ReadOnly:  true,
		})
	}

	// Adapter must mount test-data volume when S3 test data is used so it can read data downloaded by init container.
	if hasS3TestData(cfg) {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      testDataVolumeName,
			MountPath: testDataMountPath,
		})
	}

	// PVC test data: mount the PVC directly — no init container required.
	if hasPVCTestData(cfg) {
		volumes = append(volumes, corev1.Volume{
			Name: testDataVolumeName,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: cfg.testDataPVC.claimName,
					ReadOnly:  true,
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      testDataVolumeName,
			MountPath: testDataMountPath,
			SubPath:   cfg.testDataPVC.subPath,
			ReadOnly:  true,
		})
	}

	return volumes, volumeMounts
}

func buildSidecarContainerVolumesAndMounts(configMap string, cfg *jobConfig) ([]corev1.Volume, []corev1.VolumeMount) {
	volumes := []corev1.Volume{
		{
			Name: jobSpecVolumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: configMap},
				},
			},
		},
		{
			Name: dataVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
		{
			Name: terminationFileVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
	}

	// Build volume mounts list
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      jobSpecVolumeName,
			MountPath: jobSpecMountPath,
			SubPath:   jobSpecFileName,
			ReadOnly:  true,
		},
		{
			Name:      jobSpecVolumeName,
			MountPath: sidecarConfigMountPath,
			SubPath:   sidecarConfigFileName,
			ReadOnly:  true,
		},
		{
			Name:      dataVolumeName,
			MountPath: dataMountPath,
		},
		{
			Name:      terminationFileVolumeName,
			MountPath: adapterTerminationSharedMountPath,
		},
	}

	serviceCAConfigMap := cfg.serviceCAConfigMap
	// Ensure service CA volume/mount when configured.
	if serviceCAConfigMap != "" {
		volumes = ensureServiceCAVolume(volumes, serviceCAConfigMap)
		volumeMounts = ensureServiceCAMount(volumeMounts)
	}

	// Projected ServiceAccountToken for the sidecar only.
	// Pod-level auto-mount is disabled (AutomountServiceAccountToken=false on PodSpec) so that
	// the adapter cannot access the SA token. The sidecar needs it to authenticate callbacks to
	// the eval-hub API server (X-Tenant header via kube-rbac-proxy). On ROSA/STS clusters the
	// default auto-mounted token carries the wrong audience (AWS OIDC); projecting explicitly
	// gives us a token scoped to the Kubernetes API audience.
	{
		expSeconds := int64(3600)
		volumes = append(volumes, corev1.Volume{
			Name: evalhubSATokenVolumeName,
			VolumeSource: corev1.VolumeSource{
				Projected: &corev1.ProjectedVolumeSource{
					Sources: []corev1.VolumeProjection{
						{
							ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
								Path:              evalhubSATokenFile,
								ExpirationSeconds: &expSeconds,
							},
						},
					},
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      evalhubSATokenVolumeName,
			MountPath: k8sSAMountPath,
			ReadOnly:  true,
		})
	}

	// Add projected ServiceAccountToken volume for MLFlow authentication (sidecar proxies MLflow).
	// On ROSA/STS clusters, the auto-mounted SA token has the wrong audience
	// (AWS OIDC instead of Kubernetes API), so we mint a token with the default
	// audience that MLFlow's kubernetes-auth plugin can use for SelfSubjectAccessReview.
	if cfg.mlflowTrackingURI != "" {
		expSeconds := int64(3600)
		volumes = append(volumes, corev1.Volume{
			Name: mlflowTokenVolumeName,
			VolumeSource: corev1.VolumeSource{
				Projected: &corev1.ProjectedVolumeSource{
					Sources: []corev1.VolumeProjection{
						{
							ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
								Path:              mlflowTokenFile,
								ExpirationSeconds: &expSeconds,
							},
						},
					},
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      mlflowTokenVolumeName,
			MountPath: mlflowTokenMountPath,
			ReadOnly:  true,
		})
	}

	// Mount the real credentials secret in the sidecar whenever model auth is configured.
	// The sidecar needs it for ca_cert TLS and (in the credential-injection path) for
	// resolving ref tokens. The adapter never sees this volume.
	if cfg.modelAuthSecretRef != "" {
		volumes = append(volumes, corev1.Volume{
			Name: modelAuthVolumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: cfg.modelAuthSecretRef,
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      modelAuthVolumeName,
			MountPath: modelAuthMountPath,
			ReadOnly:  true,
		})
	}

	// Mount OCI credentials on the sidecar so it can proxy calls to the OCI registry.
	// The runtime container declares the same volume; mergeVolumesByName deduplicates it by name,
	// so declaring it here keeps this builder self-contained.
	if cfg.ociCredentialsSecret != "" {
		volumes = append(volumes, corev1.Volume{
			Name: ociCredentialsVolumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: cfg.ociCredentialsSecret,
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      ociCredentialsVolumeName,
			MountPath: ociCredentialsMountPath,
			SubPath:   ociCredentialsSubPath,
			ReadOnly:  true,
		})
	}

	return volumes, volumeMounts
}

func initContainerVolumesAndMounts(cfg *jobConfig) ([]corev1.Container, []corev1.Volume, error) {
	var initContainers []corev1.Container
	var volumes []corev1.Volume
	if hasS3TestData(cfg) {
		if cfg.testDataInitImage == "" {
			return nil, nil, fmt.Errorf("init image is required when S3 test data is configured")
		}
		initCommand := defaultTestDataInitCmd
		initImage := cfg.testDataInitImage
		initResources := corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(defaultInitCPURequest),
				corev1.ResourceMemory: resource.MustParse(defaultInitMemoryRequest),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(defaultInitCPULimit),
				corev1.ResourceMemory: resource.MustParse(defaultInitMemoryLimit),
			},
		}
		volumes = append(volumes, corev1.Volume{
			Name: testDataVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
		volumes = append(volumes, corev1.Volume{
			Name: testDataSecretVolumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: cfg.testDataS3.secretRef,
				},
			},
		})

		initContainers = append(initContainers, corev1.Container{
			Name:            initContainerName,
			Image:           initImage,
			ImagePullPolicy: corev1.PullIfNotPresent,
			Command:         []string{initCommand},
			Resources:       initResources,
			Env: []corev1.EnvVar{
				{Name: envTestDataS3BucketName, Value: cfg.testDataS3.bucket},
				{Name: envTestDataS3KeyName, Value: normalizeS3Key(cfg.testDataS3.key)},
			},
			SecurityContext: defaultSecurityContext(),
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      testDataVolumeName,
					MountPath: testDataMountPath,
				},
				{
					Name:      testDataSecretVolumeName,
					MountPath: testDataSecretMountPath,
					ReadOnly:  true,
				},
			},
		})
	}
	return initContainers, volumes, nil
}

func ensureServiceCAVolume(volumes []corev1.Volume, configMapName string) []corev1.Volume {
	for _, volume := range volumes {
		if volume.Name == serviceCAVolumeName {
			return volumes
		}
	}
	return append(volumes, corev1.Volume{
		Name: serviceCAVolumeName,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: configMapName},
			},
		},
	})
}

func ensureServiceCAMount(mounts []corev1.VolumeMount) []corev1.VolumeMount {
	for _, mount := range mounts {
		if mount.Name == serviceCAVolumeName {
			return mounts
		}
	}
	return append(mounts, corev1.VolumeMount{
		Name:      serviceCAVolumeName,
		MountPath: serviceCAMountPath,
		ReadOnly:  true,
	})
}

func hasS3TestData(cfg *jobConfig) bool {
	if cfg.testDataS3.secretRef == "" || cfg.testDataS3.bucket == "" {
		return false
	}
	return normalizeS3Key(cfg.testDataS3.key) != ""
}

func hasPVCTestData(cfg *jobConfig) bool {
	return cfg.testDataPVC.claimName != ""
}

func normalizeS3Key(key string) string {
	return strings.TrimPrefix(strings.TrimSpace(key), "/")
}
