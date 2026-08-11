package features

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/server"
	pkgapi "github.com/eval-hub/eval-hub/pkg/api"

	"github.com/cucumber/godog"
)

func (tc *scenarioConfig) theServiceIsRunning(ctx context.Context) error {
	// Check that the server is actually running by sending a request to the health endpoint
	for range 20 {
		if err := tc.checkHealthEndpoint(); err != nil {
			tc.logDebug("Error checking health endpoint: %v\n", err.Error())
			time.Sleep(1 * time.Second)
		} else {
			return nil
		}
	}
	return tc.logError(fmt.Errorf("service is not running"))
}

func (tc *scenarioConfig) thereAreSystemProviders(ctx context.Context) error {
	if err := tc.iSendARequestImpl("GET", "/api/v1/evaluations/providers?scope=system&limit=100", "", "there are system providers"); err != nil {
		return err
	}
	if tc.response.StatusCode != 200 {
		return tc.logError(fmt.Errorf("expected 200 listing system providers, got %d: %s", tc.response.StatusCode, string(tc.body)))
	}

	var resp struct {
		TotalCount int `json:"total_count"`
	}
	if err := json.Unmarshal(tc.body, &resp); err != nil {
		return tc.logError(fmt.Errorf("failed to parse providers list: %w", err))
	}

	if resp.TotalCount == 0 {
		tc.logDebug("Skipping scenario: no system providers found so skipping the scenario\n")
		return godog.ErrSkip
	}

	return nil
}

func (tc *scenarioConfig) thereAreSystemCollections(ctx context.Context) error {
	if err := tc.iSendARequestImpl("GET", "/api/v1/evaluations/collections?scope=system&limit=100", "", "there are system collections"); err != nil {
		return err
	}
	if tc.response.StatusCode != 200 {
		return tc.logError(fmt.Errorf("expected 200 listing system collections, got %d: %s", tc.response.StatusCode, string(tc.body)))
	}

	var resp struct {
		TotalCount int `json:"total_count"`
		Items      []struct {
			Resource struct {
				ID string `json:"id"`
			} `json:"resource"`
			Name string `json:"name"`
		} `json:"items"`
	}
	if err := json.Unmarshal(tc.body, &resp); err != nil {
		return tc.logError(fmt.Errorf("failed to parse collections list: %w", err))
	}

	if resp.TotalCount == 0 {
		tc.logDebug("Skipping scenario: no system collections found so skipping the scenario\n")
		return godog.ErrSkip
	}

	// save the collection names for later use
	for index, item := range resp.Items {
		tc.saveValue(fmt.Sprintf("collection%d:id", index), item.Resource.ID)
		tc.saveValue(fmt.Sprintf("collection%d:name", index), item.Name)
	}

	return nil
}

func (tc *scenarioConfig) thereIsASystemCollectionWithId(ctx context.Context, id string) error {
	if err := tc.iSendARequestImpl("GET", "/api/v1/evaluations/collections/"+id, "", "there is a system collection with id "+id); err != nil {
		return err
	}
	if tc.response.StatusCode != 200 {
		tc.logDebug("Skipping scenario: system collection with id %s not found\n", id)
		return godog.ErrSkip
	}

	// save the collection id for later use
	tc.saveValue("collection:id", id)
	name, err := tc.getJsonPathValue("$.name")
	if err != nil {
		return err
	}
	nameStr, ok := name.(string)
	if !ok {
		return tc.logError(fmt.Errorf("expected name to be a string, got %T", name))
	}
	tc.saveValue("collection:name", nameStr)

	return nil
}

func (tc *scenarioConfig) theValueIsSet(ctx context.Context, name string) error {
	value, err := tc.getValue(name)
	if err != nil {
		return err
	}
	if strings.TrimSpace(value) == "" {
		return tc.logError(fmt.Errorf("value %s is not set", name))
	}
	return nil
}

