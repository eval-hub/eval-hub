package k8s

// Contains the builder functions that construct Kubernetes objects
import (
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
	defaultJobTTLSeconds            = int32(3600)
	adapterContainerName            = "adapter"
	jobSpecVolumeName               = "job-spec"
	dataVolumeName                  = "data"
	jobSpecFileName                 = "job.json"
	jobSpecMountPath                = "/meta/job.json"
	dataMountPath                   = "/data"
	jobPrefix                       = "eval-job-"
	specSuffix                      = "-spec"
	envJobIDName                    = "JOB_ID"
	defaultAllowPrivilegeEscalation = false
	defaultRunAsUser                = int64(1000)
	defaultRunAsGroup               = int64(1000)
	labelAppKey                     = "app"
	labelComponentKey               = "component"
	labelJobIDKey                   = "job_id"
	labelProviderIDKey              = "provider_id"
	labelBenchmarkIDKey             = "benchmark_id"
	labelAppValue                   = "evalhub"
	labelComponentValue             = "evaluation-job"
	capabilityDropAll               = "ALL"
)

var dnsLabelSanitizer = regexp.MustCompile(`[^a-z0-9-]+`)

func sanitizeDNS1123Label(value string) string {
	safe := strings.ToLower(value)
	safe = dnsLabelSanitizer.ReplaceAllString(safe, "-")
	safe = strings.Trim(safe, "-")
	if safe == "" {
		return "x"
	}
	return safe
}

func buildK8sName(jobID, benchmarkID, suffix string) string {
	base := jobPrefix + sanitizeDNS1123Label(jobID) + "-" + sanitizeDNS1123Label(benchmarkID)
	maxBase := maxK8sNameLength - len(suffix)
	if maxBase < 1 {
		maxBase = 1
	}
	if len(base) > maxBase {
		base = strings.Trim(base[:maxBase], "-")
	}
	name := base + suffix
	if len(name) > maxK8sNameLength {
		name = strings.Trim(name[:maxK8sNameLength], "-")
	}
	return name
}

func buildConfigMap(cfg *jobConfig) *corev1.ConfigMap {
	labels := jobLabels(cfg.jobID, cfg.providerID, cfg.benchmarkID)
	name := configMapName(cfg.jobID, cfg.benchmarkID)
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: cfg.namespace,
			Labels:    labels,
		},
		Data: map[string]string{
			jobSpecFileName: cfg.jobSpecJSON,
		},
	}
}

func buildJob(cfg *jobConfig) (*batchv1.Job, error) {
	if cfg.adapterImage == "" {
		return nil, fmt.Errorf("adapter image is required")
	}
	labels := jobLabels(cfg.jobID, cfg.providerID, cfg.benchmarkID)
	jobName := jobName(cfg.jobID, cfg.benchmarkID)
	configMap := configMapName(cfg.jobID, cfg.benchmarkID)

	ttl := defaultJobTTLSeconds
	backoff := int32(cfg.retryAttempts)

	envVars := buildEnvVars(cfg)
	resources, err := buildResources(cfg)
	if err != nil {
		return nil, err
	}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: cfg.namespace,
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoff,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:            adapterContainerName,
							Image:           cfg.adapterImage,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Command:         containerCommand(cfg.entrypoint),
							Env:             envVars,
							Resources:       resources,
							SecurityContext: defaultSecurityContext(),
							VolumeMounts: []corev1.VolumeMount{
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
							},
						},
					},
					Volumes: []corev1.Volume{
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
					},
				},
			},
		},
	}, nil
}

func containerCommand(entrypoint string) []string {
	if entrypoint == "" {
		return nil
	}
	return []string{entrypoint}
}

func defaultSecurityContext() *corev1.SecurityContext {
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: boolPtr(defaultAllowPrivilegeEscalation),
		RunAsNonRoot:             boolPtr(true),
		RunAsUser:                int64Ptr(defaultRunAsUser),
		RunAsGroup:               int64Ptr(defaultRunAsGroup),
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

func int64Ptr(value int64) *int64 {
	return &value
}

func buildEnvVars(cfg *jobConfig) []corev1.EnvVar {
	var env []corev1.EnvVar
	seen := map[string]bool{}
	env = append(env, corev1.EnvVar{
		Name:  envJobIDName,
		Value: cfg.jobID,
	})
	seen[envJobIDName] = true
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

func jobName(jobID, benchmarkID string) string {
	return buildK8sName(jobID, benchmarkID, "")
}

func configMapName(jobID, benchmarkID string) string {
	return buildK8sName(jobID, benchmarkID, specSuffix)
}

func jobLabels(jobID, providerID, benchmarkID string) map[string]string {
	return map[string]string{
		labelAppKey:         labelAppValue,
		labelComponentKey:   labelComponentValue,
		labelJobIDKey:       jobID,
		labelProviderIDKey:  providerID,
		labelBenchmarkIDKey: benchmarkID,
	}
}
