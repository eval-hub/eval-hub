package features

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/cucumber/godog"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var configPrinted bool

const k8sEnvPrefix = "env:"

// testContext holds state for real Kubernetes integration tests
type testContext struct {
	// HTTP client and server details
	client     *http.Client
	baseURL    string
	response   *http.Response
	body       []byte
	reqHeaders map[string]string
	// Request tracking
	lastRequestDuration time.Duration
	lastRequestBody     string
	lastBenchmarkIDs    []string

	// Kubernetes resources
	k8sClient             kubernetes.Interface
	namespace             string
	currentConfigMap      *corev1.ConfigMap
	currentJob            *batchv1.Job
	configMaps            []*corev1.ConfigMap
	jobs                  []*batchv1.Job
	cachedJobs            []batchv1.Job
	cachedConfigMaps      []corev1.ConfigMap
	cachedJobsJobID       string
	cachedConfigMapsJobID string

	// Tracking state from responses
	lastJobID       string
	lastBenchmarkID string
	lastProviderID  string
	createdJobIDs   []string

	// Scenario flags
}

func resolveK8sStepValue(raw string) (string, error) {
	re := regexp.MustCompile(`^\{\{([^}]*)\}\}$`)
	if m := re.FindStringSubmatch(raw); len(m) > 1 {
		raw = m[1]
	}
	if after, ok := strings.CutPrefix(raw, k8sEnvPrefix); ok {
		envName, fallback, hasFallback := strings.Cut(after, "|")
		if v, ok := os.LookupEnv(envName); ok {
			return v, nil
		}
		if hasFallback {
			return fallback, nil
		}
		return "", fmt.Errorf("environment variable %s is not set", envName)
	}
	return raw, nil
}

func newTestContext() *testContext {
	// Get SERVER_URL/SERVICE_URL from environment (no default fallback)
	serviceURL, envName := serverURLFromEnv()

	// Get namespace from environment, default to "default"
	namespace := os.Getenv("KUBERNETES_NAMESPACE")
	if namespace == "" {
		namespace = "default"
	}

	// Create HTTP client with custom transport
	// Skip TLS verification if SKIP_TLS_VERIFY is set (for self-signed certs)
	transport := &http.Transport{}
	if os.Getenv("SKIP_TLS_VERIFY") == "true" {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	tc := &testContext{
		client: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				// Don't follow redirects automatically - OAuth redirects can cause issues
				return http.ErrUseLastResponse
			},
		},
		baseURL:    serviceURL,
		namespace:  namespace,
		reqHeaders: make(map[string]string),
		configMaps: []*corev1.ConfigMap{},
		jobs:       []*batchv1.Job{},
	}

	// Initialize Kubernetes client
	if err := tc.initKubernetesClient(); err != nil {
		fmt.Printf("Warning: Failed to initialize Kubernetes client: %v\n", err)
	}

	// Print configuration on first initialization (once per test run)
	if !configPrinted {
		authToken := os.Getenv("AUTH_TOKEN")
		fmt.Printf("\n[CONFIG] Test Environment:\n")
		if serviceURL == "" {
			fmt.Printf("  SERVER_URL: ❌ NOT SET\n")
		} else {
			fmt.Printf("  %s: %s\n", envName, serviceURL)
		}
		if namespace == "" {
			fmt.Printf("  KUBERNETES_NAMESPACE: ❌ NOT SET\n")
		} else {
			fmt.Printf("  KUBERNETES_NAMESPACE: %s\n", namespace)
		}
		fmt.Printf("  AUTH_TOKEN: %s\n", func() string {
			if authToken == "" {
				return "❌ NOT SET"
			}
			if len(authToken) < 10 {
				return "⚠️  SET (but very short - might be invalid)"
			}
			return fmt.Sprintf("✅ SET (%d chars)", len(authToken))
		}())
		fmt.Printf("  SKIP_TLS_VERIFY: %s\n\n", os.Getenv("SKIP_TLS_VERIFY"))
		configPrinted = true
	}

	return tc
}

