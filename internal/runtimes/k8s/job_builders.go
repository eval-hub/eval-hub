package k8s

// Contains the builder functions that construct Kubernetes objects
import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	maxK8sNameLength                = 63
	maxK8sLabelValueLength          = 63
	defaultJobTTLSeconds            = int32(3600)
	defaultJobBackoffLimit          = int32(3)
	adapterContainerName            = "adapter"
	jobSpecVolumeName               = "job-spec"
	dataVolumeName                  = "data"
	serviceCAVolumeName             = "evalhub-service-ca"
	jobSpecFileName                 = "job.json"
	jobSpecMountPath                = "/meta/job.json"
	dataMountPath                   = "/data"
	serviceCAMountPath              = "/etc/pki/ca-trust/source/anchors"
	specSuffix                      = "-spec"
	envJobIDName                    = "JOB_ID"
	envEvalHubURLName               = "EVALHUB_URL"
	envMLFlowTrackingURIName        = "MLFLOW_TRACKING_URI"
	envMLFlowWorkspaceName          = "MLFLOW_WORKSPACE"
	envMLFlowTokenPathName          = "MLFLOW_TRACKING_TOKEN_PATH"
	mlflowTokenVolumeName           = "mlflow-token"
	mlflowTokenMountPath            = "/var/run/secrets/mlflow"
	mlflowTokenFile                 = "token"
	ociCredentialsVolumeName        = "oci-credentials"
	ociCredentialsMountPath         = "/etc/evalhub/.docker/config.json"
	ociCredentialsSubPath           = ".dockerconfigjson"
	envOCIAuthConfigPathName        = "OCI_AUTH_CONFIG_PATH"
	modelAuthVolumeName             = "model-auth"
	modelAuthMountPath              = "/var/run/secrets/model"
	modelAuthTokenFile              = "api-key"
	modelAuthCACertFile             = "ca_cert"
	envModelAuthAPIKeyPathName      = "MODEL_AUTH_API_KEY_PATH"
	envModelAuthCACertPathName      = "MODEL_AUTH_CA_CERT_PATH"
	serviceCABundleFile             = "service-ca.crt"
	envMLFlowCertPathName           = "MLFLOW_TRACKING_SERVER_CERT_PATH"
	defaultAllowPrivilegeEscalation = false
	//defaultRunAsUser                = int64(1000)
	//defaultRunAsGroup               = int64(1000)
	labelAppKey              = "app"
	labelComponentKey        = "component"
	labelJobIDKey            = "job_id"
	labelProviderIDKey       = "provider_id"
	labelBenchmarkIDKey      = "benchmark_id"
	labelAppValue            = "evalhub"
	labelComponentValue      = "evaluation-job"
	capabilityDropAll        = "ALL"
	annotationJobIDKey       = "eval-hub.github.io/job_id"
	annotationProviderIDKey  = "eval-hub.github.io/provider_id"
	annotationBenchmarkIDKey = "eval-hub.github.io/benchmark_id"
)

var (
	k8sResourceNameSanitizer = regexp.MustCompile(`[^a-z0-9-]+`)
	k8sLabelValueSanitizer   = regexp.MustCompile(`[^a-z0-9-_.]+`)
)

func sanitizeDNS1123Label(value string) string {
	safe := strings.ToLower(value)
	safe = k8sResourceNameSanitizer.ReplaceAllString(safe, "-")
	safe = strings.Trim(safe, "-")
	if safe == "" {
		return "x"
	}
	return safe
}

func sanitizeLabelValue(value string) string {
	safe := strings.ToLower(value)
	safe = k8sLabelValueSanitizer.ReplaceAllString(safe, "-")
	if len(safe) > maxK8sLabelValueLength {
		safe = safe[:maxK8sLabelValueLength]
	}
	safe = strings.Trim(safe, "-_.")
	if safe == "" {
		return "x"
	}
	return safe
}

// buildK8sName returns a DNS-1123-safe name for Jobs and ConfigMaps:
// base = "<jobID>-<guid>", plus optional suffix (e.g. "-spec" for ConfigMaps),
// all kept within 63 chars.
func buildK8sName(jobID, resourceGUID, suffix string) string {
	safeJobID := sanitizeDNS1123Label(jobID)
	safeGUID := sanitizeDNS1123Label(resourceGUID)
	maxJobID := maxK8sNameLength - len(suffix) - len(safeGUID) - 1
	if maxJobID < 1 {
		maxJobID = 1
	}
	if len(safeJobID) > maxJobID {
		safeJobID = strings.Trim(safeJobID[:maxJobID], "-")
	}
	name := safeJobID + "-" + safeGUID + suffix
	if len(name) > maxK8sNameLength {
		name = strings.Trim(name[:maxK8sNameLength], "-")
	}
	return name
}

