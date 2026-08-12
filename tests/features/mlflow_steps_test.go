package features

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/pkg/mlflowclient"

	"github.com/PaesslerAG/jsonpath"
)

// --- MLflow Artifact Step Definitions ---

func (tc *scenarioConfig) iFetchMLflowArtifact(artifactPath, runIDPattern string) error {
	// Resolve run ID from saved values or use literal
	runID, err := tc.getValue(runIDPattern)
	if err != nil {
		return tc.logError(fmt.Errorf("failed to get run ID from pattern %q: %w", runIDPattern, err))
	}
	if runID == "" {
		return tc.logError(fmt.Errorf("run ID pattern %q resolved to empty string", runIDPattern))
	}

	// Extract experiment ID from the current response body
	var experimentID string
	var respData map[string]interface{}
	if err := json.Unmarshal(tc.body, &respData); err == nil {
		if resource, ok := respData["resource"].(map[string]interface{}); ok {
			if expIDFloat, ok := resource["mlflow_experiment_id"].(float64); ok {
				experimentID = fmt.Sprintf("%.0f", expIDFloat)
			} else if expIDStr, ok := resource["mlflow_experiment_id"].(string); ok {
				experimentID = expIDStr
			}
		}
	}
	if experimentID == "" {
		return tc.logError(fmt.Errorf("failed to extract mlflow_experiment_id from response"))
	}

	return tc.fetchMLflowArtifactWithExperimentID(artifactPath, experimentID, runID)
}

func (tc *scenarioConfig) fetchMLflowArtifactWithExperimentID(artifactPath, experimentID, runID string) error {
	client, err := tc.mlflowClient()
	if err != nil {
		tc.mlflowArtifactError = err
		return tc.logError(err)
	}

	fullPath := fmt.Sprintf("%s/%s/artifacts/%s", experimentID, runID, artifactPath)
	tc.logDebug("Fetching MLflow artifact: %s\n", fullPath)

	reader, err := client.DownloadArtifact(fullPath)
	if err != nil {
		tc.mlflowArtifactError = err
		return tc.logError(fmt.Errorf("failed to fetch MLflow artifact: %w", err))
	}
	defer func() { _ = reader.Close() }()

	body, err := io.ReadAll(reader)
	if err != nil {
		tc.mlflowArtifactError = err
		return tc.logError(fmt.Errorf("failed to read MLflow artifact body: %w", err))
	}

	tc.mlflowArtifactBody = body
	tc.mlflowArtifactError = nil
	tc.logDebug("MLflow artifact fetched successfully (%d bytes)\n", len(body))
	return nil
}

func (tc *scenarioConfig) theMLflowArtifactShouldExist() error {
	if tc.mlflowArtifactError != nil {
		return tc.logError(fmt.Errorf("MLflow artifact fetch failed: %w", tc.mlflowArtifactError))
	}
	if len(tc.mlflowArtifactBody) == 0 {
		return tc.logError(fmt.Errorf("MLflow artifact is empty"))
	}
	return nil
}

func (tc *scenarioConfig) theMLflowArtifactShouldContain(pathOrString string) error {
	if tc.mlflowArtifactError != nil {
		return tc.logError(fmt.Errorf("MLflow artifact fetch failed: %w", tc.mlflowArtifactError))
	}

	// If it looks like a JSONPath (contains . or [), validate as path
	if strings.Contains(pathOrString, ".") || strings.Contains(pathOrString, "[") {
		var jsonData interface{}
		if err := json.Unmarshal(tc.mlflowArtifactBody, &jsonData); err != nil {
			return tc.logError(fmt.Errorf("failed to parse MLflow artifact as JSON: %w", err))
		}

		jsonPath := "$." + pathOrString
		_, err := jsonpath.Get(jsonPath, jsonData)
		if err != nil {
			return tc.logError(fmt.Errorf("MLflow artifact does not contain path %q: %w. Body: %s", pathOrString, err, string(tc.mlflowArtifactBody)))
		}
		return nil
	}

	// Otherwise do literal string search
	if !strings.Contains(string(tc.mlflowArtifactBody), pathOrString) {
		return tc.logError(fmt.Errorf("MLflow artifact does not contain %q. Body: %s", pathOrString, string(tc.mlflowArtifactBody)))
	}
	return nil
}

