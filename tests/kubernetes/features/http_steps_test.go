package features

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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

func (tc *testContext) iSetHeaderTo(paramName, paramValue string) error {
	value, err := resolveK8sStepValue(paramValue)
	if err != nil {
		return err
	}
	tc.reqHeaders[paramName] = value
	return nil
}

func (tc *testContext) iUnsetHeader(paramName string) {
	delete(tc.reqHeaders, paramName)
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

	tc.applyAPIHeaders(req)

	start := time.Now()
	tc.response, err = tc.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = tc.response.Body.Close() }()

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
			authToken := os.Getenv("AUTH_TOKEN")
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
			listCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			jobs, err := tc.k8sClient.BatchV1().Jobs(tc.namespace).List(listCtx, metav1.ListOptions{
				LabelSelector: fmt.Sprintf("job_id=%s", tc.lastJobID),
			})
			cancel()
			if err == nil && len(jobs.Items) > 0 {
				tc.currentJob = &jobs.Items[0]
				tc.jobs = append(tc.jobs, tc.currentJob)
			}
		}
		if tc.currentConfigMap == nil {
			listCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			configMaps, err := tc.k8sClient.CoreV1().ConfigMaps(tc.namespace).List(listCtx, metav1.ListOptions{
				LabelSelector: fmt.Sprintf("job_id=%s", tc.lastJobID),
			})
			cancel()
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
