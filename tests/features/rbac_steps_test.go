package features

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// setResourceIDFromEnvironment supplies an existing resource ID to RBAC
// scenarios without changing the shared {id} request substitution behavior.
func (tc *scenarioConfig) setResourceIDFromEnvironment(envName string) error {
	envName = strings.TrimSpace(envName)
	if envName == "" {
		return tc.logError(fmt.Errorf("resource ID environment variable name is empty"))
	}

	resourceID := strings.TrimSpace(os.Getenv(envName))
	if resourceID == "" {
		return tc.logError(fmt.Errorf("resource ID environment variable %s is not set", envName))
	}

	tc.lastId = resourceID
	tc.values["id"] = resourceID
	return nil
}

type mlflowExperimentResponse struct {
	ExperimentID string `json:"experiment_id"`
	Experiment   struct {
		ExperimentID string `json:"experiment_id"`
		Name         string `json:"name"`
	} `json:"experiment"`
}

type mlflowExperimentListResponse struct {
	Experiments []struct {
		ExperimentID string `json:"experiment_id"`
		Name         string `json:"name"`
	} `json:"experiments"`
}

// sendMLflowRBACRequest calls the MLflow endpoint with the same bearer token and
// tenant workspace used by the current non-admin identity.
func (tc *scenarioConfig) sendMLflowRBACRequest(method, path string, payload any) (int, []byte, error) {
	baseURL, err := mlflowBaseURL()
	if err != nil {
		return 0, nil, err
	}
	token := strings.TrimSpace(os.Getenv("AUTH_TOKEN"))
	if token == "" {
		return 0, nil, fmt.Errorf("AUTH_TOKEN is required for direct MLflow RBAC checks")
	}

	var body io.Reader
	if payload != nil {
		encoded, marshalErr := json.Marshal(payload)
		if marshalErr != nil {
			return 0, nil, fmt.Errorf("marshal MLflow request: %w", marshalErr)
		}
		body = strings.NewReader(string(encoded))
	}
	request, err := http.NewRequestWithContext(context.Background(), method, strings.TrimRight(baseURL, "/")+path, body)
	if err != nil {
		return 0, nil, fmt.Errorf("build MLflow request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-MLFLOW-WORKSPACE", tc.mlflowWorkspace())
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := getMLflowHTTPClient().Do(request)
	if err != nil {
		return 0, nil, fmt.Errorf("send MLflow request: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return response.StatusCode, nil, fmt.Errorf("read MLflow response: %w", err)
	}
	return response.StatusCode, responseBody, nil
}

func (tc *scenarioConfig) viewerCannotAccessMLflowExperiments() error {
	experimentID := strings.TrimSpace(tc.values["id"])
	if experimentID == "" {
		return tc.logError(fmt.Errorf("viewer MLflow experiment ID is missing"))
	}

	query := url.Values{"experiment_id": []string{experimentID}}
	createName := fmt.Sprintf("evalhub-viewer-rbac-%d", time.Now().UTC().UnixNano())
	// Use an invalid ID so write probes cannot rename or delete a real
	// experiment, while still reaching the viewer permission check.
	probeExperimentID := "rbac-probe-nonexistent"
	requests := []struct {
		operation string
		method    string
		path      string
		payload   any
	}{
		{
			operation: "get",
			method:    http.MethodGet,
			path:      "/api/2.0/mlflow/experiments/get?" + query.Encode(),
		},
		{
			operation: "list",
			method:    http.MethodPost,
			path:      "/api/2.0/mlflow/experiments/search",
			payload:   map[string]any{"max_results": 1},
		},
		{
			operation: "create",
			method:    http.MethodPost,
			path:      "/api/2.0/mlflow/experiments/create",
			// Missing the required name keeps an unexpected permission grant from
			// creating a persistent experiment.
			payload: map[string]any{},
		},
		{
			operation: "update",
			method:    http.MethodPost,
			path:      "/api/2.0/mlflow/experiments/update",
			payload: map[string]any{
				"experiment_id": probeExperimentID,
				"new_name":      createName,
			},
		},
		{
			operation: "delete",
			method:    http.MethodPost,
			path:      "/api/2.0/mlflow/experiments/delete",
			payload:   map[string]any{"experiment_id": probeExperimentID},
		},
	}

	for _, request := range requests {
		status, body, err := tc.sendMLflowRBACRequest(request.method, request.path, request.payload)
		if err != nil {
			return tc.logError(fmt.Errorf("MLflow %s request failed: %w", request.operation, err))
		}
		if status != http.StatusForbidden {
			return tc.logError(fmt.Errorf("viewer MLflow %s returned HTTP %d, expected 403: %s", request.operation, status, body))
		}
	}

	tc.logDebug("Viewer MLflow experiment get, list, create, update, and delete requests were denied\n")
	return nil
}

func (tc *scenarioConfig) createMLflowExperimentAsCurrentUser() error {
	name := fmt.Sprintf("evalhub-nonadmin-rbac-%d", time.Now().UTC().UnixNano())
	status, body, err := tc.sendMLflowRBACRequest(http.MethodPost, "/api/2.0/mlflow/experiments/create", map[string]any{"name": name})
	if err != nil {
		return tc.logError(err)
	}
	if status != http.StatusOK {
		return tc.logError(fmt.Errorf("create MLflow experiment returned HTTP %d: %s", status, body))
	}
	var response mlflowExperimentResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return tc.logError(fmt.Errorf("decode MLflow create response: %w", err))
	}
	if response.ExperimentID == "" {
		return tc.logError(fmt.Errorf("MLflow create response did not include experiment_id: %s", body))
	}
	tc.values["user_mlflow_experiment_id"] = response.ExperimentID
	tc.values["user_mlflow_experiment_name"] = name
	tc.logDebug("Created MLflow experiment %q as the current user\n", name)
	return nil
}

func (tc *scenarioConfig) currentUserCanGetCreatedMLflowExperiment() error {
	experimentID := tc.values["user_mlflow_experiment_id"]
	if experimentID == "" {
		return tc.logError(fmt.Errorf("created MLflow experiment ID is missing"))
	}
	query := url.Values{"experiment_id": []string{experimentID}}
	status, body, err := tc.sendMLflowRBACRequest(http.MethodGet, "/api/2.0/mlflow/experiments/get?"+query.Encode(), nil)
	if err != nil {
		return tc.logError(err)
	}
	if status != http.StatusOK {
		return tc.logError(fmt.Errorf("get MLflow experiment returned HTTP %d: %s", status, body))
	}
	var response mlflowExperimentResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return tc.logError(fmt.Errorf("decode MLflow get response: %w", err))
	}
	if response.Experiment.ExperimentID != experimentID || response.Experiment.Name != tc.values["user_mlflow_experiment_name"] {
		return tc.logError(fmt.Errorf("MLflow get returned experiment %q (%q), expected %q (%q)", response.Experiment.ExperimentID, response.Experiment.Name, experimentID, tc.values["user_mlflow_experiment_name"]))
	}
	return nil
}

