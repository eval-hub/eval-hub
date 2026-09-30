package mlflowclient

import (
	"fmt"
	"strings"
)

// GetOrCreateExperiment returns an active experiment by name, creating it when missing.
// Idempotent when concurrent callers race to create the same experiment. Delegates to
// mlflow-go's Tracking().GetOrCreateExperiment, which performs the get/create/get
// sequence and handles the RESOURCE_ALREADY_EXISTS race.
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

	t, err := c.tracking()
	if err != nil {
		return nil, err
	}
	exp, err := t.GetOrCreateExperiment(c.Context(), name, createExperimentOptions(&normalizedReq)...)
	if err != nil {
		return nil, mapError(err)
	}
	return &GetExperimentResponse{Experiment: experimentFromSDK(exp)}, nil
}