func (tc *scenarioConfig) theMLflowArtifactShouldContainValueAtPath(expected, jsonPath string) error {
	if tc.mlflowArtifactError != nil {
		return tc.logError(fmt.Errorf("MLflow artifact fetch failed: %w", tc.mlflowArtifactError))
	}

	// Parse artifact as JSON
	var artifactData interface{}
	if err := json.Unmarshal(tc.mlflowArtifactBody, &artifactData); err != nil {
		return tc.logError(fmt.Errorf("failed to parse MLflow artifact as JSON: %w", err))
	}

	// Evaluate JSONPath
	result, err := jsonpath.Get(jsonPath, artifactData)
	if err != nil {
		return tc.logError(fmt.Errorf("failed to evaluate JSONPath %q on MLflow artifact: %w", jsonPath, err))
	}

	// Convert result to string for comparison
	var resultStr string
	switch v := result.(type) {
	case string:
		resultStr = v
	case float64:
		resultStr = fmt.Sprintf("%g", v)
	case bool:
		resultStr = fmt.Sprintf("%t", v)
	default:
		resultBytes, _ := json.Marshal(v)
		resultStr = string(resultBytes)
	}

	// Resolve expected value from saved values
	expectedResolved, err := tc.getValue(expected)
	if err != nil {
		return tc.logError(fmt.Errorf("failed to resolve expected value %q: %w", expected, err))
	}

	if resultStr != expectedResolved {
		return tc.logError(fmt.Errorf("MLflow artifact path %q = %q, expected %q", jsonPath, resultStr, expectedResolved))
	}

	return nil
}

func (tc *scenarioConfig) iFetchMLflowArtifactByExperimentAndJob(artifactName, experimentID, jobID string) error {
	experimentIDResolved, jobIDResolved, err := tc.resolveMLflowExperimentAndJobIDs(experimentID, jobID)
	if err != nil {
		return err
	}

	runID, err := tc.findMLflowRunIDForJob(experimentIDResolved, jobIDResolved)
	if err != nil {
		return err
	}
	if runID == "" {
		return tc.logError(fmt.Errorf("no MLflow run found for job %s in experiment %s", jobIDResolved, experimentIDResolved))
	}

	return tc.fetchMLflowArtifactWithExperimentID(artifactName, experimentIDResolved, runID)
}

func (tc *scenarioConfig) theMLflowArtifactShouldNotExistForExperimentAndJob(artifactName, experimentID, jobID string) error {
	experimentIDResolved, jobIDResolved, err := tc.resolveMLflowExperimentAndJobIDs(experimentID, jobID)
	if err != nil {
		return err
	}

	runID, err := tc.findMLflowRunIDForJob(experimentIDResolved, jobIDResolved)
	if err != nil {
		return err
	}
	if runID == "" {
		tc.logDebug("No MLflow run for job %s in experiment %s; EvalCard absent as expected\n", jobIDResolved, experimentIDResolved)
		return nil
	}

	exists, err := tc.mlflowArtifactExists(artifactName, experimentIDResolved, runID)
	if err != nil {
		return err
	}
	if exists {
		return tc.logError(fmt.Errorf("expected MLflow artifact %q to be absent for pending job %s in experiment %s, but it exists", artifactName, jobIDResolved, experimentIDResolved))
	}
	tc.logDebug("MLflow artifact %q absent for job %s as expected\n", artifactName, jobIDResolved)
	return nil
}

func (tc *scenarioConfig) mlflowArtifactExists(artifactPath, experimentID, runID string) (bool, error) {
	client, err := tc.mlflowClient()
	if err != nil {
		return false, tc.logError(err)
	}

	fullPath := fmt.Sprintf("%s/%s/artifacts/%s", experimentID, runID, artifactPath)
	reader, err := client.DownloadArtifact(fullPath)
	if err != nil {
		var apiErr *mlflowclient.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return false, nil
		}
		return false, tc.logError(fmt.Errorf("failed to check MLflow artifact: %w", err))
	}
	_ = reader.Close()
	return true, nil
}

func (tc *scenarioConfig) resolveMLflowExperimentAndJobIDs(experimentID, jobID string) (string, string, error) {
	experimentIDResolved, err := tc.getValue(experimentID)
	if err != nil {
		return "", "", tc.logError(fmt.Errorf("failed to resolve experiment ID %q: %w", experimentID, err))
	}
	jobIDResolved, err := tc.getValue(jobID)
	if err != nil {
		return "", "", tc.logError(fmt.Errorf("failed to resolve job ID %q: %w", jobID, err))
	}
	return experimentIDResolved, jobIDResolved, nil
}

func (tc *scenarioConfig) findMLflowRunIDForJob(experimentIDResolved, jobIDResolved string) (string, error) {
	client, err := tc.mlflowClient()
	if err != nil {
		return "", tc.logError(err)
	}

	resp, err := client.SearchRuns(&mlflowclient.SearchRunsRequest{
		ExperimentIDs: []string{experimentIDResolved},
		Filter:        fmt.Sprintf("tags.evaluation_job_id = '%s'", jobIDResolved),
	})
	if err != nil {
		return "", tc.logError(fmt.Errorf("failed to search MLflow runs: %w", err))
	}

	if len(resp.Runs) == 0 {
		return "", nil
	}
	return resp.Runs[0].Info.RunID, nil
}

