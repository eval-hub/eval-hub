package k8s

// Contains the configuration logic that prepares the data needed by the builders
import (
	"fmt"
	"os"
	"strings"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_hub/runtimes/shared"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	defaultCPURequest           = "250m"
	defaultMemoryRequest        = "512Mi"
	defaultCPULimit             = "1"
	defaultMemoryLimit          = "2Gi"
	defaultSidecarImage         = "eval-runtime-sidecar:latest"
	defaultSidecarCPURequest    = "100m"
	defaultSidecarMemoryRequest = "128Mi"
	defaultSidecarCPULimit      = "200m"
	defaultSidecarMemoryLimit   = "256Mi"
	defaultNamespace            = "default"
	evalHubInstanceNameEnv      = "EVALHUB_INSTANCE_NAME"
	mlflowTrackingURIEnv        = "MLFLOW_TRACKING_URI"
	inClusterNamespaceFile      = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
	serviceAccountNameSuffix    = "-job"
	serviceCAConfigMapSuffix    = "-service-ca"
	defaultTestDataInitCmd      = "/app/eval-runtime-init"
	defaultEvalHubPort          = "8443"
)

type jobConfig struct {
	jobID               string
	resourceGUID        string
	namespace           string
	providerID          string
	benchmarkID         string
	benchmarkIndex      int
	adapterImage        string
	adapterPullPolicy   corev1.PullPolicy
	sidecarImage        string
	entrypoint          []string
	defaultEnv          []api.EnvVar
	cpuRequest          string
	memoryRequest       string
	cpuLimit            string
	memoryLimit         string
	gpuResource         string            // Kubernetes extended resource name (e.g. "nvidia.com/gpu")
	gpuCount            int               // number of GPU units to request (0 = CPU-only)
	nodeSelector        map[string]string // pod nodeSelector; nil when a queue is set (HardwareProfile or hardware_config.queue)
	tolerations         []corev1.Toleration
	priorityClassName   string // pod PriorityClassName and/or Kueue priority-class label
	jobSpec             shared.JobSpec
	serviceAccountName  string
	serviceCAConfigMap  string
	evalHubURL          string // in-cluster URL for sidecar to call eval-hub
	sidecarBaseURL      string // base URL for adapter/runtime to call sidecar's proxy (config.Sidecar.BaseURL)
	evalHubInstanceName string
	// evalHubCRNamespace is the namespace of the EvalHub CR (control plane); used for Job labels.
	evalHubCRNamespace         string
	mlflowTrackingURI          string
	mlflowWorkspace            string
	ociCredentialsSecret       string
	modelAuthSecretRef         string // user's real credentials secret mounted only in sidecar
	modelInternalRefSecretName string // ephemeral internalModelRef secret mounted in adapter; empty when credential injection is not active
	modelTargetURL             string // real model URL forwarded by the sidecar model proxy; empty for pre-recorded-data jobs
	sidecarResources           corev1.ResourceRequirements
	testDataS3                 s3TestDataConfig
	testDataPVC                pvcTestDataConfig
	testDataGit                gitTestDataConfig
	testDataInitImage          string
	sidecarConfig              *config.SidecarConfig
	// queueKind and queueName come from a queue-backed HardwareProfile when set,
	// otherwise from effective hardware_config.queue, else deprecated evaluation.queue
	// (API layer normalizes empty kind to kueue).
	queueKind string
	queueName string
}

type s3TestDataConfig struct {
	bucket    string
	key       string
	secretRef string
}

type pvcTestDataConfig struct {
	claimName string
	subPath   string
}

type gitTestDataConfig struct {
	url       string
	ref       string
	subPath   string
	secretRef string
}