func buildConfigMap(cfg *jobConfig) (*corev1.ConfigMap, error) {
	labels := jobLabels(cfg.jobID, cfg.providerID, cfg.benchmarkID)
	annotations := jobAnnotations(cfg.jobID, cfg.providerID, cfg.benchmarkID)
	name := configMapName(cfg.jobID, cfg.resourceGUID)

	specJSON, err := json.MarshalIndent(cfg.jobSpec, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal job spec: %w", err)
	}
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   cfg.namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Data: map[string]string{
			jobSpecFileName: string(specJSON),
		},
	}, nil
}

func buildJob(cfg *jobConfig) (*batchv1.Job, error) {
	if cfg.adapterImage == "" {
		return nil, fmt.Errorf("adapter image is required")
	}
	labels := jobLabels(cfg.jobID, cfg.providerID, cfg.benchmarkID)
	annotations := jobAnnotations(cfg.jobID, cfg.providerID, cfg.benchmarkID)
	jobName := jobName(cfg.jobID, cfg.resourceGUID)
	configMap := configMapName(cfg.jobID, cfg.resourceGUID)

	ttl := defaultJobTTLSeconds
	backoff := defaultJobBackoffLimit

	envVars := buildEnvVars(cfg)
	resources, err := buildResources(cfg)
	if err != nil {
		return nil, err
	}

	// Build volumes list
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
	}

	serviceCAConfigMap := cfg.serviceCAConfigMap
	// Ensure service CA volume/mount when configured.
	if serviceCAConfigMap != "" {
		volumes = ensureServiceCAVolume(volumes, serviceCAConfigMap)
		volumeMounts = ensureServiceCAMount(volumeMounts)
	}

	// Add projected ServiceAccountToken volume for MLFlow authentication.
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

	// Add model auth secret when configured.
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

	// Set ServiceAccount if configured
	// applied below in template spec

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:        jobName,
			Namespace:   cfg.namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoff,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: annotations,
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:            adapterContainerName,
							Image:           cfg.adapterImage,
							ImagePullPolicy: corev1.PullAlways,
							Command:         buildContainerCommand(cfg.entrypoint),
							Env:             envVars,
							Resources:       resources,
							SecurityContext: defaultSecurityContext(),
							VolumeMounts:    volumeMounts,
						},
					},
					Volumes:            volumes,
					ServiceAccountName: cfg.serviceAccountName,
				},
			},
		},
	}, nil
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

func buildContainerCommand(entrypoint []string) []string {
	if len(entrypoint) == 0 {
		return nil
	}
	var command []string
	for _, part := range entrypoint {
		item := strings.TrimSpace(part)
		if item == "" {
			continue
		}
		command = append(command, item)
	}
	if len(command) == 0 {
		return nil
	}
	return command
}

func defaultSecurityContext() *corev1.SecurityContext {
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: boolPtr(defaultAllowPrivilegeEscalation),
		RunAsNonRoot:             boolPtr(true),
		// RunAsUser and RunAsGroup omitted to let OpenShift assign from allowed range
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{
				capabilityDropAll,
			},
		},
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
}

func boolPtr(value bool) *bool {
	return &value
}