func (tc *scenarioConfig) checkHealthEndpoint() error {
	if err := tc.iSendARequestImpl("GET", "/api/v1/health", "", "check health endpoint"); err != nil {
		return tc.logError(fmt.Errorf("failed to send health check request: %w for URL %s", err, tc.apiFeature.baseURL.String()))
	}
	if tc.response.StatusCode != 200 {
		return tc.logError(fmt.Errorf("expected status 200, got %d", tc.response.StatusCode))
	}

	match := "\"status\":\"healthy\""
	if !strings.Contains(string(tc.body), match) {
		return tc.logError(fmt.Errorf("expected body to contain %s, got %s", match, string(tc.body)))
	}

	return nil
}

func (tc *scenarioConfig) iSetHeaderTo(paramName, paramValue string) error {
	value, err := tc.getValue(paramValue)
	if err != nil {
		return err
	}
	tc.reqHeaders[paramName] = value
	return nil
}

func (tc *scenarioConfig) iUnsetHeader(paramName string) error {
	delete(tc.reqHeaders, paramName)
	return nil
}

func (tc *scenarioConfig) iSetTransactionIdTo(paramValue string) error {
	return tc.iSetHeaderTo(server.TRANSACTION_ID_HEADER, paramValue)
}

func (tc *scenarioConfig) iSendARequestTo(method, path string) error {
	return tc.iSendARequestToWithBody(method, path, "")
}

func (tc *scenarioConfig) setDuration(dest *time.Duration, fieldName, paramValue string) error {
	value, err := tc.getValue(paramValue)
	if err != nil {
		return err
	}
	*dest, err = time.ParseDuration(value)
	if err != nil {
		return tc.logError(fmt.Errorf("failed to parse duration %q: %w", value, err))
	}
	if *dest <= 0 {
		return tc.logError(fmt.Errorf("%s must be positive, got %q (%v)", fieldName, value, *dest))
	}
	return nil
}

func (tc *scenarioConfig) iSetWaitDeadlineTo(paramValue string) error {
	return tc.setDuration(&tc.waitDeadline, "wait deadline", paramValue)
}

func (tc *scenarioConfig) iSetWaitIntervalTo(paramValue string) error {
	return tc.setDuration(&tc.waitInterval, "wait interval", paramValue)
}

func (tc *scenarioConfig) iWaitForEvaluationJobStatus(expectedStatus string) error {
	return tc.iWaitForEvaluationJobStatusByID("", expectedStatus)
}

func (tc *scenarioConfig) iWaitForEvaluationJobStatusByID(jobIDExpr, expectedStatus string) error {
	jobID := tc.lastId
	if strings.TrimSpace(jobIDExpr) != "" && jobIDExpr != "{id}" {
		resolved, err := tc.getValue(jobIDExpr)
		if err != nil {
			return err
		}
		jobID = resolved
	}
	if jobID == "" {
		return tc.logError(fmt.Errorf("job ID is empty for %q", jobIDExpr))
	}
	prevID := tc.lastId
	tc.lastId = jobID
	defer func() { tc.lastId = prevID }()

	deadline := time.Now().Add(tc.waitDeadline)
	var lastErr error
	var lastStatus string
	for time.Now().Before(deadline) {
		if err := tc.iSendARequestImpl(http.MethodGet, "/api/v1/evaluations/jobs/{id}", "", "wait for evaluation job status"); err != nil {
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
			} else if status == expectedStatus {
				return nil
			} else {
				// Fail fast when the job has reached any terminal state other than the expected one.
				if pkgapi.OverallState(status).IsTerminalState() {
					// Get additional error context from the response for better diagnostics
					message, _ := tc.getJsonPath("$.status.message.message")
					if message != "" {
						return tc.logError(fmt.Errorf("evaluation job reached terminal state %q (expected %q): %s", status, expectedStatus, message))
					}
					return tc.logError(fmt.Errorf("evaluation job reached terminal state %q (expected %q)", status, expectedStatus))
				}
				// we should not do this because it will be logged as an error
				// lastErr = fmt.Errorf("expected status %q but got %q", expectedStatus, status)
			}
		} else if tc.response != nil {
			lastErr = tc.logError(fmt.Errorf("unexpected response status %d", tc.response.StatusCode))
		}
		time.Sleep(tc.waitInterval)
	}
	if lastErr != nil {
		return tc.logError(lastErr)
	}
	return tc.logError(fmt.Errorf("timed out after %v waiting for status %q, last status: %q", tc.waitDeadline, expectedStatus, lastStatus))
}

