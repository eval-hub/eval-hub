package k8s

// Contains the builder functions that construct Kubernetes objects
import (
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	maxK8sNameLength       = 63
	maxK8sLabelValueLength = 63
	defaultJobTTLSeconds   = int32(3600)
	defaultJobBackoffLimit = int32(0)
	adapterContainerName   = "adapter"
	sidecarContainerName   = "sidecar"
	defaultSidecarPort     = int32(8080)
	initContainerName      = "init"
	jobSpecVolumeName      = "job-spec"
	dataVolumeName         = "data"
	// RFC 1123: volume names must be lowercase DNS labels (no camelCase).
	terminationFileVolumeName         = "termination-file-volume"
	adapterTerminationSharedMountPath = "/shared"
	testDataVolumeName                = "test-data"
	serviceCAVolumeName               = "evalhub-service-ca"
	jobSpecFileName                   = "job.json"
	jobSpecMountPath                  = "/meta/job.json"
	sidecarConfigFileName             = "sidecar_config.json"
	sidecarConfigMountPath            = "/meta/sidecar_config.json"
	dataMountPath                     = "/data"
	testDataMountPath                 = "/test_data"
	serviceCAMountPath                = "/etc/pki/ca-trust/source/anchors"
	specSuffix                        = "-spec"
	envMLFlowTrackingURIName          = "MLFLOW_TRACKING_URI"
	envMLFlowWorkspaceName            = "MLFLOW_WORKSPACE"
	mlflowTokenVolumeName             = "mlflow-token"
	mlflowTokenMountPath              = "/var/run/secrets/mlflow" // #nosec G101 -- K8s secret mount path
	mlflowTokenFile                   = "token"
	ociCredentialsVolumeName          = "oci-credentials"
	ociCredentialsMountPath           = "/etc/evalhub/.docker/config.json" // #nosec G101 -- K8s secret mount path
	ociCredentialsSubPath             = ".dockerconfigjson"                // #nosec G101 -- K8s secret subpath name
	envOCIAuthConfigPathName          = "OCI_AUTH_CONFIG_PATH"
	modelAuthVolumeName               = "model-auth" // credentials secret; mounted in sidecar only
	modelAuthMountPath                = "/var/run/secrets/model"
	// Standard Kubernetes SA mount path; used by both the sidecar SA token volume and the
	// adapter DownwardAPI namespace volume so the SDK finds files at the expected locations.
	k8sSAMountPath = "/var/run/secrets/kubernetes.io/serviceaccount"
	// evalhub SA token — projected into sidecar only; pod-level auto-mount is disabled so adapter cannot see it.
	evalhubSATokenVolumeName = "evalhub-sa-token" // #nosec G101 -- K8s projected volume name
	evalhubSATokenFile       = "token"
	// pod namespace projected into adapter via DownwardAPI so the SDK can set X-Tenant on sidecar requests.
	// The SA token auto-mount is disabled so the standard namespace file is absent; we expose it explicitly.
	adapterNamespaceVolumeName      = "pod-namespace"
	adapterNamespaceFile            = "namespace"
	modelInternalAuthVolumeName     = "model-auth-internal" // internalModelRef projected volume; mounted in adapter during credential injection
	testDataSecretVolumeName        = "test-data-secret"
	testDataSecretMountPath         = "/var/run/secrets/test-data" // #nosec G101 -- K8s secret mount path
	serviceCABundleFile             = "service-ca.crt"
	envMLFlowCertPathName           = "MLFLOW_TRACKING_SERVER_CERT_PATH"
	envEvalHubModeName              = "EVALHUB_MODE"
	envTestDataS3BucketName         = "TEST_DATA_S3_BUCKET"
	envTestDataS3KeyName            = "TEST_DATA_S3_KEY"
	defaultInitCPURequest           = "100m"
	defaultInitCPULimit             = "500m"
	defaultInitMemoryRequest        = "128Mi"
	defaultInitMemoryLimit          = "512Mi"
	defaultAllowPrivilegeEscalation = false
	//defaultRunAsUser                = int64(1000)
	//defaultRunAsGroup               = int64(1000)
	labelAppKey       = "app"
	labelComponentKey = "component"
	// EvalHub CR identity for operator consumers (e.g. failure watcher); values are DNS-label sanitized.
	labelEvalHubInstanceNameKey      = "evalhub_instance_name"
	labelEvalHubInstanceNamespaceKey = "evalhub_instance_namespace"
	labelJobIDKey                    = "job_id"
	labelProviderIDKey               = "provider_id"
	labelBenchmarkIDKey              = "benchmark_id"
	labelBenchmarkIndexKey           = "benchmark_index"
	labelAppValue                    = "evalhub"
	labelComponentValue              = "evaluation-job"
	capabilityDropAll                = "ALL"
	annotationJobIDKey               = "eval-hub.github.io/job_id"
	annotationProviderIDKey          = "eval-hub.github.io/provider_id"
	annotationBenchmarkIDKey         = "eval-hub.github.io/benchmark_id"
	labelKueueQueueNameKey           = "kueue.x-k8s.io/queue-name"
	labelKueuePriorityClassKey       = "kueue.x-k8s.io/priority-class"
	labelEvaluationPhaseKey          = "trustyai.opendatahub.io/evaluation-phase"
)

