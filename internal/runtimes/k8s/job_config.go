package k8s

// Contains the configuration logic that prepares the data needed by the builders
import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/eval-hub/eval-hub/pkg/api"
)

const (
	defaultCPURequest        = "250m"
	defaultMemoryRequest     = "512Mi"
	defaultCPULimit          = "1"
	defaultMemoryLimit       = "2Gi"
	defaultNamespace         = "default"
	serviceURLEnv            = "SERVICE_URL"
	evalHubInstanceNameEnv   = "EVALHUB_INSTANCE_NAME"
	inClusterNamespaceFile   = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
	serviceAccountNameSuffix = "-jobs"
	serviceCAConfigMapSuffix = "-service-ca"
	defaultEvalHubPort       = "8443"
)

type jobConfig struct {
	jobID               string
	namespace           string
	providerID          string
	benchmarkID         string
	retryAttempts       int
	adapterImage        string
	entrypoint          []string
	defaultEnv          []api.EnvVar
	cpuRequest          string
	memoryRequest       string
	cpuLimit            string
	memoryLimit         string
	jobSpecJSON         string
	serviceAccountName  string
	serviceCAConfigMap  string
	evalHubURL          string
	evalHubInstanceName string
}

type jobSpec struct {
	JobID           string              `json:"job_id"`
	BenchmarkID     string              `json:"benchmark_id"`
	Model           api.ModelRef        `json:"model"`
	NumExamples     *int                `json:"num_examples,omitempty"`
	BenchmarkConfig map[string]any      `json:"benchmark_config"`
	ExperimentName  string              `json:"experiment_name,omitempty"`
	Tags            []api.ExperimentTag `json:"tags,omitempty"`
	TimeoutSeconds  *int                `json:"timeout_seconds,omitempty"`
	RetryAttempts   *int                `json:"retry_attempts,omitempty"`
	CallbackURL     *string             `json:"callback_url"`
}

func buildJobConfig(evaluation *api.EvaluationJobResource, provider *api.ProviderResource, benchmarkID string) (*jobConfig, error) {
	runtime := provider.Runtime
	if runtime == nil || runtime.K8s == nil {
		return nil, fmt.Errorf("provider %q missing runtime configuration", provider.ProviderID)
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

	retryAttempts := 0
	if evaluation.RetryAttempts != nil {
		if *evaluation.RetryAttempts < 0 {
			return nil, fmt.Errorf("retry attempts cannot be negative")
		}
		retryAttempts = *evaluation.RetryAttempts
	}
	namespace := resolveNamespace("")
	benchmarkConfig, err := findBenchmarkConfig(evaluation, benchmarkID)
	if err != nil {
		return nil, err
	}
	benchmarkParams := copyParams(benchmarkConfig.Parameters)
	numExamples := numExamplesFromParameters(benchmarkParams)
	delete(benchmarkParams, "num_examples")
	if len(benchmarkParams) == 0 {
		return nil, fmt.Errorf("benchmark_config is required")
	}
	timeoutSeconds := timeoutSecondsFromMinutes(evaluation.TimeoutMinutes)
	spec := jobSpec{
		JobID:           evaluation.Resource.ID,
		BenchmarkID:     benchmarkID,
		Model:           evaluation.Model,
		NumExamples:     numExamples,
		BenchmarkConfig: benchmarkParams,
		TimeoutSeconds:  timeoutSeconds,
		RetryAttempts:   evaluation.RetryAttempts,
		CallbackURL:     &serviceURL,
	}
	if evaluation.Experiment != nil {
		spec.ExperimentName = evaluation.Experiment.Name
		spec.Tags = evaluation.Experiment.Tags
	}
	specJSON, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal job spec: %w", err)
	}

	// Get EvalHub instance name from environment (set by operator in deployment)
	evalHubInstanceName := strings.TrimSpace(os.Getenv(evalHubInstanceNameEnv))

	// Build ServiceAccount name, ConfigMap name, and EvalHub URL if instance name is set
	var serviceAccountName, serviceCAConfigMap, evalHubURL string
	if evalHubInstanceName != "" {
		serviceAccountName = evalHubInstanceName + serviceAccountNameSuffix
		serviceCAConfigMap = evalHubInstanceName + serviceCAConfigMapSuffix
		// EvalHub URL points to the kube-rbac-proxy HTTPS endpoint
		evalHubURL = fmt.Sprintf("https://%s.%s.svc.cluster.local:%s",
			evalHubInstanceName, namespace, defaultEvalHubPort)
	}

	return &jobConfig{
		jobID:               evaluation.Resource.ID,
		namespace:           namespace,
		providerID:          provider.ProviderID,
		benchmarkID:         benchmarkID,
		retryAttempts:       retryAttempts,
		adapterImage:        runtime.K8s.Image,
		entrypoint:          runtime.K8s.Entrypoint,
		defaultEnv:          runtime.K8s.Env,
		cpuRequest:          cpuRequest,
		memoryRequest:       memoryRequest,
		cpuLimit:            cpuLimit,
		memoryLimit:         memoryLimit,
		jobSpecJSON:         string(specJSON),
		serviceAccountName:  serviceAccountName,
		serviceCAConfigMap:  serviceCAConfigMap,
		evalHubURL:          evalHubURL,
		evalHubInstanceName: evalHubInstanceName,
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

func findBenchmarkConfig(evaluation *api.EvaluationJobResource, benchmarkID string) (*api.BenchmarkConfig, error) {
	for i := range evaluation.Benchmarks {
		benchmark := &evaluation.Benchmarks[i]
		if benchmark.ID == benchmarkID {
			return benchmark, nil
		}
	}
	return nil, fmt.Errorf("benchmark config not found for %q", benchmarkID)
}

func timeoutSecondsFromMinutes(minutes *int) *int {
	if minutes == nil {
		return nil
	}
	seconds := *minutes * 60
	return &seconds
}

func copyParams(source map[string]any) map[string]any {
	if len(source) == 0 {
		return map[string]any{}
	}
	clone := make(map[string]any, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func numExamplesFromParameters(parameters map[string]any) *int {
	if parameters == nil {
		return nil
	}
	value, ok := parameters["num_examples"]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case int:
		return &typed
	case int32:
		converted := int(typed)
		return &converted
	case int64:
		converted := int(typed)
		return &converted
	case float64:
		converted := int(typed)
		return &converted
	default:
		return nil
	}
}