func (tc *scenarioConfig) iSendARequestToWithInlineBody(method, path string, body *godog.DocString) error {
	if body == nil {
		return tc.logError(fmt.Errorf("inline body is missing"))
	}
	return tc.iSendARequestToWithBody(method, path, body.Content)
}

func (tc *scenarioConfig) iSendARequestToWithBody(method, path, body string) error {
	return tc.iSendARequestImpl(method, path, body, "")
}

func (tc *scenarioConfig) iSendARequestImpl(method, path, body, caller string) error {
	endpoint, err := tc.getEndpoint(path)
	if err != nil {
		return err
	}
	tc.lastURL = endpoint
	tc.lastMethod = method
	entity, err := tc.getRequestBody(body)
	if err != nil {
		return err
	}
	if caller != "" {
		tc.logDebug("Sending %s request to %s by %s with body %s\n", method, endpoint, caller, body)
	} else {
		tc.logDebug("Sending %s request to %s with body %s\n", method, endpoint, body)
	}
	req, err := http.NewRequest(method, endpoint, entity)
	if err != nil {
		tc.logDebug("Failed to create request: %v\n", err)
		return err
	}
	scrapeMetrics := isMetricsScrapePath(path)
	if !scrapeMetrics {
		if authToken := os.Getenv("AUTH_TOKEN"); authToken != "" {
			req.Header.Set("Authorization", "Bearer "+authToken)
		}
		if tenant := os.Getenv("X_TENANT"); tenant != "" {
			req.Header.Set("X-Tenant", tenant)
		}
	}

	for k, v := range tc.reqHeaders {
		req.Header.Set(k, v)
	}

	tc.response, err = tc.apiFeature.client.Do(req)
	if err != nil {
		tc.logDebug("Failed to send request: %v\n", err)
		return err
	}

	defer func() {
		// we do this for now as request ids are supposed to be unique per request
		_ = tc.iUnsetHeader(server.TRANSACTION_ID_HEADER)
	}()

	defer func() { _ = tc.response.Body.Close() }()

	tc.body, err = io.ReadAll(tc.response.Body)
	if err != nil {
		return err
	}

	if len(tc.body) > 0 && len(tc.body) < 1024*5 {
		tc.logDebug("Response status %d for %s %s with body %s\n", tc.response.StatusCode, method, endpoint, string(tc.body))
	} else {
		tc.logDebug("Response status %d for %s %s\n", tc.response.StatusCode, method, endpoint)
	}

	// capture resource id for create (evaluation job or collection)
	if method == http.MethodPost && (tc.response.StatusCode == http.StatusAccepted || tc.response.StatusCode == http.StatusCreated) {
		_, assetName, _, err := tc.getAssetDetails(endpoint)
		if err != nil {
			return err
		}
		if assetName != "" {
			tc.lastId, err = tc.extractId(tc.body)
			if err != nil {
				return err
			}
			if tc.lastId == "" {
				return tc.logError(fmt.Errorf("response does not contain an ID in response %s", string(tc.body)))
			}
			tc.addAsset(assetName, tc.lastId)
			tc.values["id"] = tc.lastId
		}
	}

	if method == http.MethodDelete {
		_, assetName, id, err := tc.getAssetDetails(endpoint)
		if err != nil {
			return err
		}
		if assetName != "" {
			if id == "" {
				return tc.logError(fmt.Errorf("no ID found in path %s", endpoint))
			}
			parsedURL, err := url.Parse(endpoint)
			if err != nil {
				return tc.logError(fmt.Errorf("failed to parse endpoint %s: %w", endpoint, err))
			}
			if parsedURL.Query().Get("hard_delete") == "true" {
				tc.removeAsset(assetName, id)
			}
		}
	}

	return nil
}

