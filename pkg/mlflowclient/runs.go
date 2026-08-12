package mlflowclient

import (
	"fmt"
	"net/http"
	"time"
)

const (
	endpointRunsCreate = apiBasePath + "/runs/create"
	endpointRunsSearch = apiBasePath + "/runs/search"
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

// SearchRunsRequest is the request body for runs/search.
type SearchRunsRequest struct {
	ExperimentIDs []string `json:"experiment_ids"`
	Filter        string   `json:"filter,omitempty"`
}

// CreateRunResponse is the response body from runs/create.
type CreateRunResponse struct {
	Run Run `json:"run"`
}

// SearchRunsResponse is the response body from runs/search.
type SearchRunsResponse struct {
	Runs []Run `json:"runs"`
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

// SearchRuns searches for runs matching a filter in the given experiments.
func (c *Client) SearchRuns(req *SearchRunsRequest) (*SearchRunsResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("search runs request is nil")
	}

	respBody, err := c.doRequest(http.MethodPost, endpointRunsSearch, req)
	if err != nil {
		return nil, err
	}
	return unmarshalResponse[SearchRunsResponse](respBody)
}
