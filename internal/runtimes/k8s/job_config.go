package k8s

// Contains the configuration logic that prepares the data needed by the builders
import (
	"fmt"
	"os"
	"strings"

	"github.com/eval-hub/eval-hub/internal/runtimes/shared"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/google/uuid"
)

const (
	defaultCPURequest        = "250m"
	defaultMemoryRequest     = "512Mi"
	defaultCPULimit          = "1"
	defaultMemoryLimit       = "2Gi"
	defaultNamespace         = "default"
	serviceURLEnv            = "SERVICE_URL"
	evalHubInstanceNameEnv   = "EVALHUB_INSTANCE_NAME"
	mlflowTrackingURIEnv     = "MLFLOW_TRACKING_URI"
	mlflowWorkspaceEnv       = "MLFLOW_WORKSPACE"
	inClusterNamespaceFile   = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
	serviceAccountNameSuffix = "-jobs"
	serviceCAConfigMapSuffix = "-service-ca"
	defaultEvalHubPort       = "8443"
)

type jobConfig struct {
	jobID                string
	resourceGUID         string
	namespace            string
	providerID           string
	benchmarkID          string
	benchmarkIndex       int
	adapterImage         string
	entrypoint           []string
	defaultEnv           []api.EnvVar
	cpuRequest           string
	memoryRequest        string
	cpuLimit             string
	memoryLimit          string
	jobSpec              shared.JobSpec
	serviceAccountName   string
	serviceCAConfigMap   string
	evalHubURL           string
	evalHubInstanceName  string
	mlflowTrackingURI    string
	mlflowWorkspace      string
	ociCredentialsSecret string
	modelAuthSecretRef   string
}

func buildJobConfig(evaluation *api.EvaluationJobResource, provider *api.ProviderResource, benchmarkID string, benchmarkIndex int) (*jobConfig, error) {
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
	serviceURL := strings.TrimSpace(os.Getenv(serviceURLEnv))
	if serviceURL == "" {
		return nil, fmt.Errorf("%s is required", serviceURLEnv)
	}

	namespace := resolveNamespace("")
	spec, err := shared.BuildJobSpec(evaluation, provider.Resource.ID, benchmarkID, benchmarkIndex, &serviceURL)
	if err != nil {
		return nil, err
	}

	// Get EvalHub instance name from environment (set by operator in deployment)
	evalHubInstanceName := strings.TrimSpace(os.Getenv(evalHubInstanceNameEnv))

	// Get MLFlow configuration from environment (set by operator in deployment)
	mlflowTrackingURI := strings.TrimSpace(os.Getenv(mlflowTrackingURIEnv))
	mlflowWorkspace := strings.TrimSpace(os.Getenv(mlflowWorkspaceEnv))

	// Build ServiceAccount name, ConfigMap name, and EvalHub URL if instance name is set
	var serviceAccountName, serviceCAConfigMap, evalHubURL string
	if evalHubInstanceName != "" {
		serviceAccountName = evalHubInstanceName + serviceAccountNameSuffix
		serviceCAConfigMap = evalHubInstanceName + serviceCAConfigMapSuffix
		// EvalHub URL points to the kube-rbac-proxy HTTPS endpoint
		evalHubURL = fmt.Sprintf("https://%s.%s.svc.cluster.local:%s",
			evalHubInstanceName, namespace, defaultEvalHubPort)
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

	return &jobConfig{
		jobID:                evaluation.Resource.ID,
		resourceGUID:         uuid.NewString(),
		namespace:            namespace,
		providerID:           provider.Resource.ID,
		benchmarkID:          benchmarkID,
		adapterImage:         runtime.K8s.Image,
		entrypoint:           runtime.K8s.Entrypoint,
		defaultEnv:           runtime.K8s.Env,
		cpuRequest:           cpuRequest,
		memoryRequest:        memoryRequest,
		cpuLimit:             cpuLimit,
		memoryLimit:          memoryLimit,
		jobSpec:              *spec,
		serviceAccountName:   serviceAccountName,
		serviceCAConfigMap:   serviceCAConfigMap,
		evalHubURL:           evalHubURL,
		evalHubInstanceName:  evalHubInstanceName,
		mlflowTrackingURI:    mlflowTrackingURI,
		mlflowWorkspace:      mlflowWorkspace,
		ociCredentialsSecret: ociCredentialsSecret,
		modelAuthSecretRef:   modelAuthSecretRef,
	}, nil
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