func buildJobConfig(evaluation *api.EvaluationJobResource, provider *api.ProviderResource, benchmarkConfig *api.EvaluationBenchmarkConfig, benchmarkIndex int, serviceConfig *config.Config, hardwareProfile *hardwareProfileResources) (*jobConfig, error) {
	runtime := provider.Runtime
	if runtime == nil || runtime.K8s == nil {
		return nil, fmt.Errorf("provider %q missing runtime configuration", provider.Resource.ID)
	}

	cpuRequest := defaultIfEmpty(runtime.K8s.CPURequest, defaultCPURequest)
	memoryRequest := defaultIfEmpty(runtime.K8s.MemoryRequest, defaultMemoryRequest)
	cpuLimit := defaultIfEmpty(runtime.K8s.CPULimit, defaultCPULimit)
	memoryLimit := defaultIfEmpty(runtime.K8s.MemoryLimit, defaultMemoryLimit)

	if runtime.K8s.Image == "" {
		return nil, fmt.Errorf("runtime adapter image is required")
	}
	// evaluation.Model.URL is optional (for pre-recorded datasets) and will have
	// been checked by the API layer but evaluation.Model.Name is required
	if evaluation.Model.Name == "" {
		return nil, fmt.Errorf("model name is required")
	}

	sidecarBaseURL := config.DefaultSidecarBaseURL
	if serviceConfig != nil && serviceConfig.Sidecar != nil {
		sidecarBaseURL = serviceConfig.Sidecar.EffectiveBaseURL()
	}
	namespace := resolveNamespace(string(evaluation.Resource.Tenant))
	spec, err := shared.BuildJobSpec(evaluation, provider.Resource.ID, benchmarkConfig, benchmarkIndex, &sidecarBaseURL)
	if err != nil {
		return nil, err
	}
	// Get EvalHub instance name from environment (set by operator in deployment)
	evalHubInstanceName := strings.TrimSpace(os.Getenv(evalHubInstanceNameEnv))

	// Get MLFlow configuration from environment (set by operator in deployment)
	mlflowTrackingURI := strings.TrimSpace(os.Getenv(mlflowTrackingURIEnv))
	// Job pod must send X-MLFLOW-WORKSPACE = tenant namespace so MLflow's kubernetes-auth
	// checks RBAC in the correct namespace. Always use the job's namespace; the
	// MLFLOW_WORKSPACE env var on EvalHub identifies EvalHub's own namespace,
	// not the tenant's, so it must not be forwarded to job pods.
	mlflowWorkspace := ""
	if mlflowTrackingURI != "" {
		mlflowWorkspace = namespace
	}

	// Build ServiceAccount name and ConfigMap name if instance name is set.
	// The SA name uses the instance namespace (not the tenant namespace) to match
	// the operator's naming convention: <instance>-<instance-namespace>-job.
	instanceNamespace := readInClusterNamespace()
	var serviceAccountName, serviceCAConfigMap, evalHubURL string
	var evalHubCRNamespace string
	if evalHubInstanceName != "" {
		saNamespace := instanceNamespace
		if saNamespace == "" {
			saNamespace = namespace // fallback when not running in-cluster
		}
		evalHubCRNamespace = saNamespace
		serviceAccountName = evalHubInstanceName + "-" + saNamespace + serviceAccountNameSuffix
		serviceCAConfigMap = evalHubInstanceName + serviceCAConfigMapSuffix
		// EvalHub URL points to the kube-rbac-proxy HTTPS endpoint in the instance namespace.
		// Use saNamespace (which falls back to namespace when not in-cluster) to avoid a malformed host
		// when instanceNamespace is empty.
		// This is required by sidecar to call eval-hub API.
		// This is different from job_spec.callback_url which is used by the adapter to call the sidecar
		evalHubURL = fmt.Sprintf("https://%s.%s.svc.cluster.local:%s",
			evalHubInstanceName, saNamespace, defaultEvalHubPort)
	}

	// Extract OCI credentials secret name from exports config (not forwarded to jobSpec)
	var ociCredentialsSecret string
	if evaluation.Exports != nil && evaluation.Exports.OCI != nil && evaluation.Exports.OCI.K8s != nil {
		ociCredentialsSecret = evaluation.Exports.OCI.K8s.Connection
	}

	// modelTargetURL is set when the user supplies a model URL; it remains empty for
	// pre-recorded-data jobs where no live model endpoint is needed.
	modelTargetURL := strings.TrimSpace(evaluation.Model.URL)

	// Model auth is only relevant when there is a model URL to authenticate with.
	modelAuthSecretRef := ""
	if modelTargetURL != "" && evaluation.Model.Auth != nil {
		modelAuthSecretRef = strings.TrimSpace(evaluation.Model.Auth.SecretRef)
	}

	// modelInternalRefSecretName is set in createBenchmarkResources after inspectModelSecret
	// confirms proxy-injectable keys.
	modelInternalRefSecretName := ""

	sidecarImage, sidecarResources, err := sidecarImageAndResources(serviceConfig)
	if err != nil {
		return nil, err
	}

	var testDataS3Bucket, testDataS3Key, testDataS3SecretRef string
	if benchmarkConfig.TestDataRef != nil && benchmarkConfig.TestDataRef.S3 != nil {
		testDataS3Bucket = strings.TrimSpace(benchmarkConfig.TestDataRef.S3.Bucket)
		testDataS3Key = strings.TrimSpace(benchmarkConfig.TestDataRef.S3.Key)
		testDataS3SecretRef = strings.TrimSpace(benchmarkConfig.TestDataRef.S3.SecretRef)
	}

	var testDataPVCClaimName, testDataPVCSubPath string
	if benchmarkConfig.TestDataRef != nil && benchmarkConfig.TestDataRef.PVC != nil {
		testDataPVCClaimName = strings.TrimSpace(benchmarkConfig.TestDataRef.PVC.ClaimName)
		testDataPVCSubPath = strings.TrimSpace(benchmarkConfig.TestDataRef.PVC.SubPath)
	}

	var testDataGitURL, testDataGitRef, testDataGitSubPath, testDataGitSecretRef string
	if benchmarkConfig.TestDataRef != nil && benchmarkConfig.TestDataRef.Git != nil {
		testDataGitURL = strings.TrimSpace(benchmarkConfig.TestDataRef.Git.URL)
		testDataGitRef = strings.TrimSpace(benchmarkConfig.TestDataRef.Git.Ref)
		testDataGitSubPath = strings.TrimSpace(benchmarkConfig.TestDataRef.Git.SubPath)
		testDataGitSecretRef = strings.TrimSpace(benchmarkConfig.TestDataRef.Git.SecretRef)
	}

	// GPU resource requests/limits are always propagated to the pod spec so that Kueue can
	// account for GPU quota. Provider nodeSelector is the default; a HardwareProfile with
	// Node scheduling overrides it, and a Queue-backed profile (or hardware_config.queue /
	// deprecated evaluation.queue) clears it so Kueue ResourceFlavors govern placement.
	gpuResource, gpuCount := resolveGPUConfig(runtime.K8s.GPU)
	nodeSelector := resolveNodeSelector(runtime.K8s.GPU)

	adapterPullPolicy := resolveImagePullPolicy(runtime.K8s.ImagePullPolicy)

	resourceGUID := uuid.NewString()

	out := &jobConfig{
		jobID:                      evaluation.Resource.ID,
		resourceGUID:               resourceGUID,
		namespace:                  namespace,
		providerID:                 provider.Resource.ID,
		benchmarkID:                benchmarkConfig.ID,
		benchmarkIndex:             benchmarkIndex,
		adapterImage:               runtime.K8s.Image,
		adapterPullPolicy:          adapterPullPolicy,
		sidecarImage:               sidecarImage,
		entrypoint:                 runtime.K8s.Entrypoint,
		defaultEnv:                 runtime.K8s.Env,
		cpuRequest:                 cpuRequest,
		memoryRequest:              memoryRequest,
		cpuLimit:                   cpuLimit,
		memoryLimit:                memoryLimit,
		gpuResource:                gpuResource,
		gpuCount:                   gpuCount,
		nodeSelector:               nodeSelector,
		jobSpec:                    *spec,
		serviceAccountName:         serviceAccountName,
		serviceCAConfigMap:         serviceCAConfigMap,
		evalHubInstanceName:        evalHubInstanceName,
		evalHubCRNamespace:         evalHubCRNamespace,
		mlflowTrackingURI:          mlflowTrackingURI,
		mlflowWorkspace:            mlflowWorkspace,
		ociCredentialsSecret:       ociCredentialsSecret,
		modelAuthSecretRef:         modelAuthSecretRef,
		modelInternalRefSecretName: modelInternalRefSecretName,
		modelTargetURL:             modelTargetURL,
		sidecarResources:           sidecarResources,
		sidecarBaseURL:             sidecarBaseURL,
		evalHubURL:                 evalHubURL,
		testDataS3: s3TestDataConfig{
			bucket:    testDataS3Bucket,
			key:       testDataS3Key,
			secretRef: testDataS3SecretRef,
		},
		testDataPVC: pvcTestDataConfig{
			claimName: testDataPVCClaimName,
			subPath:   testDataPVCSubPath,
		},
		testDataGit: gitTestDataConfig{
			url:       testDataGitURL,
			ref:       testDataGitRef,
			subPath:   testDataGitSubPath,
			secretRef: testDataGitSecretRef,
		},
	}
	applyHardwareProfileResources(out, hardwareProfile)
	effectiveHW := api.EffectiveHardwareConfig(benchmarkConfig, &evaluation.EvaluationJobConfig)
	if err := applyDirectHardwareConfig(out, effectiveHW); err != nil {
		return nil, err
	}
	// Deprecated evaluation.queue: only when neither hardware_config supplied a queue.
	applyQueueIfUnset(out, evaluation.Queue)
	return out, nil
}

