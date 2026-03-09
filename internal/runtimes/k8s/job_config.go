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
	serviceAccountNameSuffix = "-job"
	serviceCAConfigMapSuffix = "-service-ca"
	defaultEvalHubPort       = "8443"
	defaultTestDataInitCmd   = "/app/eval-hub-init"
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
	testDataS3           s3TestDataConfig
	testDataInitImage    string
}

type s3TestDataConfig struct {
	bucket    string
	key       string
	secretRef string
}

func buildJobConfig(evaluation *api.EvaluationJobResource, provider *api.ProviderResource, benchmarkConfig *api.BenchmarkConfig, benchmarkIndex int) (*jobConfig, error) {
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

	namespace := resolveNamespace(string(evaluation.Resource.Tenant))
	spec, err := shared.BuildJobSpec(evaluation, provider.Resource.ID, benchmarkConfig, benchmarkIndex, &serviceURL)
	if err != nil {
		return nil, err
	}

	// Get EvalHub instance name from environment (set by operator in deployment)
	evalHubInstanceName := strings.TrimSpace(os.Getenv(evalHubInstanceNameEnv))

	// Get MLFlow configuration from environment (set by operator in deployment)
	mlflowTrackingURI := strings.TrimSpace(os.Getenv(mlflowTrackingURIEnv))
	mlflowWorkspace := strings.TrimSpace(os.Getenv(mlflowWorkspaceEnv))

	// Build ServiceAccount name, ConfigMap name, and EvalHub URL if instance name is set.
	// The SA name uses the instance namespace (not the tenant namespace) to match
	// the operator's naming convention: <instance>-<instance-namespace>-job.
	instanceNamespace := readInClusterNamespace()
	var serviceAccountName, serviceCAConfigMap, evalHubURL string
	if evalHubInstanceName != "" {
		saNamespace := instanceNamespace
		if saNamespace == "" {
			saNamespace = namespace // fallback for local mode
		}
		serviceAccountName = evalHubInstanceName + "-" + saNamespace + serviceAccountNameSuffix
		serviceCAConfigMap = evalHubInstanceName + serviceCAConfigMapSuffix
		// EvalHub URL points to the kube-rbac-proxy HTTPS endpoint in the instance namespace.
		// Use saNamespace (which has the local-mode fallback applied) to avoid a malformed host
		// when instanceNamespace is empty.
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

	var testDataS3Bucket, testDataS3Key, testDataS3SecretRef string
	if benchmarkConfig.TestDataRef != nil && benchmarkConfig.TestDataRef.S3 != nil {
		testDataS3Bucket = strings.TrimSpace(benchmarkConfig.TestDataRef.S3.Bucket)
		testDataS3Key = strings.TrimSpace(benchmarkConfig.TestDataRef.S3.Key)
		testDataS3SecretRef = strings.TrimSpace(benchmarkConfig.TestDataRef.S3.SecretRef)
	}

	return &jobConfig{
		jobID:                evaluation.Resource.ID,
		resourceGUID:         uuid.NewString(),
		namespace:            namespace,
		providerID:           provider.Resource.ID,
		benchmarkID:          benchmarkConfig.ID,
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
		testDataS3: s3TestDataConfig{
			bucket:    testDataS3Bucket,
			key:       testDataS3Key,
			secretRef: testDataS3SecretRef,
		},
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
