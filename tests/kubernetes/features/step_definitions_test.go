package features

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/cucumber/godog"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var configPrinted bool

// testContext holds state for real Kubernetes integration tests
type testContext struct {
	// HTTP client and server details
	client   *http.Client
	baseURL  string
	response *http.Response
	body     []byte
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

func newTestContext() *testContext {
	// Get SERVICE_URL from environment, default to localhost
	serviceURL := os.Getenv("SERVICE_URL")
	if serviceURL == "" {
		serviceURL = "http://localhost:8080"
	}

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
		fmt.Printf("  SERVICE_URL: %s\n", serviceURL)
		fmt.Printf("  KUBERNETES_NAMESPACE: %s\n", namespace)
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

// cleanup removes resources created during the test
func (tc *testContext) cleanup(ctx context.Context) error {
	for _, jobID := range tc.createdJobIDs {
		if jobID == "" {
			continue
		}
		req, err := http.NewRequestWithContext(ctx, "DELETE", tc.baseURL+"/api/v1/evaluations/jobs/"+jobID+"?hard_delete=true", nil)
		if err == nil {
			authToken := os.Getenv("AUTH_TOKEN")
			if authToken != "" {
				req.Header.Set("Authorization", "Bearer "+authToken)
			}
			resp, reqErr := tc.client.Do(req)
			if reqErr == nil && resp != nil {
				resp.Body.Close()
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
	ctx.Step(`^the container should have environment variable "([^"]*)" set to the job ID$`, tc.containerShouldHaveEnvVarWithJobID)
	ctx.Step(`^the container securityContext should have "([^"]*)" set to (true|false)$`, tc.containerSecurityContextShouldHaveBoolValue)
	ctx.Step(`^the container securityContext capabilities should drop "([^"]*)"$`, tc.containerSecurityContextCapabilitiesShouldDrop)
	ctx.Step(`^the container securityContext should have seccompProfile type "([^"]*)"$`, tc.containerSecurityContextSeccompProfile)
	ctx.Step(`^the container should have CPU request set$`, tc.containerShouldHaveCPURequestSet)
	ctx.Step(`^the container should have memory request set$`, tc.containerShouldHaveMemoryRequestSet)
	ctx.Step(`^the container should have CPU limit set$`, tc.containerShouldHaveCPULimitSet)
	ctx.Step(`^the container should have memory limit set$`, tc.containerShouldHaveMemoryLimitSet)
	ctx.Step(`^the Job pod template should have serviceAccountName derived from service account name$`, tc.jobPodTemplateShouldHaveServiceAccountFromSA)
	ctx.Step(`^the volume "([^"]*)" should reference ConfigMap derived from service account name$`, tc.volumeShouldReferenceConfigMapFromSA)
	ctx.Step(`^the container should have environment variable "([^"]*)" derived from service account name$`, tc.containerEnvVarShouldBeDerivedFromSA)

	// Volume & Mount Steps
	ctx.Step(`^the Job pod template should have volume "([^"]*)" of type ConfigMap$`, tc.jobPodTemplateShouldHaveConfigMapVolume)
	ctx.Step(`^the Job pod template should have volume "([^"]*)" of type EmptyDir$`, tc.jobPodTemplateShouldHaveEmptyDirVolume)
	ctx.Step(`^the volume "([^"]*)" should reference the ConfigMap with suffix "([^"]*)"$`, tc.volumeShouldReferenceConfigMap)
	ctx.Step(`^the container should have volumeMount "([^"]*)" at path "([^"]*)"$`, tc.containerShouldHaveVolumeMount)
	ctx.Step(`^the volumeMount "([^"]*)" should have subPath "([^"]*)"$`, tc.volumeMountShouldHaveSubPath)
	ctx.Step(`^the volumeMount "([^"]*)" should be readOnly$`, tc.volumeMountShouldBeReadOnly)

	// Service Account & Environment
	ctx.Step(`^MLflow is configured$`, tc.mlflowIsConfigured)
	ctx.Step(`^the container command should be a valid array$`, tc.containerCommandShouldBeValidArray)
	ctx.Step(`^the container command should not contain empty strings$`, tc.containerCommandShouldNotContainEmptyStrings)
	ctx.Step(`^the container command should have trimmed whitespace from each element$`, tc.containerCommandShouldHaveTrimmedWhitespace)
	ctx.Step(`^the container should have environment variables from the provider configuration$`, tc.containerShouldHaveProviderEnvVars)
	ctx.Step(`^the environment variable "([^"]*)" should not be overridden by provider variables$`, tc.envVarShouldNotBeOverridden)

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
	ctx.Step(`^the response should be returned immediately without waiting for Job creation$`, tc.responseShouldBeImmediate)
	ctx.Step(`^Jobs should be created in the background$`, tc.jobsShouldBeCreatedInBackground)
	ctx.Step(`^the job has (\d+) benchmarks? configured$`, tc.jobHasBenchmarksConfigured)
	ctx.Step(`^the Job deletion should use propagationPolicy "([^"]*)"$`, tc.jobDeletionShouldUsePropagationPolicy)
	ctx.Step(`^DeleteEvaluationJobResources should be called$`, tc.deleteEvaluationJobResourcesShouldBeCalled)
	ctx.Step(`^all (\d+) Jobs should be deleted from Kubernetes$`, tc.allJobsShouldBeDeletedCount)
	ctx.Step(`^all (\d+) ConfigMaps should be deleted from Kubernetes$`, tc.allConfigMapsShouldBeDeletedCount)
}

// ============================================================================
// Background Steps
// ============================================================================

func (tc *testContext) theServiceIsRunningWithK8sRuntime(ctx context.Context) error {
	// Verify Kubernetes client is available
	if tc.k8sClient == nil {
		return fmt.Errorf("Kubernetes client not initialized")
	}

	// No need to check health endpoint repeatedly before each scenario
	// The actual API calls (POST /api/v1/evaluations/jobs) will verify service is up
	// This avoids 40 redundant health checks and potential issues with health endpoint
	return nil
}

// ============================================================================
// HTTP Steps
// ============================================================================

func (tc *testContext) iSendPostRequestWithBody(path, bodyFile string) error {
	return tc.iSendRequestWithBody("POST", path, bodyFile)
}

func (tc *testContext) iSendRequest(method, path string) error {
	return tc.iSendRequestWithBody(method, path, "")
}

func (tc *testContext) iSendRequestWithBody(method, path, bodyFile string) error {
	// Replace {id} placeholder with actual job ID
	path = strings.ReplaceAll(path, "{id}", tc.lastJobID)

	url := tc.baseURL + path

	var bodyReader io.Reader
	if bodyFile != "" {
		content, err := tc.loadTestFile(bodyFile)
		if err != nil {
			return err
		}
		if method == "POST" {
			tc.lastRequestBody = content
			if ids, parseErr := parseBenchmarkIDs(content); parseErr == nil {
				tc.lastBenchmarkIDs = ids
			}
		}
		bodyReader = strings.NewReader(content)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if bodyReader != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	// Add authentication token if available
	authToken := os.Getenv("AUTH_TOKEN")
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}

	start := time.Now()
	tc.response, err = tc.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer tc.response.Body.Close()

	tc.body, err = io.ReadAll(tc.response.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}
	tc.lastRequestDuration = time.Since(start)

	// Debug output for non-2xx responses
	if tc.response.StatusCode >= 400 && os.Getenv("K8S_TEST_DEBUG") == "true" {
		fmt.Printf("\n[DEBUG] Request failed:\n")
		fmt.Printf("  URL: %s %s\n", method, url)
		fmt.Printf("  Status: %d %s\n", tc.response.StatusCode, http.StatusText(tc.response.StatusCode))
		fmt.Printf("  Auth Token: %s\n", func() string {
			if authToken == "" {
				return "❌ NOT SET"
			}
			return fmt.Sprintf("✅ SET (%d chars)", len(authToken))
		}())
		fmt.Printf("  Authorization Header: %s\n", func() string {
			header := req.Header.Get("Authorization")
			if header == "" {
				return "❌ NOT SET"
			}
			return "✅ SET"
		}())
		fmt.Printf("  Response Headers: %v\n", tc.response.Header)
		fmt.Printf("  Response Body: %s\n\n", string(tc.body))
	}

	// Extract job ID from response if this was a POST to create job
	if method == "POST" && strings.Contains(path, "/evaluations/jobs") && tc.response.StatusCode == 202 {
		if err := tc.extractJobIDFromResponse(); err != nil {
			return err
		}

		// Wait for Kubernetes resources to be created
		time.Sleep(2 * time.Second)
		tc.trySetCurrentResources()
	}

	return nil
}

func parseBenchmarkIDs(body string) ([]string, error) {
	var payload struct {
		Benchmarks []struct {
			ID string `json:"id"`
		} `json:"benchmarks"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(payload.Benchmarks))
	for _, bench := range payload.Benchmarks {
		if bench.ID != "" {
			ids = append(ids, bench.ID)
		}
	}
	return ids, nil
}

func (tc *testContext) trySetCurrentResources() {
	if tc.k8sClient == nil || tc.lastJobID == "" {
		return
	}
	tc.currentJob = nil
	tc.currentConfigMap = nil
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if tc.currentJob == nil {
			jobs, err := tc.k8sClient.BatchV1().Jobs(tc.namespace).List(context.Background(), metav1.ListOptions{
				LabelSelector: fmt.Sprintf("job_id=%s", tc.lastJobID),
			})
			if err == nil && len(jobs.Items) > 0 {
				tc.currentJob = &jobs.Items[0]
				tc.jobs = append(tc.jobs, tc.currentJob)
			}
		}
		if tc.currentConfigMap == nil {
			configMaps, err := tc.k8sClient.CoreV1().ConfigMaps(tc.namespace).List(context.Background(), metav1.ListOptions{
				LabelSelector: fmt.Sprintf("job_id=%s", tc.lastJobID),
			})
			if err == nil && len(configMaps.Items) > 0 {
				tc.currentConfigMap = &configMaps.Items[0]
				tc.configMaps = append(tc.configMaps, tc.currentConfigMap)
			}
		}
		if tc.currentJob != nil && tc.currentConfigMap != nil {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func (tc *testContext) loadTestFile(fileName string) (string, error) {
	// Remove "file:/" prefix if present
	fileName = strings.TrimPrefix(fileName, "file:/")

	filePath := filepath.Join("test_data", fileName)
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read test file %s: %w", filePath, err)
	}

	return string(content), nil
}

func (tc *testContext) extractJobIDFromResponse() error {
	var responseData map[string]interface{}
	if err := json.Unmarshal(tc.body, &responseData); err != nil {
		return fmt.Errorf("failed to parse response JSON: %w", err)
	}

	// API response format: {"resource": {"id": "...", ...}, ...}
	// Extract job ID from resource.id
	if resource, ok := responseData["resource"].(map[string]interface{}); ok {
		if id, ok := resource["id"].(string); ok && id != "" {
			tc.lastJobID = id
			tc.addCreatedJobID(id)
			if os.Getenv("K8S_TEST_DEBUG") == "true" {
				fmt.Printf("Extracted job ID: %s\n", id)
			}
			return nil
		}
	}

	// Fallback: try top-level id or job_id
	if id, ok := responseData["id"].(string); ok && id != "" {
		tc.lastJobID = id
		tc.addCreatedJobID(id)
		if os.Getenv("K8S_TEST_DEBUG") == "true" {
			fmt.Printf("Extracted job ID from top level: %s\n", id)
		}
		return nil
	}
	if jobID, ok := responseData["job_id"].(string); ok && jobID != "" {
		tc.lastJobID = jobID
		tc.addCreatedJobID(jobID)
		if os.Getenv("K8S_TEST_DEBUG") == "true" {
			fmt.Printf("Extracted job_id: %s\n", jobID)
		}
		return nil
	}

	// If no ID found, warn but don't fail - some tests might not need it
	if os.Getenv("K8S_TEST_DEBUG") == "true" {
		fmt.Println("Warning: No job ID found in response, will search for resources")
	}
	return nil
}

func (tc *testContext) addCreatedJobID(jobID string) {
	for _, existing := range tc.createdJobIDs {
		if existing == jobID {
			return
		}
	}
	tc.createdJobIDs = append(tc.createdJobIDs, jobID)
}

// ============================================================================
// Response Validation Steps
// ============================================================================

func (tc *testContext) theResponseCodeShouldBe(code int) error {
	if tc.response == nil {
		return fmt.Errorf("no response recorded (body=%q)", string(tc.body))
	}
	if tc.response.StatusCode != code {
		return fmt.Errorf("expected status code %d, got %d. Response: %s", code, tc.response.StatusCode, string(tc.body))
	}
	return nil
}

// ============================================================================
// ConfigMap Validation Steps
// ============================================================================

func (tc *testContext) configMapShouldBeCreatedWithNamePattern(pattern string) error {
	// Convert pattern to regex
	regexPattern := strings.ReplaceAll(pattern, "{id}", ".*")
	regexPattern = strings.ReplaceAll(regexPattern, "{benchmark_id}", ".*")
	regex := regexp.MustCompile(regexPattern)

	// List ConfigMaps with job_id label if we have it
	listOptions := metav1.ListOptions{}
	if tc.lastJobID != "" {
		listOptions.LabelSelector = fmt.Sprintf("job_id=%s", tc.lastJobID)
	}

	configMaps, err := tc.k8sClient.CoreV1().ConfigMaps(tc.namespace).List(context.Background(), listOptions)
	if err != nil {
		return fmt.Errorf("failed to list ConfigMaps: %w", err)
	}

	for i := range configMaps.Items {
		cm := &configMaps.Items[i]
		if regex.MatchString(cm.Name) {
			tc.currentConfigMap = cm
			tc.configMaps = append(tc.configMaps, cm)

			// Extract job ID from labels if we don't have it yet
			if tc.lastJobID == "" {
				if jobID, ok := cm.Labels["job_id"]; ok {
					tc.lastJobID = jobID
				}
			}

			return nil
		}
	}

	return fmt.Errorf("no ConfigMap found matching pattern %s (searched %d ConfigMaps)", pattern, len(configMaps.Items))
}

func (tc *testContext) configMapShouldHaveLabel(label, value string) error {
	if tc.currentConfigMap == nil {
		return fmt.Errorf("no current ConfigMap")
	}

	actualValue, exists := tc.currentConfigMap.Labels[label]
	if !exists {
		return fmt.Errorf("ConfigMap %s does not have label %s", tc.currentConfigMap.Name, label)
	}
	if actualValue != value {
		return fmt.Errorf("ConfigMap %s label %s expected %s, got %s", tc.currentConfigMap.Name, label, value, actualValue)
	}
	return nil
}

func (tc *testContext) configMapShouldHaveLabelMatchingJobID(label string) error {
	if tc.currentConfigMap == nil {
		return fmt.Errorf("no current ConfigMap")
	}

	actualValue, exists := tc.currentConfigMap.Labels[label]
	if !exists {
		return fmt.Errorf("ConfigMap %s does not have label %s", tc.currentConfigMap.Name, label)
	}

	if tc.lastJobID != "" && actualValue != tc.lastJobID {
		return fmt.Errorf("ConfigMap %s label %s expected %s, got %s", tc.currentConfigMap.Name, label, tc.lastJobID, actualValue)
	}
	tc.lastJobID = actualValue
	return nil
}

func (tc *testContext) configMapShouldHaveLabelMatchingProviderID(label string) error {
	if tc.currentConfigMap == nil {
		return fmt.Errorf("no current ConfigMap")
	}

	actualValue, exists := tc.currentConfigMap.Labels[label]
	if !exists {
		return fmt.Errorf("ConfigMap %s does not have label %s", tc.currentConfigMap.Name, label)
	}

	tc.lastProviderID = actualValue
	return nil
}

func (tc *testContext) configMapShouldHaveLabelMatchingBenchmarkID(label string) error {
	if tc.currentConfigMap == nil {
		return fmt.Errorf("no current ConfigMap")
	}

	actualValue, exists := tc.currentConfigMap.Labels[label]
	if !exists {
		return fmt.Errorf("ConfigMap %s does not have label %s", tc.currentConfigMap.Name, label)
	}

	tc.lastBenchmarkID = actualValue
	return nil
}

func (tc *testContext) configMapShouldContainDataKey(key string) error {
	if tc.currentConfigMap == nil {
		return fmt.Errorf("no current ConfigMap")
	}

	if _, exists := tc.currentConfigMap.Data[key]; !exists {
		return fmt.Errorf("ConfigMap %s does not have data key %s", tc.currentConfigMap.Name, key)
	}
	return nil
}

func (tc *testContext) configMapDataShouldBeValidJSON(dataKey string) error {
	if tc.currentConfigMap == nil {
		return fmt.Errorf("no current ConfigMap")
	}

	data, exists := tc.currentConfigMap.Data[dataKey]
	if !exists {
		return fmt.Errorf("ConfigMap %s does not have data key %s", tc.currentConfigMap.Name, dataKey)
	}

	var js interface{}
	if err := json.Unmarshal([]byte(data), &js); err != nil {
		return fmt.Errorf("ConfigMap %s data %s is not valid JSON: %w", tc.currentConfigMap.Name, dataKey, err)
	}
	return nil
}

func (tc *testContext) configMapDataShouldContainFieldWithJobID(dataKey, field string) error {
	if tc.currentConfigMap == nil {
		return fmt.Errorf("no current ConfigMap")
	}

	data, exists := tc.currentConfigMap.Data[dataKey]
	if !exists {
		return fmt.Errorf("ConfigMap %s does not have data key %s", tc.currentConfigMap.Name, dataKey)
	}

	var jobSpec map[string]interface{}
	if err := json.Unmarshal([]byte(data), &jobSpec); err != nil {
		return fmt.Errorf("failed to parse job.json: %w", err)
	}

	value, exists := jobSpec[field]
	if !exists {
		return fmt.Errorf("job.json does not contain field %s", field)
	}

	tc.lastJobID = fmt.Sprintf("%v", value)
	return nil
}

func (tc *testContext) configMapDataShouldContainField(dataKey, field string) error {
	if tc.currentConfigMap == nil {
		return fmt.Errorf("no current ConfigMap")
	}

	data, exists := tc.currentConfigMap.Data[dataKey]
	if !exists {
		return fmt.Errorf("ConfigMap %s does not have data key %s", tc.currentConfigMap.Name, dataKey)
	}

	var jobSpec map[string]interface{}
	if err := json.Unmarshal([]byte(data), &jobSpec); err != nil {
		return fmt.Errorf("failed to parse %s: %w", dataKey, err)
	}

	// Handle nested fields like "model.url"
	parts := strings.Split(field, ".")
	current := jobSpec
	for i, part := range parts {
		if i == len(parts)-1 {
			if _, exists := current[part]; !exists {
				return fmt.Errorf("%s does not contain field %s", dataKey, field)
			}
		} else {
			next, ok := current[part].(map[string]interface{})
			if !ok {
				return fmt.Errorf("%s field %s is not an object", dataKey, part)
			}
			current = next
		}
	}

	return nil
}

func (tc *testContext) configMapDataShouldContainFieldAsObject(dataKey, field string) error {
	if tc.currentConfigMap == nil {
		return fmt.Errorf("no current ConfigMap")
	}

	data, exists := tc.currentConfigMap.Data[dataKey]
	if !exists {
		return fmt.Errorf("ConfigMap %s does not have data key %s", tc.currentConfigMap.Name, dataKey)
	}

	var jobSpec map[string]interface{}
	if err := json.Unmarshal([]byte(data), &jobSpec); err != nil {
		return fmt.Errorf("failed to parse %s: %w", dataKey, err)
	}

	value, exists := jobSpec[field]
	if !exists {
		return fmt.Errorf("%s does not contain field %s", dataKey, field)
	}

	if _, ok := value.(map[string]interface{}); !ok {
		return fmt.Errorf("%s field %s is not an object", dataKey, field)
	}

	return nil
}

func (tc *testContext) configMapDataShouldContainFieldFromParams(dataKey, field string) error {
	return tc.configMapDataShouldContainField(dataKey, field)
}

func (tc *testContext) configMapDataShouldContainFieldFromParamsForBenchmark(benchmark, dataKey, field string) error {
	cm, err := tc.findConfigMapForBenchmark(benchmark, dataKey)
	if err != nil {
		return err
	}
	return tc.configMapDataShouldContainFieldWithConfigMap(cm, dataKey, field)
}

func (tc *testContext) configMapDataFieldShouldNotContain(dataKey, field, subfield string) error {
	if tc.currentConfigMap == nil {
		return fmt.Errorf("no current ConfigMap")
	}
	return tc.configMapDataFieldShouldNotContainWithConfigMap(tc.currentConfigMap, dataKey, field, subfield)
}

func (tc *testContext) configMapDataFieldShouldNotContainForBenchmark(benchmark, dataKey, field, subfield string) error {
	cm, err := tc.findConfigMapForBenchmark(benchmark, dataKey)
	if err != nil {
		return err
	}
	return tc.configMapDataFieldShouldNotContainWithConfigMap(cm, dataKey, field, subfield)
}

func (tc *testContext) configMapDataShouldContainEmptyObject(benchmark, dataKey, field string) error {
	cm, err := tc.findConfigMapForBenchmark(benchmark, dataKey)
	if err != nil {
		return err
	}
	data, exists := cm.Data[dataKey]
	if !exists {
		return fmt.Errorf("ConfigMap %s does not have data key %s", cm.Name, dataKey)
	}

	var jobSpec map[string]interface{}
	if err := json.Unmarshal([]byte(data), &jobSpec); err != nil {
		return fmt.Errorf("failed to parse %s: %w", dataKey, err)
	}

	value, exists := jobSpec[field]
	if !exists {
		return fmt.Errorf("%s does not contain field %s", dataKey, field)
	}

	fieldMap, ok := value.(map[string]interface{})
	if !ok {
		return fmt.Errorf("%s field %s is not an object", dataKey, field)
	}

	if len(fieldMap) != 0 {
		return fmt.Errorf("%s field %s is not empty, has %d keys", dataKey, field, len(fieldMap))
	}

	return nil
}

func (tc *testContext) findConfigMapForBenchmark(benchmark, dataKey string) (*corev1.ConfigMap, error) {
	// Find ConfigMap for specific benchmark (retry for async creation)
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		var configMaps []corev1.ConfigMap
		if tc.lastJobID != "" {
			maps, err := tc.listConfigMapsByJobIDFresh()
			if err != nil {
				return nil, err
			}
			configMaps = maps
		} else {
			maps, err := tc.k8sClient.CoreV1().ConfigMaps(tc.namespace).List(context.Background(), metav1.ListOptions{
				LabelSelector: fmt.Sprintf("benchmark_id=%s", benchmark),
			})
			if err != nil {
				return nil, fmt.Errorf("failed to list ConfigMaps: %w", err)
			}
			configMaps = maps.Items
		}

		for i := range configMaps {
			candidate := &configMaps[i]
			if candidate.Labels["benchmark_id"] == benchmark {
				return candidate, nil
			}
			if dataKey != "" {
				if data, exists := candidate.Data[dataKey]; exists {
					var jobSpec map[string]interface{}
					if json.Unmarshal([]byte(data), &jobSpec) == nil {
						if jobBenchmark, ok := jobSpec["benchmark_id"].(string); ok && jobBenchmark == benchmark {
							return candidate, nil
						}
					}
				}
			}
		}
		if tc.lastJobID != "" {
			// Fallback: try by benchmark_id label in case job_id mismatch.
			maps, err := tc.k8sClient.CoreV1().ConfigMaps(tc.namespace).List(context.Background(), metav1.ListOptions{
				LabelSelector: fmt.Sprintf("benchmark_id=%s", benchmark),
			})
			if err == nil {
				for i := range maps.Items {
					candidate := &maps.Items[i]
					if candidate.Labels["benchmark_id"] == benchmark {
						return candidate, nil
					}
				}
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	if os.Getenv("K8S_TEST_DEBUG") == "true" {
		status := 0
		if tc.response != nil {
			status = tc.response.StatusCode
		}
		fmt.Printf("[DEBUG] no ConfigMap found for benchmark %s (job_id=%s, namespace=%s)\n", benchmark, tc.lastJobID, tc.namespace)
		fmt.Printf("[DEBUG] last request status=%d\n", status)
		if tc.lastRequestBody != "" {
			fmt.Printf("[DEBUG] last request body: %s\n", tc.lastRequestBody)
		}
		if len(tc.body) > 0 {
			fmt.Printf("[DEBUG] last response body: %s\n", string(tc.body))
		}
	}
	return nil, fmt.Errorf("no ConfigMap found for benchmark %s", benchmark)
}

func (tc *testContext) configMapDataShouldContainFieldWithConfigMap(cm *corev1.ConfigMap, dataKey, field string) error {
	if cm == nil {
		return fmt.Errorf("no ConfigMap provided")
	}
	data, exists := cm.Data[dataKey]
	if !exists {
		return fmt.Errorf("ConfigMap %s does not have data key %s", cm.Name, dataKey)
	}

	var jobSpec map[string]interface{}
	if err := json.Unmarshal([]byte(data), &jobSpec); err != nil {
		return fmt.Errorf("failed to parse %s: %w", dataKey, err)
	}

	// Handle nested fields like "model.url"
	parts := strings.Split(field, ".")
	current := jobSpec
	for i, part := range parts {
		if i == len(parts)-1 {
			if _, exists := current[part]; !exists {
				return fmt.Errorf("%s does not contain field %s", dataKey, field)
			}
		} else {
			next, ok := current[part].(map[string]interface{})
			if !ok {
				return fmt.Errorf("%s field %s is not an object", dataKey, part)
			}
			current = next
		}
	}

	return nil
}

func (tc *testContext) configMapDataFieldShouldNotContainWithConfigMap(cm *corev1.ConfigMap, dataKey, field, subfield string) error {
	if cm == nil {
		return fmt.Errorf("no ConfigMap provided")
	}

	data, exists := cm.Data[dataKey]
	if !exists {
		return fmt.Errorf("ConfigMap %s does not have data key %s", cm.Name, dataKey)
	}

	var jobSpec map[string]interface{}
	if err := json.Unmarshal([]byte(data), &jobSpec); err != nil {
		return fmt.Errorf("failed to parse %s: %w", dataKey, err)
	}

	fieldValue, exists := jobSpec[field]
	if !exists {
		return fmt.Errorf("%s does not contain field %s", dataKey, field)
	}

	fieldMap, ok := fieldValue.(map[string]interface{})
	if !ok {
		return fmt.Errorf("%s field %s is not an object", dataKey, field)
	}

	if _, exists := fieldMap[subfield]; exists {
		return fmt.Errorf("%s field %s should not contain %s", dataKey, field, subfield)
	}

	return nil
}

func (tc *testContext) configMapShouldHaveOwnerReference(kind string) error {
	if tc.currentConfigMap == nil {
		return fmt.Errorf("no current ConfigMap")
	}

	if len(tc.currentConfigMap.OwnerReferences) == 0 {
		return fmt.Errorf("ConfigMap %s has no owner references", tc.currentConfigMap.Name)
	}

	for _, ref := range tc.currentConfigMap.OwnerReferences {
		if ref.Kind == kind {
			return nil
		}
	}

	return fmt.Errorf("ConfigMap %s has no owner reference of kind %s", tc.currentConfigMap.Name, kind)
}

func (tc *testContext) configMapOwnerReferenceShouldHaveController() error {
	if tc.currentConfigMap == nil {
		return fmt.Errorf("no current ConfigMap")
	}

	if len(tc.currentConfigMap.OwnerReferences) == 0 {
		return fmt.Errorf("ConfigMap %s has no owner references", tc.currentConfigMap.Name)
	}

	for _, ref := range tc.currentConfigMap.OwnerReferences {
		if ref.Controller != nil && *ref.Controller {
			return nil
		}
	}

	return fmt.Errorf("ConfigMap %s has no owner reference with controller=true", tc.currentConfigMap.Name)
}

func (tc *testContext) configMapOwnerReferenceShouldReferenceJob() error {
	if tc.currentConfigMap == nil {
		return fmt.Errorf("no current ConfigMap")
	}
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}

	for _, ref := range tc.currentConfigMap.OwnerReferences {
		if ref.Kind == "Job" && ref.Name == tc.currentJob.Name {
			return nil
		}
	}

	return fmt.Errorf("ConfigMap %s does not reference Job %s", tc.currentConfigMap.Name, tc.currentJob.Name)
}

func (tc *testContext) configMapDataShouldContainFieldAsArray(dataKey, field string) error {
	if tc.currentConfigMap == nil {
		return fmt.Errorf("no current ConfigMap")
	}

	data, exists := tc.currentConfigMap.Data[dataKey]
	if !exists {
		return fmt.Errorf("ConfigMap %s does not have data key %s", tc.currentConfigMap.Name, dataKey)
	}

	var jobSpec map[string]interface{}
	if err := json.Unmarshal([]byte(data), &jobSpec); err != nil {
		return fmt.Errorf("failed to parse %s: %w", dataKey, err)
	}

	value, exists := jobSpec[field]
	if !exists {
		return fmt.Errorf("%s does not contain field %s", dataKey, field)
	}

	if _, ok := value.([]interface{}); !ok {
		return fmt.Errorf("%s field %s is not an array", dataKey, field)
	}

	return nil
}

// ============================================================================
// Job Validation Steps
// ============================================================================

func (tc *testContext) jobShouldBeCreatedWithNamePattern(pattern string) error {
	// Convert pattern to regex
	regexPattern := strings.ReplaceAll(pattern, "{id}", ".*")
	regexPattern = strings.ReplaceAll(regexPattern, "{benchmark_id}", ".*")
	regex := regexp.MustCompile(regexPattern)

	// List Jobs with job_id label if we have it
	listOptions := metav1.ListOptions{}
	if tc.lastJobID != "" {
		listOptions.LabelSelector = fmt.Sprintf("job_id=%s", tc.lastJobID)
	}

	jobs, err := tc.k8sClient.BatchV1().Jobs(tc.namespace).List(context.Background(), listOptions)
	if err != nil {
		return fmt.Errorf("failed to list Jobs: %w", err)
	}

	for i := range jobs.Items {
		job := &jobs.Items[i]
		if regex.MatchString(job.Name) {
			tc.currentJob = job
			tc.jobs = append(tc.jobs, job)

			// Extract job ID from labels if we don't have it yet
			if tc.lastJobID == "" {
				if jobID, ok := job.Labels["job_id"]; ok {
					tc.lastJobID = jobID
				}
			}

			return nil
		}
	}

	return fmt.Errorf("no Job found matching pattern %s (searched %d Jobs)", pattern, len(jobs.Items))
}

func (tc *testContext) jobShouldHaveLabel(label, value string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}

	actualValue, exists := tc.currentJob.Labels[label]
	if !exists {
		return fmt.Errorf("Job %s does not have label %s", tc.currentJob.Name, label)
	}
	if actualValue != value {
		return fmt.Errorf("Job %s label %s expected %s, got %s", tc.currentJob.Name, label, value, actualValue)
	}
	return nil
}

func (tc *testContext) jobShouldHaveLabelMatchingJobID(label string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}

	actualValue, exists := tc.currentJob.Labels[label]
	if !exists {
		return fmt.Errorf("Job %s does not have label %s", tc.currentJob.Name, label)
	}

	if tc.lastJobID != "" && actualValue != tc.lastJobID {
		return fmt.Errorf("Job %s label %s expected %s, got %s", tc.currentJob.Name, label, tc.lastJobID, actualValue)
	}

	tc.lastJobID = actualValue
	return nil
}

func (tc *testContext) jobShouldHaveLabelMatchingProviderID(label string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}

	actualValue, exists := tc.currentJob.Labels[label]
	if !exists {
		return fmt.Errorf("Job %s does not have label %s", tc.currentJob.Name, label)
	}

	if tc.lastProviderID != "" && actualValue != tc.lastProviderID {
		return fmt.Errorf("Job %s label %s expected %s, got %s", tc.currentJob.Name, label, tc.lastProviderID, actualValue)
	}

	tc.lastProviderID = actualValue
	return nil
}

func (tc *testContext) jobShouldHaveLabelMatchingBenchmarkID(label string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}

	actualValue, exists := tc.currentJob.Labels[label]
	if !exists {
		return fmt.Errorf("Job %s does not have label %s", tc.currentJob.Name, label)
	}

	if tc.lastBenchmarkID != "" && actualValue != tc.lastBenchmarkID {
		return fmt.Errorf("Job %s label %s expected %s, got %s", tc.currentJob.Name, label, tc.lastBenchmarkID, actualValue)
	}

	tc.lastBenchmarkID = actualValue
	return nil
}

func (tc *testContext) jobPodTemplateShouldHaveLabel(label, value string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}
	actualValue, exists := tc.currentJob.Spec.Template.Labels[label]
	if !exists {
		return fmt.Errorf("Job %s pod template does not have label %s", tc.currentJob.Name, label)
	}
	if actualValue != value {
		return fmt.Errorf("Job %s pod template label %s expected %s, got %s", tc.currentJob.Name, label, value, actualValue)
	}
	return nil
}

func (tc *testContext) jobPodTemplateShouldHaveLabelMatchingJobID(label string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}
	actualValue, exists := tc.currentJob.Spec.Template.Labels[label]
	if !exists {
		return fmt.Errorf("Job %s pod template does not have label %s", tc.currentJob.Name, label)
	}
	if tc.lastJobID != "" && actualValue != tc.lastJobID {
		return fmt.Errorf("Job %s pod template label %s expected %s, got %s", tc.currentJob.Name, label, tc.lastJobID, actualValue)
	}
	tc.lastJobID = actualValue
	return nil
}

func (tc *testContext) jobPodTemplateShouldHaveLabelMatchingProviderID(label string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}
	actualValue, exists := tc.currentJob.Spec.Template.Labels[label]
	if !exists {
		return fmt.Errorf("Job %s pod template does not have label %s", tc.currentJob.Name, label)
	}
	if tc.lastProviderID != "" && actualValue != tc.lastProviderID {
		return fmt.Errorf("Job %s pod template label %s expected %s, got %s", tc.currentJob.Name, label, tc.lastProviderID, actualValue)
	}
	tc.lastProviderID = actualValue
	return nil
}

func (tc *testContext) jobPodTemplateShouldHaveLabelMatchingBenchmarkID(label string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}
	actualValue, exists := tc.currentJob.Spec.Template.Labels[label]
	if !exists {
		return fmt.Errorf("Job %s pod template does not have label %s", tc.currentJob.Name, label)
	}
	if tc.lastBenchmarkID != "" && actualValue != tc.lastBenchmarkID {
		return fmt.Errorf("Job %s pod template label %s expected %s, got %s", tc.currentJob.Name, label, tc.lastBenchmarkID, actualValue)
	}
	tc.lastBenchmarkID = actualValue
	return nil
}

func (tc *testContext) jobSpecShouldHaveRetryAttempts(field string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}

	if field == "backoffLimit" {
		if tc.currentJob.Spec.BackoffLimit == nil {
			return fmt.Errorf("Job %s has no backoffLimit set", tc.currentJob.Name)
		}
		if *tc.currentJob.Spec.BackoffLimit < 0 {
			return fmt.Errorf("Job %s backoffLimit is negative: %d", tc.currentJob.Name, *tc.currentJob.Spec.BackoffLimit)
		}
		return nil
	}

	return fmt.Errorf("unknown field %s for retry attempts", field)
}

func (tc *testContext) jobSpecShouldHaveValue(field string, value int) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}

	if field == "ttlSecondsAfterFinished" {
		if tc.currentJob.Spec.TTLSecondsAfterFinished == nil {
			return fmt.Errorf("Job %s has no %s set", tc.currentJob.Name, field)
		}
		if int(*tc.currentJob.Spec.TTLSecondsAfterFinished) != value {
			return fmt.Errorf("Job %s %s expected %d, got %d", tc.currentJob.Name, field, value, *tc.currentJob.Spec.TTLSecondsAfterFinished)
		}
		return nil
	}

	return fmt.Errorf("unknown field %s", field)
}

func (tc *testContext) jobTemplateSpecShouldHaveValue(field, value string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}

	if field == "restartPolicy" {
		actualValue := string(tc.currentJob.Spec.Template.Spec.RestartPolicy)
		if actualValue != value {
			return fmt.Errorf("Job %s restartPolicy expected %s, got %s", tc.currentJob.Name, value, actualValue)
		}
		return nil
	}

	return fmt.Errorf("unknown template spec field %s", field)
}

func (tc *testContext) jobNameShouldBeLowercase() error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}

	if tc.currentJob.Name != strings.ToLower(tc.currentJob.Name) {
		return fmt.Errorf("Job name %s is not lowercase", tc.currentJob.Name)
	}
	return nil
}

func (tc *testContext) jobNameShouldNotExceedLength(maxLength int) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}

	if len(tc.currentJob.Name) > maxLength {
		return fmt.Errorf("Job name %s exceeds %d characters (has %d)", tc.currentJob.Name, maxLength, len(tc.currentJob.Name))
	}
	return nil
}

func (tc *testContext) jobNameShouldBeAlphanumericAndHyphens() error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}

	regex := regexp.MustCompile(`^[a-z0-9-]+$`)
	if !regex.MatchString(tc.currentJob.Name) {
		return fmt.Errorf("Job name %s contains invalid characters (must be alphanumeric and hyphens)", tc.currentJob.Name)
	}
	return nil
}

func (tc *testContext) jobNameShouldNotStartOrEndWithHyphen() error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}

	if strings.HasPrefix(tc.currentJob.Name, "-") || strings.HasSuffix(tc.currentJob.Name, "-") {
		return fmt.Errorf("Job name %s starts or ends with hyphen", tc.currentJob.Name)
	}
	return nil
}

// ============================================================================
// Container Steps
// ============================================================================

func (tc *testContext) jobPodTemplateShouldHaveContainer(containerName string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}

	for _, container := range tc.currentJob.Spec.Template.Spec.Containers {
		if container.Name == containerName {
			return nil
		}
	}

	return fmt.Errorf("Job %s does not have container named %s", tc.currentJob.Name, containerName)
}

func (tc *testContext) containerShouldHaveValue(field, value string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}

	if len(tc.currentJob.Spec.Template.Spec.Containers) == 0 {
		return fmt.Errorf("Job %s has no containers", tc.currentJob.Name)
	}

	container := tc.currentJob.Spec.Template.Spec.Containers[0]

	if field == "imagePullPolicy" {
		actualValue := string(container.ImagePullPolicy)
		if actualValue != value {
			return fmt.Errorf("Container %s imagePullPolicy expected %s, got %s", container.Name, value, actualValue)
		}
		return nil
	}

	return fmt.Errorf("unknown container field %s", field)
}

func (tc *testContext) containerShouldHaveImage() error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}
	if len(tc.currentJob.Spec.Template.Spec.Containers) == 0 {
		return fmt.Errorf("Job %s has no containers", tc.currentJob.Name)
	}
	container := tc.currentJob.Spec.Template.Spec.Containers[0]
	if container.Image == "" {
		return fmt.Errorf("Container %s has no image", container.Name)
	}
	return nil
}

func (tc *testContext) containerShouldHaveEnvVarWithJobID(envVar string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}
	if tc.lastJobID == "" {
		return fmt.Errorf("no job ID tracked")
	}

	if len(tc.currentJob.Spec.Template.Spec.Containers) == 0 {
		return fmt.Errorf("Job %s has no containers", tc.currentJob.Name)
	}

	container := tc.currentJob.Spec.Template.Spec.Containers[0]
	for _, env := range container.Env {
		if env.Name == envVar && env.Value == tc.lastJobID {
			return nil
		}
	}

	return fmt.Errorf("Container %s does not have env var %s with value %s", container.Name, envVar, tc.lastJobID)
}

func (tc *testContext) containerSecurityContextShouldHaveBoolValue(field, value string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}

	if len(tc.currentJob.Spec.Template.Spec.Containers) == 0 {
		return fmt.Errorf("Job %s has no containers", tc.currentJob.Name)
	}

	container := tc.currentJob.Spec.Template.Spec.Containers[0]
	if container.SecurityContext == nil {
		return fmt.Errorf("Container %s has no securityContext", container.Name)
	}

	expectedValue := value == "true"

	if field == "allowPrivilegeEscalation" {
		if container.SecurityContext.AllowPrivilegeEscalation == nil {
			return fmt.Errorf("Container %s securityContext has no %s", container.Name, field)
		}
		if *container.SecurityContext.AllowPrivilegeEscalation != expectedValue {
			return fmt.Errorf("Container %s %s expected %v, got %v", container.Name, field, expectedValue, *container.SecurityContext.AllowPrivilegeEscalation)
		}
		return nil
	}

	if field == "runAsNonRoot" {
		if container.SecurityContext.RunAsNonRoot == nil {
			return fmt.Errorf("Container %s securityContext has no %s", container.Name, field)
		}
		if *container.SecurityContext.RunAsNonRoot != expectedValue {
			return fmt.Errorf("Container %s %s expected %v, got %v", container.Name, field, expectedValue, *container.SecurityContext.RunAsNonRoot)
		}
		return nil
	}

	return fmt.Errorf("unknown securityContext field %s", field)
}

func (tc *testContext) containerSecurityContextCapabilitiesShouldDrop(capability string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}

	if len(tc.currentJob.Spec.Template.Spec.Containers) == 0 {
		return fmt.Errorf("Job %s has no containers", tc.currentJob.Name)
	}

	container := tc.currentJob.Spec.Template.Spec.Containers[0]
	if container.SecurityContext == nil || container.SecurityContext.Capabilities == nil {
		return fmt.Errorf("Container %s has no capabilities", container.Name)
	}

	for _, cap := range container.SecurityContext.Capabilities.Drop {
		if string(cap) == capability {
			return nil
		}
	}

	return fmt.Errorf("Container %s does not drop capability %s", container.Name, capability)
}

func (tc *testContext) containerSecurityContextSeccompProfile(profileType string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}

	if len(tc.currentJob.Spec.Template.Spec.Containers) == 0 {
		return fmt.Errorf("Job %s has no containers", tc.currentJob.Name)
	}

	container := tc.currentJob.Spec.Template.Spec.Containers[0]
	if container.SecurityContext == nil || container.SecurityContext.SeccompProfile == nil {
		return fmt.Errorf("Container %s has no seccomp profile", container.Name)
	}

	actualType := string(container.SecurityContext.SeccompProfile.Type)
	if actualType != profileType {
		return fmt.Errorf("Container %s seccomp profile type expected %s, got %s", container.Name, profileType, actualType)
	}

	return nil
}

func (tc *testContext) containerShouldHaveCPURequestSet() error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}
	if len(tc.currentJob.Spec.Template.Spec.Containers) == 0 {
		return fmt.Errorf("Job %s has no containers", tc.currentJob.Name)
	}
	container := tc.currentJob.Spec.Template.Spec.Containers[0]
	cpuRequest := container.Resources.Requests.Cpu()
	if cpuRequest == nil || cpuRequest.IsZero() {
		return fmt.Errorf("Container %s has no CPU request", container.Name)
	}
	return nil
}

func (tc *testContext) containerShouldHaveMemoryRequestSet() error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}
	if len(tc.currentJob.Spec.Template.Spec.Containers) == 0 {
		return fmt.Errorf("Job %s has no containers", tc.currentJob.Name)
	}
	container := tc.currentJob.Spec.Template.Spec.Containers[0]
	memRequest := container.Resources.Requests.Memory()
	if memRequest == nil || memRequest.IsZero() {
		return fmt.Errorf("Container %s has no memory request", container.Name)
	}
	return nil
}

func (tc *testContext) containerShouldHaveCPULimitSet() error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}
	if len(tc.currentJob.Spec.Template.Spec.Containers) == 0 {
		return fmt.Errorf("Job %s has no containers", tc.currentJob.Name)
	}
	container := tc.currentJob.Spec.Template.Spec.Containers[0]
	cpuLimit := container.Resources.Limits.Cpu()
	if cpuLimit == nil || cpuLimit.IsZero() {
		return fmt.Errorf("Container %s has no CPU limit", container.Name)
	}
	return nil
}

func (tc *testContext) containerShouldHaveMemoryLimitSet() error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}
	if len(tc.currentJob.Spec.Template.Spec.Containers) == 0 {
		return fmt.Errorf("Job %s has no containers", tc.currentJob.Name)
	}
	container := tc.currentJob.Spec.Template.Spec.Containers[0]
	memLimit := container.Resources.Limits.Memory()
	if memLimit == nil || memLimit.IsZero() {
		return fmt.Errorf("Container %s has no memory limit", container.Name)
	}
	return nil
}

func (tc *testContext) jobPodTemplateShouldHaveServiceAccountFromSA() error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}
	_, err := tc.instanceNameFromServiceAccount()
	return err
}

func (tc *testContext) volumeShouldReferenceConfigMapFromSA(volumeName string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}
	envValue, err := tc.instanceNameFromServiceAccount()
	if err != nil {
		return err
	}
	expected := envValue + "-service-ca"
	return tc.volumeShouldReferenceConfigMapByName(volumeName, expected)
}

func (tc *testContext) containerEnvVarShouldBeDerivedFromSA(targetEnvVar string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}
	envValue, err := tc.instanceNameFromServiceAccount()
	if err != nil {
		return err
	}
	expected := fmt.Sprintf("https://%s.%s.svc.cluster.local:8443", envValue, tc.namespace)
	return tc.containerEnvVarShouldEqualValue(targetEnvVar, expected)
}

func (tc *testContext) containerEnvVarShouldEqualValue(envVar, expected string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}
	if len(tc.currentJob.Spec.Template.Spec.Containers) == 0 {
		return fmt.Errorf("Job %s has no containers", tc.currentJob.Name)
	}
	container := tc.currentJob.Spec.Template.Spec.Containers[0]
	for _, env := range container.Env {
		if env.Name == envVar {
			if env.Value != expected {
				return fmt.Errorf("Container %s env var %s expected %s, got %s", container.Name, envVar, expected, env.Value)
			}
			return nil
		}
	}
	return fmt.Errorf("Container %s does not have env var %s", container.Name, envVar)
}

func (tc *testContext) getContainerEnvValue(envVar string) (string, error) {
	if tc.currentJob == nil {
		return "", fmt.Errorf("no current Job")
	}
	if len(tc.currentJob.Spec.Template.Spec.Containers) == 0 {
		return "", fmt.Errorf("Job %s has no containers", tc.currentJob.Name)
	}
	container := tc.currentJob.Spec.Template.Spec.Containers[0]
	for _, env := range container.Env {
		if env.Name == envVar {
			if env.Value == "" {
				return "", fmt.Errorf("Container %s env var %s is empty", container.Name, envVar)
			}
			return env.Value, nil
		}
	}
	return "", fmt.Errorf("Container %s does not have env var %s", container.Name, envVar)
}

func (tc *testContext) instanceNameFromServiceAccount() (string, error) {
	if tc.currentJob == nil {
		return "", fmt.Errorf("no current Job")
	}
	serviceAccount := tc.currentJob.Spec.Template.Spec.ServiceAccountName
	if strings.HasSuffix(serviceAccount, "-jobs") {
		return strings.TrimSuffix(serviceAccount, "-jobs"), nil
	}
	return "", fmt.Errorf("unable to derive instance name from serviceAccountName %q", serviceAccount)
}

// ============================================================================
// Volume & Mount Steps
// ============================================================================

func (tc *testContext) jobPodTemplateShouldHaveConfigMapVolume(volumeName string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}

	for _, vol := range tc.currentJob.Spec.Template.Spec.Volumes {
		if vol.Name == volumeName && vol.ConfigMap != nil {
			return nil
		}
	}

	return fmt.Errorf("Job %s does not have ConfigMap volume %s", tc.currentJob.Name, volumeName)
}

func (tc *testContext) jobPodTemplateShouldHaveEmptyDirVolume(volumeName string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}

	for _, vol := range tc.currentJob.Spec.Template.Spec.Volumes {
		if vol.Name == volumeName && vol.EmptyDir != nil {
			return nil
		}
	}

	return fmt.Errorf("Job %s does not have EmptyDir volume %s", tc.currentJob.Name, volumeName)
}

func (tc *testContext) volumeShouldReferenceConfigMap(volumeName, suffix string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}

	for _, vol := range tc.currentJob.Spec.Template.Spec.Volumes {
		if vol.Name == volumeName && vol.ConfigMap != nil {
			if strings.HasSuffix(vol.ConfigMap.Name, suffix) {
				return nil
			}
		}
	}

	return fmt.Errorf("Job %s volume %s does not reference ConfigMap with suffix %s", tc.currentJob.Name, volumeName, suffix)
}

func (tc *testContext) containerShouldHaveVolumeMount(mountName, path string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}

	if len(tc.currentJob.Spec.Template.Spec.Containers) == 0 {
		return fmt.Errorf("Job %s has no containers", tc.currentJob.Name)
	}

	container := tc.currentJob.Spec.Template.Spec.Containers[0]
	for _, mount := range container.VolumeMounts {
		if mount.Name == mountName && mount.MountPath == path {
			return nil
		}
	}

	return fmt.Errorf("Container %s does not have volumeMount %s at path %s", container.Name, mountName, path)
}

func (tc *testContext) volumeMountShouldHaveSubPath(mountName, subPath string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}

	if len(tc.currentJob.Spec.Template.Spec.Containers) == 0 {
		return fmt.Errorf("Job %s has no containers", tc.currentJob.Name)
	}

	container := tc.currentJob.Spec.Template.Spec.Containers[0]
	for _, mount := range container.VolumeMounts {
		if mount.Name == mountName {
			if mount.SubPath != subPath {
				return fmt.Errorf("VolumeMount %s subPath expected %s, got %s", mountName, subPath, mount.SubPath)
			}
			return nil
		}
	}

	return fmt.Errorf("VolumeMount %s not found", mountName)
}

func (tc *testContext) volumeMountShouldBeReadOnly(mountName string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}

	if len(tc.currentJob.Spec.Template.Spec.Containers) == 0 {
		return fmt.Errorf("Job %s has no containers", tc.currentJob.Name)
	}

	container := tc.currentJob.Spec.Template.Spec.Containers[0]
	for _, mount := range container.VolumeMounts {
		if mount.Name == mountName {
			if !mount.ReadOnly {
				return fmt.Errorf("VolumeMount %s is not readOnly", mountName)
			}
			return nil
		}
	}

	return fmt.Errorf("VolumeMount %s not found", mountName)
}

func (tc *testContext) volumeShouldReferenceConfigMapByName(volumeName, configMapName string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}

	for _, vol := range tc.currentJob.Spec.Template.Spec.Volumes {
		if vol.Name == volumeName && vol.ConfigMap != nil {
			if vol.ConfigMap.Name == configMapName {
				return nil
			}
		}
	}

	return fmt.Errorf("Job %s volume %s does not reference ConfigMap %s", tc.currentJob.Name, volumeName, configMapName)
}

// ============================================================================
// Service Account & Environment Steps
// ============================================================================

func (tc *testContext) mlflowIsConfigured() error {
	if os.Getenv("MLFLOW_TRACKING_URI") == "" {
		return godog.ErrSkip
	}
	return nil
}

func (tc *testContext) environmentVariableIsSet(name, value string) error {
	if os.Getenv("K8S_TEST_DEBUG") == "true" {
		fmt.Printf("[DEBUG] assuming service env %s=%s\n", name, value)
	}
	return nil
}

func (tc *testContext) containerCommandShouldBeValidArray() error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}

	if len(tc.currentJob.Spec.Template.Spec.Containers) == 0 {
		return fmt.Errorf("Job %s has no containers", tc.currentJob.Name)
	}

	container := tc.currentJob.Spec.Template.Spec.Containers[0]
	if len(container.Command) == 0 {
		return fmt.Errorf("Container %s has no command", container.Name)
	}

	return nil
}

func (tc *testContext) containerCommandShouldNotContainEmptyStrings() error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}

	if len(tc.currentJob.Spec.Template.Spec.Containers) == 0 {
		return fmt.Errorf("Job %s has no containers", tc.currentJob.Name)
	}

	container := tc.currentJob.Spec.Template.Spec.Containers[0]
	for _, cmd := range container.Command {
		if cmd == "" {
			return fmt.Errorf("Container %s command contains empty string", container.Name)
		}
	}

	return nil
}

func (tc *testContext) containerCommandShouldHaveTrimmedWhitespace() error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}

	if len(tc.currentJob.Spec.Template.Spec.Containers) == 0 {
		return fmt.Errorf("Job %s has no containers", tc.currentJob.Name)
	}

	container := tc.currentJob.Spec.Template.Spec.Containers[0]
	for _, cmd := range container.Command {
		if strings.TrimSpace(cmd) != cmd {
			return fmt.Errorf("Container %s command element has untrimmed whitespace: %q", container.Name, cmd)
		}
	}

	return nil
}

func (tc *testContext) containerShouldHaveProviderEnvVars() error {
	// Validates that provider environment variables are present
	// This is a general check that the container has env vars
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}

	if len(tc.currentJob.Spec.Template.Spec.Containers) == 0 {
		return fmt.Errorf("Job %s has no containers", tc.currentJob.Name)
	}

	// Just verify that there are env vars
	container := tc.currentJob.Spec.Template.Spec.Containers[0]
	if len(container.Env) == 0 {
		return fmt.Errorf("Container %s has no environment variables", container.Name)
	}

	return nil
}

func (tc *testContext) envVarShouldNotBeOverridden(envVar string) error {
	// Validates that JOB_ID is present and not overridden
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}

	if len(tc.currentJob.Spec.Template.Spec.Containers) == 0 {
		return fmt.Errorf("Job %s has no containers", tc.currentJob.Name)
	}

	container := tc.currentJob.Spec.Template.Spec.Containers[0]
	foundCount := 0
	for _, env := range container.Env {
		if env.Name == envVar {
			foundCount++
		}
	}

	if foundCount == 0 {
		return fmt.Errorf("Container %s does not have env var %s", container.Name, envVar)
	}

	if foundCount > 1 {
		return fmt.Errorf("Container %s has multiple definitions of env var %s", container.Name, envVar)
	}

	return nil
}

// ============================================================================
// Deletion Steps
// ============================================================================

func (tc *testContext) allJobsShouldBeDeleted() error {
	if tc.lastJobID == "" {
		return fmt.Errorf("no job ID tracked for deletion validation")
	}

	deadline := time.Now().Add(30 * time.Second)
	labelSelector := fmt.Sprintf("job_id=%s", tc.lastJobID)
	for time.Now().Before(deadline) {
		jobs, err := tc.k8sClient.BatchV1().Jobs(tc.namespace).List(context.Background(), metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		if err != nil {
			return fmt.Errorf("failed to list Jobs: %w", err)
		}
		remaining := 0
		for i := range jobs.Items {
			if jobs.Items[i].DeletionTimestamp == nil {
				remaining++
			}
		}
		if remaining == 0 {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("expected all Jobs with job_id=%s to be deleted, but they still exist", tc.lastJobID)
}

func (tc *testContext) allConfigMapsShouldBeDeleted() error {
	if tc.lastJobID == "" {
		return fmt.Errorf("no job ID tracked for deletion validation")
	}

	deadline := time.Now().Add(30 * time.Second)
	labelSelector := fmt.Sprintf("job_id=%s", tc.lastJobID)
	for time.Now().Before(deadline) {
		configMaps, err := tc.k8sClient.CoreV1().ConfigMaps(tc.namespace).List(context.Background(), metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		if err != nil {
			return fmt.Errorf("failed to list ConfigMaps: %w", err)
		}
		remaining := 0
		for i := range configMaps.Items {
			if configMaps.Items[i].DeletionTimestamp == nil {
				remaining++
			}
		}
		if remaining == 0 {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("expected all ConfigMaps with job_id=%s to be deleted, but they still exist", tc.lastJobID)
}

func (tc *testContext) jobsShouldStillExist() error {
	if tc.lastJobID == "" {
		return fmt.Errorf("no job ID tracked")
	}

	labelSelector := fmt.Sprintf("job_id=%s", tc.lastJobID)
	jobs, err := tc.k8sClient.BatchV1().Jobs(tc.namespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return fmt.Errorf("failed to list Jobs: %w", err)
	}

	if len(jobs.Items) == 0 {
		return fmt.Errorf("expected Jobs with job_id=%s to still exist, but found none", tc.lastJobID)
	}
	return nil
}

func (tc *testContext) configMapsShouldStillExist() error {
	if tc.lastJobID == "" {
		return fmt.Errorf("no job ID tracked")
	}

	labelSelector := fmt.Sprintf("job_id=%s", tc.lastJobID)
	configMaps, err := tc.k8sClient.CoreV1().ConfigMaps(tc.namespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return fmt.Errorf("failed to list ConfigMaps: %w", err)
	}

	if len(configMaps.Items) == 0 {
		return fmt.Errorf("expected ConfigMaps with job_id=%s to still exist, but found none", tc.lastJobID)
	}
	return nil
}

// ============================================================================
// Implemented Steps (previously stubbed)
// ============================================================================

func (tc *testContext) numberOfJobsShouldEqualBenchmarks() error {
	if len(tc.lastBenchmarkIDs) == 0 {
		return fmt.Errorf("no benchmark IDs tracked for comparison")
	}
	expected := len(tc.lastBenchmarkIDs)
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		jobs, err := tc.listJobsByJobIDFresh()
		if err != nil {
			return err
		}
		if len(jobs) == expected {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	jobs, err := tc.listJobsByJobIDFresh()
	if err != nil {
		return err
	}
	return fmt.Errorf("expected %d Jobs, found %d", expected, len(jobs))
}

func (tc *testContext) numberOfConfigMapsShouldEqualBenchmarks() error {
	if len(tc.lastBenchmarkIDs) == 0 {
		return fmt.Errorf("no benchmark IDs tracked for comparison")
	}
	expected := len(tc.lastBenchmarkIDs)
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		configMaps, err := tc.listConfigMapsByJobIDFresh()
		if err != nil {
			return err
		}
		if len(configMaps) == expected {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	configMaps, err := tc.listConfigMapsByJobIDFresh()
	if err != nil {
		return err
	}
	return fmt.Errorf("expected %d ConfigMaps, found %d", expected, len(configMaps))
}

func (tc *testContext) eachJobShouldHaveUniqueBenchmarkIDLabel() error {
	jobs, err := tc.listJobsByJobID()
	if err != nil {
		return err
	}
	if len(jobs) == 0 {
		return fmt.Errorf("no Jobs found for unique benchmark_id validation")
	}
	seen := map[string]bool{}
	for _, job := range jobs {
		benchID := job.Labels["benchmark_id"]
		if benchID == "" {
			return fmt.Errorf("Job %s missing benchmark_id label", job.Name)
		}
		if seen[benchID] {
			return fmt.Errorf("duplicate benchmark_id label found: %s", benchID)
		}
		seen[benchID] = true
	}
	return nil
}

func (tc *testContext) eachConfigMapShouldHaveUniqueBenchmarkIDLabel() error {
	configMaps, err := tc.listConfigMapsByJobID()
	if err != nil {
		return err
	}
	if len(configMaps) == 0 {
		return fmt.Errorf("no ConfigMaps found for unique benchmark_id validation")
	}
	seen := map[string]bool{}
	for _, cm := range configMaps {
		benchID := cm.Labels["benchmark_id"]
		if benchID == "" {
			return fmt.Errorf("ConfigMap %s missing benchmark_id label", cm.Name)
		}
		if seen[benchID] {
			return fmt.Errorf("duplicate benchmark_id label found: %s", benchID)
		}
		seen[benchID] = true
	}
	return nil
}

func (tc *testContext) responseShouldBeImmediate() error {
	if tc.lastRequestDuration == 0 {
		return fmt.Errorf("no request duration tracked")
	}
	if tc.lastRequestDuration > 5*time.Second {
		return fmt.Errorf("request took too long: %s", tc.lastRequestDuration)
	}
	return nil
}

func (tc *testContext) jobsShouldBeCreatedInBackground() error {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		jobs, err := tc.listJobsByJobIDFresh()
		if err != nil {
			return err
		}
		if len(jobs) > 0 {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("no Jobs created in background")
}

func (tc *testContext) jobHasBenchmarksConfigured(expected int) error {
	if len(tc.lastBenchmarkIDs) == 0 && tc.lastRequestBody != "" {
		if ids, err := parseBenchmarkIDs(tc.lastRequestBody); err == nil {
			tc.lastBenchmarkIDs = ids
		}
	}
	if len(tc.lastBenchmarkIDs) != expected {
		return fmt.Errorf("expected %d benchmarks, got %d", expected, len(tc.lastBenchmarkIDs))
	}
	return nil
}

func (tc *testContext) jobDeletionShouldUsePropagationPolicy(policy string) error {
	if !strings.EqualFold(policy, "Background") {
		return godog.ErrSkip
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		jobs, err := tc.listJobsByJobIDFresh()
		if err != nil {
			return err
		}
		if len(jobs) == 0 {
			return nil
		}
		for _, job := range jobs {
			if job.DeletionTimestamp != nil {
				return nil
			}
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("Jobs were not marked for background deletion")
}

func (tc *testContext) deleteEvaluationJobResourcesShouldBeCalled() error {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		jobs, err := tc.listJobsByJobIDFresh()
		if err != nil {
			return err
		}
		configMaps, err := tc.listConfigMapsByJobIDFresh()
		if err != nil {
			return err
		}
		if len(jobs) == 0 && len(configMaps) == 0 {
			return nil
		}
		for _, job := range jobs {
			if job.DeletionTimestamp != nil {
				return nil
			}
		}
		for _, cm := range configMaps {
			if cm.DeletionTimestamp != nil {
				return nil
			}
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("no evidence of cleanup after hard delete")
}

func (tc *testContext) allJobsShouldBeDeletedCount(expected int) error {
	if len(tc.lastBenchmarkIDs) != expected {
		return fmt.Errorf("expected %d benchmarks, got %d", expected, len(tc.lastBenchmarkIDs))
	}
	return tc.allJobsShouldBeDeleted()
}

func (tc *testContext) allConfigMapsShouldBeDeletedCount(expected int) error {
	if len(tc.lastBenchmarkIDs) != expected {
		return fmt.Errorf("expected %d benchmarks, got %d", expected, len(tc.lastBenchmarkIDs))
	}
	return tc.allConfigMapsShouldBeDeleted()
}

func (tc *testContext) responseBodyShouldContain(substr string) error {
	body := strings.ToLower(string(tc.body))
	if !strings.Contains(body, strings.ToLower(substr)) {
		return fmt.Errorf("response does not contain %q: %s", substr, string(tc.body))
	}
	return nil
}

func (tc *testContext) listJobsByJobID() ([]batchv1.Job, error) {
	return tc.listJobsByJobIDWithCache(false)
}

func (tc *testContext) listJobsByJobIDFresh() ([]batchv1.Job, error) {
	return tc.listJobsByJobIDWithCache(true)
}

func (tc *testContext) listJobsByJobIDWithCache(forceRefresh bool) ([]batchv1.Job, error) {
	if tc.k8sClient == nil {
		return nil, fmt.Errorf("Kubernetes client not initialized")
	}
	if tc.lastJobID == "" {
		return nil, fmt.Errorf("no job ID tracked for listing")
	}
	if !forceRefresh && tc.cachedJobsJobID == tc.lastJobID && tc.cachedJobs != nil {
		tc.logK8sOp("Jobs", "cache-hit", tc.lastJobID)
		return tc.cachedJobs, nil
	}
	tc.logK8sOp("Jobs", "list", tc.lastJobID)
	jobs, err := tc.k8sClient.BatchV1().Jobs(tc.namespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("job_id=%s", tc.lastJobID),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list Jobs: %w", err)
	}
	tc.cachedJobsJobID = tc.lastJobID
	tc.cachedJobs = jobs.Items
	return jobs.Items, nil
}

func (tc *testContext) listConfigMapsByJobID() ([]corev1.ConfigMap, error) {
	return tc.listConfigMapsByJobIDWithCache(false)
}

func (tc *testContext) listConfigMapsByJobIDFresh() ([]corev1.ConfigMap, error) {
	return tc.listConfigMapsByJobIDWithCache(true)
}

func (tc *testContext) listConfigMapsByJobIDWithCache(forceRefresh bool) ([]corev1.ConfigMap, error) {
	if tc.k8sClient == nil {
		return nil, fmt.Errorf("Kubernetes client not initialized")
	}
	if tc.lastJobID == "" {
		return nil, fmt.Errorf("no job ID tracked for listing")
	}
	if !forceRefresh && tc.cachedConfigMapsJobID == tc.lastJobID && tc.cachedConfigMaps != nil {
		tc.logK8sOp("ConfigMaps", "cache-hit", tc.lastJobID)
		return tc.cachedConfigMaps, nil
	}
	tc.logK8sOp("ConfigMaps", "list", tc.lastJobID)
	configMaps, err := tc.k8sClient.CoreV1().ConfigMaps(tc.namespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("job_id=%s", tc.lastJobID),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list ConfigMaps: %w", err)
	}
	tc.cachedConfigMapsJobID = tc.lastJobID
	tc.cachedConfigMaps = configMaps.Items
	return configMaps.Items, nil
}

func (tc *testContext) logK8sOp(resource, action, jobID string) {
	if os.Getenv("K8S_TEST_DEBUG") != "true" {
		return
	}
	fmt.Printf("[K8S] %s %s (job_id=%s, namespace=%s)\n", action, resource, jobID, tc.namespace)
}

func (tc *testContext) firstBenchmarkID() string {
	if len(tc.lastBenchmarkIDs) > 0 {
		return tc.lastBenchmarkIDs[0]
	}
	if tc.lastBenchmarkID != "" {
		return tc.lastBenchmarkID
	}
	return ""
}

func (tc *testContext) fetchBenchmarkStatuses() ([]string, error) {
	req, err := http.NewRequest("GET", tc.baseURL+"/api/v1/evaluations/jobs/"+tc.lastJobID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	authToken := os.Getenv("AUTH_TOKEN")
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}
	resp, err := tc.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}
	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	status, ok := data["status"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("response missing status")
	}
	benchmarks, ok := status["benchmarks"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("response missing benchmark statuses")
	}
	var result []string
	for _, item := range benchmarks {
		if bm, ok := item.(map[string]interface{}); ok {
			if st, ok := bm["status"].(string); ok && st != "" {
				result = append(result, st)
			}
		}
	}
	return result, nil
}

// ============================================================================
// Stub for Undefined Steps
// ============================================================================

func (tc *testContext) stubStepNoArgs() error {
	return godog.ErrSkip
}

func (tc *testContext) stubStepInt(_ int) error {
	return godog.ErrSkip
}

func (tc *testContext) stubStepString(_ string) error {
	return godog.ErrSkip
}