// applyDirectHardwareConfig applies inline hardware_config fields (queue/cpu/memory/gpu)
// when no hardware_profile_name is set. Profile mode is handled separately via
// applyHardwareProfileResources.
func applyDirectHardwareConfig(cfg *jobConfig, hw *api.BenchmarkHardwareConfig) error {
	if cfg == nil || hw == nil {
		return nil
	}
	if strings.TrimSpace(hw.HardwareProfileName) != "" {
		return nil
	}
	if hw.CPU != nil {
		if request := strings.TrimSpace(hw.CPU.Request); request != "" {
			if _, err := resource.ParseQuantity(request); err != nil {
				return fmt.Errorf("hardware_config.cpu.request: %w", err)
			}
			cfg.cpuRequest = request
		}
		if limit := strings.TrimSpace(hw.CPU.Limit); limit != "" {
			if _, err := resource.ParseQuantity(limit); err != nil {
				return fmt.Errorf("hardware_config.cpu.limit: %w", err)
			}
			cfg.cpuLimit = limit
		}
	}
	if hw.Memory != nil {
		if request := strings.TrimSpace(hw.Memory.Request); request != "" {
			if _, err := resource.ParseQuantity(request); err != nil {
				return fmt.Errorf("hardware_config.memory.request: %w", err)
			}
			cfg.memoryRequest = request
		}
		if limit := strings.TrimSpace(hw.Memory.Limit); limit != "" {
			if _, err := resource.ParseQuantity(limit); err != nil {
				return fmt.Errorf("hardware_config.memory.limit: %w", err)
			}
			cfg.memoryLimit = limit
		}
	}
	if hw.GPU != nil {
		if name := strings.TrimSpace(hw.GPU.Name); name != "" {
			cfg.gpuResource = name
		}
		if hw.GPU.Count < 0 {
			return fmt.Errorf("hardware_config.gpu.count must be at least 1")
		}
		if hw.GPU.Count > 0 {
			cfg.gpuCount = hw.GPU.Count
		}
	}
	if hw.Queue != nil {
		applyQueueIfUnset(cfg, hw.Queue)
	}
	return nil
}

