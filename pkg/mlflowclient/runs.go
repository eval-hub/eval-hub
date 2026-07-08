package mlflowclient

import (
	"fmt"
	"net/http"
	"time"
)

const (
	endpointRunsCreate = apiBasePath + "/runs/create"
)

// RunTag is a key-value tag on an MLflow run.
type RunTag struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// RunInfo contains run metadata returned by the MLflow API.
type RunInfo struct {
	RunID        string `json:"run_id"`
	ExperimentID string `json:"experiment_id"`
	RunName      string `json:"run_name,omitempty"`
}

// Run is an MLflow run returned by the REST API.
type Run struct {
	Info RunInfo `json:"info"`
}

// CreateRunRequest is the request body for runs/create.
type CreateRunRequest struct {
	ExperimentID string   `json:"experiment_id"`
	RunName      string   `json:"run_name,omitempty"`
	StartTime    int64    `json:"start_time,omitempty"`
	Tags         []RunTag `json:"tags,omitempty"`
}

// CreateRunResponse is the response body from runs/create.
type CreateRunResponse struct {
	Run Run `json:"run"`
}

// CreateRun creates a new run in an experiment.
func (c *Client) CreateRun(req *CreateRunRequest) (*CreateRunResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("create run request is nil")
	}
	if req.StartTime == 0 {
		req.StartTime = time.Now().UnixMilli()
	}
	respBody, err := c.doRequest(http.MethodPost, endpointRunsCreate, req)
	if err != nil {
		return nil, err
	}
	return unmarshalResponse[CreateRunResponse](respBody)
}