func (tc *scenarioConfig) requireMetricsURLForRemoteServer(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
	// Not a @metrics scenario.
	// Input: any scenario without the @metrics tag.
	// Behavior: no-op; other features are unaffected by METRICS_URL requirements.
	if !scenarioHasTag(scenarioTagNames(sc), "metrics") {
		return ctx, nil
	}

	// Local embedded-server mode.
	// Input: SERVER_URL unset; METRICS_URL optional (defaults via resolveMetricsBaseURL).
	// Behavior: run the scenario; /metrics is served on the main API router in local mode.
	if strings.TrimSpace(os.Getenv("SERVER_URL")) == "" {
		return ctx, nil
	}

	// Remote/cluster mode with METRICS_URL configured.
	// Input: SERVER_URL=https://evalhub.example.com, METRICS_URL=http://evalhub-metrics.<ns>.svc:8081.
	// Behavior: run the scenario; scrape requests use the dedicated metrics port.
	if strings.TrimSpace(os.Getenv(envMetricsURL)) != "" {
		return ctx, nil
	}

	// Remote/cluster mode without METRICS_URL.
	// Input: SERVER_URL set, METRICS_URL unset.
	// Behavior: skip the scenario (scraping via SERVER_URL would hit kube-rbac-proxy and fail with 403).
	tc.logDebug(
		"Skipping scenario: METRICS_URL is required when SERVER_URL is set (metrics are served on a separate port, not through kube-rbac-proxy)\n",
	)
	return ctx, godog.ErrSkip
}

// ociIsConfigured checks if OCI environment variables are configured and skips if missing
func (tc *scenarioConfig) ociIsConfigured() error {
	// OCI configuration present - continue the scenario
	if ociConfiguration == ociConfigPresent {
		tc.logDebug("OCI configuration present - scenario will run\n")
		return nil
	}

	// OCI configuration missing - skip scenario with clear message
	tc.logDebug(
		"Skipping scenario: OCI_REGISTRY, OCI_REPOSITORY, and OCI_SECRET_NAME environment variables are required\n",
	)
	return godog.ErrSkip
}

func (tc *scenarioConfig) saveScenarioName(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
	tc.scenarioName = sc.Name
	tc.jsonnetQueueEnabled = nil
	return ctx, nil
}

func (tc *scenarioConfig) queueIsEnabledForJsonnetPayloads() error {
	queueOn := true
	tc.jsonnetQueueEnabled = &queueOn
	logDebug("Queue enabled for jsonnet payloads\n")
	return nil
}

func (tc *scenarioConfig) assetCleanup(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
	for assetName, ids := range tc.assets {
		clonedIDs := slices.Clone(ids)
		hardDelete := false
		url := assetName
		switch assetName {
		case "evaluations":
			url = "evaluations/jobs"
			hardDelete = true
		case "jobs":
			url = "evaluations/jobs"
			hardDelete = true
		case "collections":
			url = "evaluations/collections"
		case "providers":
			url = "evaluations/providers"
		}
		for _, id := range clonedIDs {
			var path string
			if hardDelete {
				path = fmt.Sprintf("/api/v1/%s/%s?hard_delete=true", url, id)
			} else {
				path = fmt.Sprintf("/api/v1/%s/%s", url, id)
			}
			err := tc.iSendARequestImpl("DELETE", path, "", "asset cleanup")
			if err != nil {
				return ctx, tc.logError(fmt.Errorf("failed to delete asset %s with id '%s': %w", assetName, id, err))
			}
			err = tc.theResponseStatusShouldBe(204)
			if err != nil {
				_ = tc.logError(fmt.Errorf("failed to delete asset %s expected status %d but got %d: %w", tc.lastURL, 204, tc.response.StatusCode, err))
				// return ctx, err
			} else {
				tc.logDebug("Deleted asset %s with status %d\n", path, tc.response.StatusCode)
			}
		}
	}
	tc.assets = nil
	return ctx, nil
}

func (tc *scenarioConfig) theModelEndpointIsReachable() error {
	switch modelEndpointConnectivity {
	case modelEndpointUnreachable:
		logDebug("Model endpoint is not reachable, skipping evaluation job scenario %s\n", tc.scenarioName)
		return godog.ErrSkip
	case modelEndpointUnchecked, modelEndpointReachable:
		return nil
	default:
		return nil
	}
}
