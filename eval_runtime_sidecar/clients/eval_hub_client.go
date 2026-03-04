package clients

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/eval-hub/eval-hub/eval_runtime_sidecar/api/eval_hub"
)

const eventsPath = "/api/v1/evaluations/jobs/%s/events"

// EvalHubClient posts status events to the eval-hub service REST API.
type EvalHubClient struct {
	BaseURL    string
	HTTPClient *http.Client
}

// NewEvalHubClient returns a client that posts to the given base URL (e.g. "https://eval.example.com").
// If httpClient is nil, http.DefaultClient is used.
func NewEvalHubClient(baseURL string, httpClient *http.Client) *EvalHubClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &EvalHubClient{BaseURL: baseURL, HTTPClient: httpClient}
}

// PostEvent sends a status event for the given job ID.
// On success the server returns 204 No Content. Non-2xx responses return an error.
func (c *EvalHubClient) PostEvent(jobID string, event *evalhub.StatusEvent) error {
	if jobID == "" {
		return fmt.Errorf("job id is required")
	}
	if event == nil || event.BenchmarkStatusEvent == nil {
		return fmt.Errorf("status event with benchmark_status_event is required")
	}

	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	url := c.BaseURL + fmt.Sprintf(eventsPath, jobID)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("post event: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("post event: unexpected status %d", resp.StatusCode)
	}
	return nil
}