// applyQueueIfUnset sets queue from hardware_config.queue or deprecated evaluation.queue
// when a HardwareProfile did not already provide a LocalQueue name. Clears
// nodeSelector/tolerations so Kueue ResourceFlavors govern placement.
func applyQueueIfUnset(cfg *jobConfig, queue *api.QueueConfig) {
	if cfg == nil || queue == nil || cfg.queueName != "" {
		return
	}
	queueName := strings.TrimSpace(queue.Name)
	if queueName == "" {
		return
	}
	cfg.queueName = queueName
	cfg.queueKind = strings.TrimSpace(queue.Kind)
	if cfg.queueKind == "" {
		cfg.queueKind = "kueue"
	}
	cfg.nodeSelector = nil
	cfg.tolerations = nil
}

// sidecarImageAndResources returns image and resources for the sidecar container from
// config.Sidecar.SidecarContainer (YAML key "sidecar"), or defaults when nil/empty.
func sidecarImageAndResources(serviceConfig *config.Config) (image string, resources corev1.ResourceRequirements, err error) {
	image = defaultSidecarImage
	resources = defaultSidecarResourceRequirements()
	if serviceConfig != nil && serviceConfig.Sidecar != nil && serviceConfig.Sidecar.SidecarContainer != nil {
		sc := serviceConfig.Sidecar.SidecarContainer
		if trimmed := strings.TrimSpace(sc.Image); trimmed != "" {
			image = trimmed
		}
		if sc.Resources != nil {
			resources, err = resourceRequirementsFromConfig(sc.Resources)
			if err != nil {
				return "", corev1.ResourceRequirements{}, err
			}
		}
	}
	return image, resources, nil
}

