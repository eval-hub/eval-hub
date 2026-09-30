package mlflowclient

import (
	"fmt"
	"time"

	"github.com/opendatahub-io/mlflow-go/mlflow/tracking"
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

func runInfoFromSDK(info tracking.RunInfo) RunInfo {
	return RunInfo{
		RunID:        info.RunID,
		ExperimentID: info.ExperimentID,
		RunName:      info.RunName,
	}
}

// CreateRun creates a new run in an experiment.
func (c *Client) CreateRun(req *CreateRunRequest) (*CreateRunResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("create run request is nil")
	}
	t, err := c.tracking()
	if err != nil {
		return nil, err
	}

	// Preserve the historical eval-hub behavior of stamping the run start time
	// when the caller does not supply one; mlflow-go leaves it unset otherwise.
	startTime := req.StartTime
	if startTime == 0 {
		startTime = time.Now().UnixMilli()
	}

	opts := make([]tracking.CreateRunOption, 0, 3)
	if req.RunName != "" {
		opts = append(opts, tracking.WithRunName(req.RunName))
	}
	if len(req.Tags) > 0 {
		tags := make(map[string]string, len(req.Tags))
		for _, tag := range req.Tags {
			tags[tag.Key] = tag.Value
		}
		opts = append(opts, tracking.WithRunTags(tags))
	}
	opts = append(opts, tracking.WithStartTime(time.UnixMilli(startTime)))

	run, err := t.CreateRun(c.Context(), req.ExperimentID, opts...)
	if err != nil {
		return nil, mapError(err)
	}
	if run == nil {
		return nil, fmt.Errorf("mlflow create run response missing run")
	}
	return &CreateRunResponse{Run: Run{Info: runInfoFromSDK(run.Info)}}, nil
}

// SearchRuns searches for runs matching a filter in the given experiments.
func (c *Client) SearchRuns(req *SearchRunsRequest) (*SearchRunsResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("search runs request is nil")
	}
	t, err := c.tracking()
	if err != nil {
		return nil, err
	}

	var opts []tracking.SearchRunsOption
	if req.Filter != "" {
		opts = append(opts, tracking.WithRunsFilter(req.Filter))
	}

	result, err := t.SearchRuns(c.Context(), req.ExperimentIDs, opts...)
	if err != nil {
		return nil, mapError(err)
	}
	resp := &SearchRunsResponse{}
	if result != nil {
		resp.Runs = make([]Run, 0, len(result.Runs))
		for _, run := range result.Runs {
			resp.Runs = append(resp.Runs, Run{Info: runInfoFromSDK(run.Info)})
		}
	}
	return resp, nil
}