func serverURLFromEnv() (string, string) {
	if serverURL := os.Getenv("SERVER_URL"); serverURL != "" {
		return serverURL, "SERVER_URL"
	}
	return "", ""
}

// initKubernetesClient initializes the real Kubernetes client
func (tc *testContext) initKubernetesClient() error {
	var config *rest.Config
	var err error

	// Try in-cluster config first
	config, err = rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig
		kubeconfig := os.Getenv("KUBECONFIG")
		if kubeconfig == "" {
			home := os.Getenv("HOME")
			if home != "" {
				kubeconfig = filepath.Join(home, ".kube", "config")
			}
		}

		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return fmt.Errorf("failed to create Kubernetes config: %w", err)
		}
	}

	tc.k8sClient, err = kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	return nil
}

func (tc *testContext) reset() {
	tc.response = nil
	tc.body = nil
	tc.reqHeaders = make(map[string]string)
	tc.lastRequestDuration = 0
	tc.lastRequestBody = ""
	tc.lastBenchmarkIDs = nil
	tc.currentConfigMap = nil
	tc.currentJob = nil
	tc.configMaps = []*corev1.ConfigMap{}
	tc.jobs = []*batchv1.Job{}
	tc.cachedJobs = nil
	tc.cachedConfigMaps = nil
	tc.cachedJobsJobID = ""
	tc.cachedConfigMapsJobID = ""
	tc.lastJobID = ""
	tc.lastBenchmarkID = ""
	tc.lastProviderID = ""
	tc.createdJobIDs = nil
}

func (tc *testContext) applyAPIHeaders(req *http.Request) {
	for k, v := range tc.reqHeaders {
		req.Header.Set(k, v)
	}
	req.Header.Set("X-Tenant", tc.namespace)
	authToken := os.Getenv("AUTH_TOKEN")
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}
}

// cleanup removes resources created during the test
func (tc *testContext) cleanup(ctx context.Context) error {
	for _, jobID := range tc.createdJobIDs {
		if jobID == "" {
			continue
		}
		req, err := http.NewRequestWithContext(ctx, "DELETE", tc.baseURL+"/api/v1/evaluations/jobs/"+jobID+"?hard_delete=true", nil)
		if err == nil {
			tc.applyAPIHeaders(req)
			resp, reqErr := tc.client.Do(req)
			if reqErr == nil && resp != nil {
				_ = resp.Body.Close()
			}
		}
	}

	if tc.k8sClient == nil {
		return nil
	}

	return nil
}