func defaultSidecarResourceRequirements() corev1.ResourceRequirements {
	q := func(s string) resource.Quantity {
		qu, _ := resource.ParseQuantity(s)
		return qu
	}
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    q(defaultSidecarCPURequest),
			corev1.ResourceMemory: q(defaultSidecarMemoryRequest),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    q(defaultSidecarCPULimit),
			corev1.ResourceMemory: q(defaultSidecarMemoryLimit),
		},
	}
}

// resourceRequirementsFromConfig converts config.ResourceRequirements to corev1.ResourceRequirements.
// Empty strings are skipped (not set). Uses default sidecar quantities for any missing request/limit.
func resourceRequirementsFromConfig(cfg *config.ResourceRequirements) (corev1.ResourceRequirements, error) {
	out := defaultSidecarResourceRequirements()
	if cfg == nil {
		return out, nil
	}
	parse := func(s string) (resource.Quantity, error) {
		if s == "" {
			return resource.Quantity{}, nil
		}
		return resource.ParseQuantity(s)
	}
	if cfg.Requests != nil {
		if cfg.Requests.CPU != "" {
			q, err := parse(cfg.Requests.CPU)
			if err != nil {
				return corev1.ResourceRequirements{}, fmt.Errorf("sidecar resources.requests.cpu: %w", err)
			}
			out.Requests[corev1.ResourceCPU] = q
		}
		if cfg.Requests.Memory != "" {
			q, err := parse(cfg.Requests.Memory)
			if err != nil {
				return corev1.ResourceRequirements{}, fmt.Errorf("sidecar resources.requests.memory: %w", err)
			}
			out.Requests[corev1.ResourceMemory] = q
		}
	}
	if cfg.Limits != nil {
		if cfg.Limits.CPU != "" {
			q, err := parse(cfg.Limits.CPU)
			if err != nil {
				return corev1.ResourceRequirements{}, fmt.Errorf("sidecar resources.limits.cpu: %w", err)
			}
			out.Limits[corev1.ResourceCPU] = q
		}
		if cfg.Limits.Memory != "" {
			q, err := parse(cfg.Limits.Memory)
			if err != nil {
				return corev1.ResourceRequirements{}, fmt.Errorf("sidecar resources.limits.memory: %w", err)
			}
			out.Limits[corev1.ResourceMemory] = q
		}
	}
	return out, nil
}

// resolveNodeSelector returns the node selector map from a GPUConfig, or nil when absent.
func resolveNodeSelector(gpu *api.GPUConfig) map[string]string {
	if gpu == nil || len(gpu.NodeSelector) == 0 {
		return nil
	}
	out := make(map[string]string, len(gpu.NodeSelector))
	for k, v := range gpu.NodeSelector {
		out[k] = v
	}
	return out
}

// resolveGPUConfig returns the Kubernetes extended resource name and count from a GPUConfig.
// When gpu is nil or Count is 0, both return values are zero-valued (no GPU scheduled).
// When Resource is empty it is left empty — the caller (buildResources) skips the GPU
// resource request, leaving node selection to Kueue or cluster defaults.
func resolveGPUConfig(gpu *api.GPUConfig) (resource string, count int) {
	if gpu == nil || gpu.Count <= 0 {
		return "", 0
	}
	return strings.TrimSpace(gpu.Resource), gpu.Count
}

func defaultIfEmpty(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func resolveNamespace(configured string) string {
	if configured != "" {
		return configured
	}
	inClusterNamespace := readInClusterNamespace()
	if inClusterNamespace != "" {
		return inClusterNamespace
	}
	return defaultNamespace
}

func readInClusterNamespace() string {
	content, err := os.ReadFile(inClusterNamespaceFile)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(content))
}

// resolveImagePullPolicy maps the validated provider image_pull_policy to a corev1.PullPolicy.
// Empty string defaults to PullIfNotPresent.
func resolveImagePullPolicy(policy string) corev1.PullPolicy {
	switch policy {
	case "", "if_not_present":
		return corev1.PullIfNotPresent
	case "always":
		return corev1.PullAlways
	default:
		return corev1.PullIfNotPresent
	}
}