func (tc *scenarioConfig) currentUserCanListCreatedMLflowExperiment() error {
	experimentID := tc.values["user_mlflow_experiment_id"]
	name := tc.values["user_mlflow_experiment_name"]
	if experimentID == "" || name == "" {
		return tc.logError(fmt.Errorf("created MLflow experiment ID or name is missing"))
	}
	filter := "name = '" + strings.ReplaceAll(name, "'", "\\'") + "'"
	status, body, err := tc.sendMLflowRBACRequest(http.MethodPost, "/api/2.0/mlflow/experiments/search", map[string]any{"filter": filter, "max_results": 100})
	if err != nil {
		return tc.logError(err)
	}
	if status != http.StatusOK {
		return tc.logError(fmt.Errorf("search MLflow experiments returned HTTP %d: %s", status, body))
	}
	var response mlflowExperimentListResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return tc.logError(fmt.Errorf("decode MLflow search response: %w", err))
	}
	for _, experiment := range response.Experiments {
		if experiment.ExperimentID == experimentID && experiment.Name == name {
			return nil
		}
	}
	return tc.logError(fmt.Errorf("MLflow experiment %q (%q) was not returned by search", experimentID, name))
}

func (tc *scenarioConfig) currentUserCannotUpdateCreatedMLflowExperiment() error {
	experimentID := tc.values["user_mlflow_experiment_id"]
	name := tc.values["user_mlflow_experiment_name"]
	if experimentID == "" || name == "" {
		return tc.logError(fmt.Errorf("created MLflow experiment ID or name is missing"))
	}
	// Reuse the current name so an unexpected permission grant does not rename
	// the experiment, while still asserting that update is denied.
	status, body, err := tc.sendMLflowRBACRequest(http.MethodPost, "/api/2.0/mlflow/experiments/update", map[string]any{
		"experiment_id": experimentID,
		"new_name":      name,
	})
	if err != nil {
		return tc.logError(err)
	}
	if status != http.StatusForbidden {
		return tc.logError(fmt.Errorf("update MLflow experiment returned HTTP %d, expected 403: %s", status, body))
	}
	var response struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return tc.logError(fmt.Errorf("decode MLflow update denial: %w", err))
	}
	if response.Error.Code != "PERMISSION_DENIED" {
		return tc.logError(fmt.Errorf("MLflow update returned error code %q, expected PERMISSION_DENIED: %s", response.Error.Code, body))
	}
	return nil
}

func (tc *scenarioConfig) currentUserCannotDeleteCreatedMLflowExperiment() error {
	experimentID := tc.values["user_mlflow_experiment_id"]
	if experimentID == "" {
		return tc.logError(fmt.Errorf("created MLflow experiment ID is missing"))
	}
	status, body, err := tc.sendMLflowRBACRequest(http.MethodPost, "/api/2.0/mlflow/experiments/delete", map[string]any{"experiment_id": experimentID})
	if err != nil {
		return tc.logError(err)
	}
	if status != http.StatusForbidden {
		return tc.logError(fmt.Errorf("delete MLflow experiment returned HTTP %d, expected 403: %s", status, body))
	}
	var response struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return tc.logError(fmt.Errorf("decode MLflow delete denial: %w", err))
	}
	if response.Error.Code != "PERMISSION_DENIED" {
		return tc.logError(fmt.Errorf("MLflow delete returned error code %q, expected PERMISSION_DENIED: %s", response.Error.Code, body))
	}
	return nil
}
