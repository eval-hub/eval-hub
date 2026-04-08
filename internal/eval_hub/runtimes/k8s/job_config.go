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
	sidecarImage        string
	entrypoint          []string
	defaultEnv          []api.EnvVar
	cpuRequest          string
	memoryRequest       string
	cpuLimit            string
	memoryLimit         string
	jobSpec             shared.JobSpec
	serviceAccountName  string
	serviceCAConfigMap  string
	evalHubURL          string // in-cluster URL for sidecar to call eval-hub
	sidecarBaseURL      string // base URL for adapter/runtime to call sidecar's proxy (config.Sidecar.BaseURL)
	evalHubInstanceName string
	// evalHubCRNamespace is the namespace of the EvalHub CR (control plane); used for Job labels.
	evalHubCRNamespace   string
	mlflowTrackingURI    string
	mlflowWorkspace      string
	ociCredentialsSecret string
	modelAuthSecretRef   string
	sidecarResources     corev1.ResourceRequirements
	localMode            bool
	testDataS3           s3TestDataConfig
	testDataInitImage    string
	sidecarConfig        *config.SidecarConfig
	// queueKind and queueName come from evaluation.Queue when set (API layer normalizes empty kind to kueue).
	queueKind string
	queueName string
}

type s3TestDataConfig struct {
	bucket    string
	key       string
	secretRef string
}

func buildJobConfig(evaluation *api.EvaluationJobResource, provider *api.ProviderResource, benchmarkConfig *api.EvaluationBenchmarkConfig, benchmarkIndex int, serviceConfig *config.Config) (*jobConfig, error) {
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
	if evaluation.Model.URL == "" || evaluation.Model.Name == "" {
		return nil, fmt.Errorf("model url and name are required")
	}

	port := defaultSidecarPort
	sidecarBaseURL := ""
	if serviceConfig != nil && serviceConfig.Sidecar != nil {
		if serviceConfig.Sidecar.Port != 0 {
			port = int32(serviceConfig.Sidecar.Port)
		}
		sidecarBaseURL = strings.TrimSpace(serviceConfig.Sidecar.BaseURL)
	}
	if sidecarBaseURL == "" {
		sidecarBaseURL = fmt.Sprintf("http://localhost:%d", port)
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
			saNamespace = namespace // fallback for local mode
		}
		evalHubCRNamespace = saNamespace
		serviceAccountName = evalHubInstanceName + "-" + saNamespace + serviceAccountNameSuffix
		serviceCAConfigMap = evalHubInstanceName + serviceCAConfigMapSuffix
		// EvalHub URL points to the kube-rbac-proxy HTTPS endpoint in the instance namespace.
		// Use saNamespace (which has the local-mode fallback applied) to avoid a malformed host
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

	modelAuthSecretRef := ""
	if evaluation.Model.Auth != nil {
		modelAuthSecretRef = strings.TrimSpace(evaluation.Model.Auth.SecretRef)
	}

	sidecarImage, sidecarResources, err := sidecarImageAndResources(serviceConfig)
	if err != nil {
		return nil, err
	}

	localMode := serviceConfig != nil && serviceConfig.Service != nil && serviceConfig.Service.LocalMode
	var testDataS3Bucket, testDataS3Key, testDataS3SecretRef string
	if benchmarkConfig.TestDataRef != nil && benchmarkConfig.TestDataRef.S3 != nil {
		testDataS3Bucket = strings.TrimSpace(benchmarkConfig.TestDataRef.S3.Bucket)
		testDataS3Key = strings.TrimSpace(benchmarkConfig.TestDataRef.S3.Key)
		testDataS3SecretRef = strings.TrimSpace(benchmarkConfig.TestDataRef.S3.SecretRef)
	}

	var queueKind, queueName string
	if evaluation.Queue != nil {
		queueName = strings.TrimSpace(evaluation.Queue.Name)
		queueKind = strings.TrimSpace(evaluation.Queue.Kind)
	}

	out := &jobConfig{
		jobID:                evaluation.Resource.ID,
		resourceGUID:         uuid.NewString(),
		namespace:            namespace,
		providerID:           provider.Resource.ID,
		benchmarkID:          benchmarkConfig.ID,
		benchmarkIndex:       benchmarkIndex,
		adapterImage:         runtime.K8s.Image,
		sidecarImage:         sidecarImage,
		entrypoint:           runtime.K8s.Entrypoint,
		defaultEnv:           runtime.K8s.Env,
		cpuRequest:           cpuRequest,
		memoryRequest:        memoryRequest,
		cpuLimit:             cpuLimit,
		memoryLimit:          memoryLimit,
		jobSpec:              *spec,
		serviceAccountName:   serviceAccountName,
		serviceCAConfigMap:   serviceCAConfigMap,
		evalHubInstanceName:  evalHubInstanceName,
		evalHubCRNamespace:   evalHubCRNamespace,
		mlflowTrackingURI:    mlflowTrackingURI,
		mlflowWorkspace:      mlflowWorkspace,
		ociCredentialsSecret: ociCredentialsSecret,
		modelAuthSecretRef:   modelAuthSecretRef,
		sidecarResources:     sidecarResources,
		sidecarBaseURL:       sidecarBaseURL,
		localMode:            localMode,
		evalHubURL:           evalHubURL,
		queueKind:            queueKind,
		queueName:            queueName,
		testDataS3: s3TestDataConfig{
			bucket:    testDataS3Bucket,
			key:       testDataS3Key,
			secretRef: testDataS3SecretRef,
		},
	}
	sidecarJSON, err := sidecarForJobPod(serviceConfig, out)
	if err != nil {
		return nil, fmt.Errorf("sidecar config json: %w", err)
	}
	out.sidecarConfig = sidecarJSON
	return out, nil
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
