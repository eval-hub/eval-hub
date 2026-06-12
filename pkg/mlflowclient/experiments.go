package mlflowclient

import (
	"fmt"
	"strings"
)

// GetOrCreateExperiment returns an active experiment by name, creating it when missing.
// Idempotent when concurrent callers race to create the same experiment.
func (c *Client) GetOrCreateExperiment(req *CreateExperimentRequest) (*GetExperimentResponse, error) {
	if c == nil {
		return nil, fmt.Errorf("mlflow client is nil")
	}
	if req == nil || strings.TrimSpace(req.Name) == "" {
		return nil, fmt.Errorf("create experiment request is nil or missing name")
	}
	name := strings.TrimSpace(req.Name)
	normalizedReq := *req
	normalizedReq.Name = name

	resp, err := c.GetExperimentByName(name)
	if err == nil {
		if isActiveExperiment(resp) {
			return resp, nil
		}
	} else if !IsResourceDoesNotExistError(err) {
		return nil, err
	}

	_, createErr := c.CreateExperiment(&normalizedReq)
	if createErr != nil && !IsResourceAlreadyExistsError(createErr) {
		return nil, createErr
	}

	resp, err = c.GetExperimentByName(name)
	if err != nil {
		return nil, err
	}
	if !isActiveExperiment(resp) {
		return nil, fmt.Errorf("experiment %q is not active after create", name)
	}
	return resp, nil
}

func isActiveExperiment(resp *GetExperimentResponse) bool {
	return resp != nil &&
		resp.Experiment.LifecycleStage == "active" &&
		resp.Experiment.ExperimentID != ""
}