var (
	k8sResourceNameSanitizer = regexp.MustCompile(`[^a-z0-9-]+`)
	k8sLabelValueSanitizer   = regexp.MustCompile(`[^a-z0-9-_.]+`)
)

func buildConfigMap(cfg *jobConfig) (*corev1.ConfigMap, error) {
	labels := jobLabels(cfg)
	annotations := jobAnnotations(cfg.jobID, cfg.providerID, cfg.benchmarkID)
	name := configMapName(cfg.jobID, cfg.resourceGUID)

	specJSON, err := json.MarshalIndent(cfg.jobSpec, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal job spec: %w", err)
	}
	sidecarJSON := "{}"
	if cfg.sidecarConfig != nil {
		bytes, err := json.MarshalIndent(cfg.sidecarConfig, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("marshal sidecar config: %w", err)
		}
		sidecarJSON = string(bytes)
	}

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   cfg.namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Data: map[string]string{
			jobSpecFileName:       string(specJSON),
			sidecarConfigFileName: sidecarJSON,
		},
	}, nil
}

func buildJob(cfg *jobConfig) (*batchv1.Job, error) {
	if cfg.adapterImage == "" {
		return nil, fmt.Errorf("adapter image is required")
	}
	labels := jobLabels(cfg)
	annotations := jobAnnotations(cfg.jobID, cfg.providerID, cfg.benchmarkID)
	jobName := jobName(cfg.jobID, cfg.resourceGUID)
	configMap := configMapName(cfg.jobID, cfg.resourceGUID)

	ttl := defaultJobTTLSeconds
	backoff := defaultJobBackoffLimit

	adapterEnvVars := buildEnvVars(cfg)
	resources, err := buildResources(cfg)
	if err != nil {
		return nil, err
	}

	// Build runtimeContainerVolumes list
	runtimeContainerVolumes, runtimeContainerVolumeMounts := buildRuntimeContainerVolumesAndMounts(configMap, cfg)

	sidecarContainerVolumes, sidecarContainerVolumeMounts := buildSidecarContainerVolumesAndMounts(configMap, cfg)

	initContainers, InitContainsVolumes, err := initContainerVolumesAndMounts(cfg)
	if err != nil {
		return nil, err
	}

	jobVolumes := mergeVolumesByName(runtimeContainerVolumes, InitContainsVolumes, sidecarContainerVolumes)

	containers := []corev1.Container{
		{
			Name:            adapterContainerName,
			Image:           cfg.adapterImage,
			ImagePullPolicy: cfg.adapterPullPolicy,
			Command:         buildContainerCommand(cfg.entrypoint),
			Env:             adapterEnvVars,
			Resources:       resources,
			SecurityContext: defaultSecurityContext(),
			VolumeMounts:    runtimeContainerVolumeMounts,
		},
	}
	probePort := defaultSidecarPort
	if cfg.sidecarConfig != nil && cfg.sidecarConfig.Port != 0 {
		p, err := sidecarPortFromInt(cfg.sidecarConfig.Port)
		if err != nil {
			return nil, fmt.Errorf("sidecar port: %w", err)
		}
		probePort = p
	}
	// Sidecar is added as an init container with restartPolicy=Always to be
	// promoted as a native sidecar container (KEP-753).
	sidecarRestartPolicy := corev1.ContainerRestartPolicyAlways
	initContainers = append(initContainers, corev1.Container{
		Name:            sidecarContainerName,
		Image:           cfg.sidecarImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"/app/eval-runtime-sidecar"},
		Resources:       cfg.sidecarResources,
		SecurityContext: defaultSecurityContext(),
		VolumeMounts:    sidecarContainerVolumeMounts,
		RestartPolicy:   &sidecarRestartPolicy,
		// Startup probe: max startup time = failureThreshold × periodSeconds = 30 × 2 = 60s
		StartupProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/health",
					Port: intstr.FromInt32(probePort),
				},
			},
			FailureThreshold: 30,
			PeriodSeconds:    2,
		},
		TerminationMessagePath: config.SidecarTerminationFilePath,
	})

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
					RestartPolicy:                corev1.RestartPolicyNever,
					NodeSelector:                 cfg.nodeSelector,
					Tolerations:                  cfg.tolerations,
					PriorityClassName:            cfg.priorityClassName,
					InitContainers:               initContainers,
					Containers:                   containers,
					Volumes:                      jobVolumes,
					ServiceAccountName:           cfg.serviceAccountName,
					AutomountServiceAccountToken: boolPtr(false),
				},
			},
		},
	}, nil
}