func buildEnvVars(cfg *jobConfig) []corev1.EnvVar {
	var env []corev1.EnvVar
	seen := map[string]bool{}

	// Add JOB_ID
	env = append(env, corev1.EnvVar{
		Name:  envJobIDName,
		Value: cfg.jobID,
	})
	seen[envJobIDName] = true

	// Add EVALHUB_URL if configured
	if cfg.evalHubURL != "" {
		env = append(env, corev1.EnvVar{
			Name:  envEvalHubURLName,
			Value: cfg.evalHubURL,
		})
		seen[envEvalHubURLName] = true
	}

	// Add MLFlow environment variables if tracking is configured
	if cfg.mlflowTrackingURI != "" {
		env = append(env, corev1.EnvVar{
			Name:  envMLFlowTrackingURIName,
			Value: cfg.mlflowTrackingURI,
		})
		seen[envMLFlowTrackingURIName] = true

		// Token path points to the projected ServiceAccountToken volume
		env = append(env, corev1.EnvVar{
			Name:  envMLFlowTokenPathName,
			Value: mlflowTokenMountPath + "/" + mlflowTokenFile,
		})
		seen[envMLFlowTokenPathName] = true
	}
	if cfg.mlflowWorkspace != "" {
		env = append(env, corev1.EnvVar{
			Name:  envMLFlowWorkspaceName,
			Value: cfg.mlflowWorkspace,
		})
		seen[envMLFlowWorkspaceName] = true
	}

	// Add OCI auth config path when credentials secret is configured
	if cfg.ociCredentialsSecret != "" {
		env = append(env, corev1.EnvVar{
			Name:  envOCIAuthConfigPathName,
			Value: ociCredentialsMountPath,
		})
		seen[envOCIAuthConfigPathName] = true
	}
	if cfg.modelAuthSecretRef != "" {
		env = append(env, corev1.EnvVar{
			Name:  envModelAuthAPIKeyPathName,
			Value: modelAuthMountPath + "/" + modelAuthTokenFile,
		})
		seen[envModelAuthAPIKeyPathName] = true
		env = append(env, corev1.EnvVar{
			Name:  envModelAuthCACertPathName,
			Value: modelAuthMountPath + "/" + modelAuthCACertFile,
		})
		seen[envModelAuthCACertPathName] = true
	}

	// Set MLFLOW_TRACKING_SERVER_CERT_PATH so mlflow's tracking client
	// trusts the OpenShift service-serving CA certificate for internal calls.
	// Note: we intentionally do NOT set REQUESTS_CA_BUNDLE, because it
	// overrides the system CA bundle globally for all Python requests calls,
	// breaking external HTTPS connections (e.g. HuggingFace tokenizer downloads).
	// The adapter SDK's httpx client auto-detects the service CA independently.
	if cfg.serviceCAConfigMap != "" && cfg.mlflowTrackingURI != "" {
		env = append(env, corev1.EnvVar{
			Name:  envMLFlowCertPathName,
			Value: serviceCAMountPath + "/" + serviceCABundleFile,
		})
		seen[envMLFlowCertPathName] = true
	}

	// Add provider-specific environment variables
	for _, item := range cfg.defaultEnv {
		if item.Name == "" || seen[item.Name] {
			continue
		}
		seen[item.Name] = true
		env = append(env, corev1.EnvVar{
			Name:  item.Name,
			Value: item.Value,
		})
	}
	return env
}

func buildResources(cfg *jobConfig) (corev1.ResourceRequirements, error) {
	resources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{},
		Limits:   corev1.ResourceList{},
	}
	if cfg.cpuRequest != "" {
		quantity, err := resource.ParseQuantity(cfg.cpuRequest)
		if err != nil {
			return corev1.ResourceRequirements{}, fmt.Errorf("parse cpu request: %w", err)
		}
		resources.Requests[corev1.ResourceCPU] = quantity
	}
	if cfg.memoryRequest != "" {
		quantity, err := resource.ParseQuantity(cfg.memoryRequest)
		if err != nil {
			return corev1.ResourceRequirements{}, fmt.Errorf("parse memory request: %w", err)
		}
		resources.Requests[corev1.ResourceMemory] = quantity
	}
	if cfg.cpuLimit != "" {
		quantity, err := resource.ParseQuantity(cfg.cpuLimit)
		if err != nil {
			return corev1.ResourceRequirements{}, fmt.Errorf("parse cpu limit: %w", err)
		}
		resources.Limits[corev1.ResourceCPU] = quantity
	}
	if cfg.memoryLimit != "" {
		quantity, err := resource.ParseQuantity(cfg.memoryLimit)
		if err != nil {
			return corev1.ResourceRequirements{}, fmt.Errorf("parse memory limit: %w", err)
		}
		resources.Limits[corev1.ResourceMemory] = quantity
	}
	if len(resources.Requests) == 0 {
		resources.Requests = nil
	}
	if len(resources.Limits) == 0 {
		resources.Limits = nil
	}
	return resources, nil
}

func jobName(jobID, resourceGUID string) string {
	return buildK8sName(jobID, resourceGUID, "")
}

func configMapName(jobID, resourceGUID string) string {
	return buildK8sName(jobID, resourceGUID, specSuffix)
}

func jobLabels(jobID, providerID, benchmarkID string) map[string]string {
	return map[string]string{
		labelAppKey:         labelAppValue,
		labelComponentKey:   labelComponentValue,
		labelJobIDKey:       sanitizeLabelValue(jobID),
		labelProviderIDKey:  sanitizeLabelValue(providerID),
		labelBenchmarkIDKey: sanitizeLabelValue(benchmarkID),
	}
}

func jobAnnotations(jobID, providerID, benchmarkID string) map[string]string {
	return map[string]string{
		annotationJobIDKey:       jobID,
		annotationProviderIDKey:  providerID,
		annotationBenchmarkIDKey: benchmarkID,
	}
}