// InitializeScenario registers all step definitions
func InitializeScenario(ctx *godog.ScenarioContext) {
	tc := newTestContext()

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		if missing := missingRequiredEnvVars(); len(missing) > 0 {
			fmt.Printf("Skipping Kubernetes scenario; missing env vars: %s\n", strings.Join(missing, ", "))
			return ctx, godog.ErrSkip
		}
		tc.reset()
		return ctx, nil
	})

	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		// Cleanup resources after each scenario
		if cleanupErr := tc.cleanup(ctx); cleanupErr != nil {
			fmt.Printf("Cleanup error: %v\n", cleanupErr)
		}
		return ctx, nil
	})

	// Background Steps
	ctx.Step(`^the service is running with Kubernetes runtime$`, tc.theServiceIsRunningWithK8sRuntime)
	ctx.Step(`^the environment variable "([^"]*)" is set to "([^"]*)"$`, tc.environmentVariableIsSet)
	ctx.Step(`^I set the header "([^"]*)" to "([^"]*)"$`, tc.iSetHeaderTo)
	ctx.Step(`^I unset the header "([^"]*)"$`, tc.iUnsetHeader)

	// HTTP Steps
	ctx.Step(`^I send a POST request to "([^"]*)" with body "([^"]*)"$`, tc.iSendPostRequestWithBody)
	ctx.Step(`^I send a (GET|DELETE|POST) request to "([^"]*)"$`, tc.iSendRequest)

	// Response Validation
	ctx.Step(`^the response code should be (\d+)$`, tc.theResponseCodeShouldBe)

	// ConfigMap Validation Steps
	ctx.Step(`^a ConfigMap should be created with name pattern "([^"]*)"$`, tc.configMapShouldBeCreatedWithNamePattern)
	ctx.Step(`^the ConfigMap should have label "([^"]*)" with value "([^"]*)"$`, tc.configMapShouldHaveLabel)
	ctx.Step(`^the ConfigMap should have label "([^"]*)" matching the evaluation job ID$`, tc.configMapShouldHaveLabelMatchingJobID)
	ctx.Step(`^the ConfigMap should have label "([^"]*)" matching the provider ID$`, tc.configMapShouldHaveLabelMatchingProviderID)
	ctx.Step(`^the ConfigMap should have label "([^"]*)" matching the benchmark ID$`, tc.configMapShouldHaveLabelMatchingBenchmarkID)
	ctx.Step(`^the ConfigMap should contain data key "([^"]*)"$`, tc.configMapShouldContainDataKey)
	ctx.Step(`^the ConfigMap data "([^"]*)" should be valid JSON$`, tc.configMapDataShouldBeValidJSON)
	ctx.Step(`^the ConfigMap data "([^"]*)" should contain field "([^"]*)" with the job ID$`, tc.configMapDataShouldContainFieldWithJobID)
	ctx.Step(`^the ConfigMap data "([^"]*)" should contain field "([^"]*)"$`, tc.configMapDataShouldContainField)
	ctx.Step(`^the ConfigMap data "([^"]*)" should contain field "([^"]*)" as object$`, tc.configMapDataShouldContainFieldAsObject)
	ctx.Step(`^the ConfigMap data "([^"]*)" should contain field "([^"]*)" with value from parameters$`, tc.configMapDataShouldContainFieldFromParams)
	ctx.Step(`^for benchmark "([^"]*)" the ConfigMap data "([^"]*)" should contain field "([^"]*)" with value from parameters$`, tc.configMapDataShouldContainFieldFromParamsForBenchmark)
	ctx.Step(`^the ConfigMap data "([^"]*)" field "([^"]*)" should not contain "([^"]*)"$`, tc.configMapDataFieldShouldNotContain)
	ctx.Step(`^for benchmark "([^"]*)" the ConfigMap data "([^"]*)" field "([^"]*)" should not contain "([^"]*)"$`, tc.configMapDataFieldShouldNotContainForBenchmark)
	ctx.Step(`^for benchmark "([^"]*)" the ConfigMap data "([^"]*)" should contain field "([^"]*)" as empty object$`, tc.configMapDataShouldContainEmptyObject)
	ctx.Step(`^the ConfigMap should have an ownerReference of kind "([^"]*)"$`, tc.configMapShouldHaveOwnerReference)
	ctx.Step(`^the ConfigMap ownerReference should have controller set to true$`, tc.configMapOwnerReferenceShouldHaveController)
	ctx.Step(`^the ConfigMap ownerReference should reference the created Job$`, tc.configMapOwnerReferenceShouldReferenceJob)
	ctx.Step(`^the ConfigMap data "([^"]*)" should contain field "([^"]*)" as array$`, tc.configMapDataShouldContainFieldAsArray)

	// Job Validation Steps
	ctx.Step(`^a Kubernetes Job should be created with name pattern "([^"]*)"$`, tc.jobShouldBeCreatedWithNamePattern)
	ctx.Step(`^the Job should have label "([^"]*)" with value "([^"]*)"$`, tc.jobShouldHaveLabel)
	ctx.Step(`^the Job should have label "([^"]*)" matching the evaluation job ID$`, tc.jobShouldHaveLabelMatchingJobID)
	ctx.Step(`^the Job should have label "([^"]*)" matching the provider ID$`, tc.jobShouldHaveLabelMatchingProviderID)
	ctx.Step(`^the Job should have label "([^"]*)" matching the benchmark ID$`, tc.jobShouldHaveLabelMatchingBenchmarkID)
	ctx.Step(`^the Job pod template should have label "([^"]*)" with value "([^"]*)"$`, tc.jobPodTemplateShouldHaveLabel)
	ctx.Step(`^the Job pod template should have label "([^"]*)" matching the evaluation job ID$`, tc.jobPodTemplateShouldHaveLabelMatchingJobID)
	ctx.Step(`^the Job pod template should have label "([^"]*)" matching the provider ID$`, tc.jobPodTemplateShouldHaveLabelMatchingProviderID)
	ctx.Step(`^the Job pod template should have label "([^"]*)" matching the benchmark ID$`, tc.jobPodTemplateShouldHaveLabelMatchingBenchmarkID)
	ctx.Step(`^the Job spec should have "([^"]*)" set to the configured retry attempts$`, tc.jobSpecShouldHaveRetryAttempts)
	ctx.Step(`^the Job spec should have "([^"]*)" set to (\d+)$`, tc.jobSpecShouldHaveValue)
	ctx.Step(`^the Job spec template should have "([^"]*)" set to "([^"]*)"$`, tc.jobTemplateSpecShouldHaveValue)
	ctx.Step(`^the Job name should be lowercase$`, tc.jobNameShouldBeLowercase)
	ctx.Step(`^the Job name should not exceed (\d+) characters$`, tc.jobNameShouldNotExceedLength)
	ctx.Step(`^the Job name should only contain alphanumeric characters and hyphens$`, tc.jobNameShouldBeAlphanumericAndHyphens)
	ctx.Step(`^the Job name should not start or end with a hyphen$`, tc.jobNameShouldNotStartOrEndWithHyphen)

	// Container Steps
	ctx.Step(`^the Job pod template should have container named "([^"]*)"$`, tc.jobPodTemplateShouldHaveContainer)
	ctx.Step(`^the container should have a non-empty image$`, tc.containerShouldHaveImage)
	ctx.Step(`^the container should have "([^"]*)" set to "([^"]*)"$`, tc.containerShouldHaveValue)
	ctx.Step(`^the container securityContext should have "([^"]*)" set to (true|false)$`, tc.containerSecurityContextShouldHaveBoolValue)
	ctx.Step(`^the container securityContext capabilities should drop "([^"]*)"$`, tc.containerSecurityContextCapabilitiesShouldDrop)
	ctx.Step(`^the container securityContext should have seccompProfile type "([^"]*)"$`, tc.containerSecurityContextSeccompProfile)
	ctx.Step(`^the container should have CPU request set$`, tc.containerShouldHaveCPURequestSet)
	ctx.Step(`^the container should have memory request set$`, tc.containerShouldHaveMemoryRequestSet)
	ctx.Step(`^the container should have CPU limit set$`, tc.containerShouldHaveCPULimitSet)
	ctx.Step(`^the container should have memory limit set$`, tc.containerShouldHaveMemoryLimitSet)
	ctx.Step(`^the Job pod template should have serviceAccountName derived from service account name$`, tc.jobPodTemplateShouldHaveServiceAccountFromSA)
	ctx.Step(`^the volume "([^"]*)" should reference ConfigMap derived from service account name$`, tc.volumeShouldReferenceConfigMapFromSA)

	// Volume & Mount Steps
	ctx.Step(`^the Job pod template should have volume "([^"]*)" of type ConfigMap$`, tc.jobPodTemplateShouldHaveConfigMapVolume)
	ctx.Step(`^the Job pod template should have volume "([^"]*)" of type EmptyDir$`, tc.jobPodTemplateShouldHaveEmptyDirVolume)
	ctx.Step(`^the volume "([^"]*)" should reference the ConfigMap with suffix "([^"]*)"$`, tc.volumeShouldReferenceConfigMap)
	ctx.Step(`^the container should have volumeMount "([^"]*)" at path "([^"]*)"$`, tc.containerShouldHaveVolumeMount)
	ctx.Step(`^the container should have volumeMount "([^"]*)" at path "([^"]*)" with subPath "([^"]*)"$`, tc.containerShouldHaveVolumeMountWithSubPath)
	ctx.Step(`^the sidecar container should have volumeMount "([^"]*)" at path "([^"]*)"$`, tc.sidecarContainerShouldHaveVolumeMount)
	ctx.Step(`^the sidecar container should have volumeMount "([^"]*)" at path "([^"]*)" with subPath "([^"]*)"$`, tc.sidecarContainerShouldHaveVolumeMountWithSubPath)
	ctx.Step(`^the volumeMount "([^"]*)" should have subPath "([^"]*)"$`, tc.volumeMountShouldHaveSubPath)
	ctx.Step(`^the volumeMount "([^"]*)" should be readOnly$`, tc.volumeMountShouldBeReadOnly)

	// Service Account & Environment
	ctx.Step(`^MLflow is configured$`, tc.mlflowIsConfigured)
	ctx.Step(`^the container command should be a valid array$`, tc.containerCommandShouldBeValidArray)
	ctx.Step(`^the container command should not contain empty strings$`, tc.containerCommandShouldNotContainEmptyStrings)
	ctx.Step(`^the container command should have trimmed whitespace from each element$`, tc.containerCommandShouldHaveTrimmedWhitespace)
	ctx.Step(`^the container should have environment variables from the provider configuration$`, tc.containerShouldHaveProviderEnvVars)

	// Deletion Steps
	ctx.Step(`^all Jobs associated with the evaluation job should be deleted$`, tc.allJobsShouldBeDeleted)
	ctx.Step(`^all ConfigMaps associated with the evaluation job should be deleted$`, tc.allConfigMapsShouldBeDeleted)
	ctx.Step(`^the Jobs should still exist in Kubernetes$`, tc.jobsShouldStillExist)
	ctx.Step(`^the ConfigMaps should still exist in Kubernetes$`, tc.configMapsShouldStillExist)

	// Stub out undefined/unimplemented steps
	ctx.Step(`^the number of Jobs created should equal the number of benchmarks$`, tc.numberOfJobsShouldEqualBenchmarks)
	ctx.Step(`^the number of ConfigMaps created should equal the number of benchmarks$`, tc.numberOfConfigMapsShouldEqualBenchmarks)
	ctx.Step(`^each Job should have a unique benchmark_id label$`, tc.eachJobShouldHaveUniqueBenchmarkIDLabel)
	ctx.Step(`^each ConfigMap should have a unique benchmark_id label$`, tc.eachConfigMapShouldHaveUniqueBenchmarkIDLabel)
	ctx.Step(`^each Job should have a unique benchmark_index label$`, tc.eachJobShouldHaveUniqueBenchmarkIndexLabel)
	ctx.Step(`^each ConfigMap should have a unique benchmark_index label$`, tc.eachConfigMapShouldHaveUniqueBenchmarkIndexLabel)
	ctx.Step(`^the response should be returned immediately without waiting for Job creation$`, tc.responseShouldBeImmediate)
	ctx.Step(`^Jobs should be created in the background$`, tc.jobsShouldBeCreatedInBackground)
	ctx.Step(`^the job has (\d+) benchmarks? configured$`, tc.jobHasBenchmarksConfigured)
	ctx.Step(`^the Job deletion should use propagationPolicy "([^"]*)"$`, tc.jobDeletionShouldUsePropagationPolicy)
	ctx.Step(`^DeleteEvaluationJobResources should be called$`, tc.deleteEvaluationJobResourcesShouldBeCalled)
	ctx.Step(`^all (\d+) Jobs should be deleted from Kubernetes$`, tc.allJobsShouldBeDeletedCount)
	ctx.Step(`^all (\d+) ConfigMaps should be deleted from Kubernetes$`, tc.allConfigMapsShouldBeDeletedCount)
}