func (tc *scenarioConfig) theMLflowArtifactShouldBeValidJSON() error {
	if tc.mlflowArtifactError != nil {
		return tc.logError(fmt.Errorf("MLflow artifact fetch failed: %w", tc.mlflowArtifactError))
	}

	var jsonData interface{}
	if err := json.Unmarshal(tc.mlflowArtifactBody, &jsonData); err != nil {
		return tc.logError(fmt.Errorf("MLflow artifact is not valid JSON: %w. Body: %s", err, string(tc.mlflowArtifactBody)))
	}

	tc.logDebug("MLflow artifact is valid JSON\n")
	return nil
}

func (tc *scenarioConfig) theMLflowArtifactFieldShouldMatchISO8601(fieldPath string) error {
	if tc.mlflowArtifactError != nil {
		return tc.logError(fmt.Errorf("MLflow artifact fetch failed: %w", tc.mlflowArtifactError))
	}

	// Parse artifact as JSON
	var jsonData interface{}
	if err := json.Unmarshal(tc.mlflowArtifactBody, &jsonData); err != nil {
		return tc.logError(fmt.Errorf("failed to parse MLflow artifact as JSON: %w", err))
	}

	// Extract value using JSONPath
	jsonPath := "$." + fieldPath
	results, err := jsonpath.Get(jsonPath, jsonData)
	if err != nil {
		return tc.logError(fmt.Errorf("failed to evaluate JSONPath %q on MLflow artifact: %w", jsonPath, err))
	}

	resultStr, ok := results.(string)
	if !ok {
		return tc.logError(fmt.Errorf("MLflow artifact field %q is not a string: %T", fieldPath, results))
	}

	// ISO 8601 regex pattern (supports formats like: 2026-07-23T14:30:55.691568184Z, 2026-07-23T14:30:55Z, 2026-07-23T14:30:55+00:00)
	iso8601Pattern := `^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})?$`
	matched, err := regexp.MatchString(iso8601Pattern, resultStr)
	if err != nil {
		return tc.logError(fmt.Errorf("failed to match ISO 8601 pattern: %w", err))
	}

	if !matched {
		return tc.logError(fmt.Errorf("MLflow artifact field %q value %q does not match ISO 8601 format", fieldPath, resultStr))
	}

	tc.logDebug("MLflow artifact field %q is valid ISO 8601: %s\n", fieldPath, resultStr)
	return nil
}

func (tc *scenarioConfig) iWaitForEvaluationJobStatusToMatch(statusPattern string) error {
	// Compile regex once before polling loop for fail-fast validation and performance
	re, err := regexp.Compile(statusPattern)
	if err != nil {
		return tc.logError(fmt.Errorf("invalid status pattern %q: %w", statusPattern, err))
	}

	deadline := time.Now().Add(tc.waitDeadline)
	var lastErr error
	var lastStatus string

	for time.Now().Before(deadline) {
		// Get current status using {id} pattern which auto-substitutes
		if err := tc.iSendARequestImpl(http.MethodGet, "/api/v1/evaluations/jobs/{id}", "", "wait for evaluation job status to match pattern"); err != nil {
			lastErr = err
			time.Sleep(tc.waitInterval)
			continue
		}

		if tc.response != nil && tc.response.StatusCode == http.StatusOK {
			status, err := tc.getJsonPath("$.status.state")
			if status != "" {
				lastStatus = status
			}
			if err != nil {
				lastErr = err
				time.Sleep(tc.waitInterval)
				continue
			}

			// Check if state matches the pattern (supports regex like "completed|failed")
			if re.MatchString(status) {
				tc.logDebug("Job reached status matching %q: %s\n", statusPattern, status)
				return nil
			}

			tc.logDebug("Waiting for job status to match %q, current: %s\n", statusPattern, status)
		}

		time.Sleep(tc.waitInterval)
	}

	if lastErr != nil {
		return tc.logError(fmt.Errorf("timeout waiting for job status to match %q, last error: %w, last status: %s", statusPattern, lastErr, lastStatus))
	}
	return tc.logError(fmt.Errorf("timeout waiting for job status to match %q, last status: %s", statusPattern, lastStatus))
}

func TestMlflowBaseURL(t *testing.T) {
	t.Run("requires MLFLOW_TRACKING_URI", func(t *testing.T) {
		t.Setenv(envMlflowTrackingURI, "")
		if _, err := mlflowBaseURL(); err == nil {
			t.Fatal("expected error when MLFLOW_TRACKING_URI is unset")
		}
	})

	t.Run("rejects slash-only MLFLOW_TRACKING_URI", func(t *testing.T) {
		t.Setenv(envMlflowTrackingURI, "///")
		if _, err := mlflowBaseURL(); err == nil {
			t.Fatal("expected error when MLFLOW_TRACKING_URI is empty after trimming")
		}
	})

	t.Run("trims trailing slashes", func(t *testing.T) {
		t.Setenv(envMlflowTrackingURI, "https://mlflow.example.com/")
		got, err := mlflowBaseURL()
		if err != nil {
			t.Fatalf("mlflowBaseURL: %v", err)
		}
		if got != "https://mlflow.example.com" {
			t.Fatalf("got %q, want https://mlflow.example.com", got)
		}
	})
}
