package mlflowclient

import (
	"strconv"
	"strings"
)

// APIError represents an error from the MLflow API
type APIError struct {
	StatusCode   int    `json:"status_code" validate:"required"`
	ResponseBody string `json:"response_body,omitempty"`
}

// Error implements the error interface
func (e *APIError) Error() string {
	sb := strings.Builder{}
	sb.WriteString("MLflow API error")
	if e.ResponseBody != "" {
		sb.WriteString(" with response body: ")
		sb.WriteString(e.ResponseBody)
	}
	sb.WriteString(" with status code: ")
	sb.WriteString(strconv.Itoa(e.StatusCode))
	return sb.String()
}

// Experiment represents an MLflow experiment
type Experiment struct {
	ExperimentID     string          `json:"experiment_id"`
	Name             string          `json:"name"`
	ArtifactLocation string          `json:"artifact_location"`
	LifecycleStage   string          `json:"lifecycle_stage"`
	LastUpdateTime   int64           `json:"last_update_time"`
	CreationTime     int64           `json:"creation_time"`
	Tags             []ExperimentTag `json:"tags"`
}

// ExperimentTag represents a tag on an experiment
type ExperimentTag struct {
	Key   string `json:"key" validate:"required"`
	Value string `json:"value" validate:"required"`
}

// CreateExperimentRequest represents a request to create an experiment
type CreateExperimentRequest struct {
	Name             string          `json:"name" validate:"required"`
	ArtifactLocation string          `json:"artifact_location,omitempty" validate:"omitempty"`
	Tags             []ExperimentTag `json:"tags,omitempty" validate:"omitempty,dive"`
}

// CreateExperimentResponse represents the response from creating an experiment
type CreateExperimentResponse struct {
	ExperimentID string `json:"experiment_id" validate:"required"`
}

// GetExperimentRequest represents a request to get an experiment
type GetExperimentRequest struct {
	ExperimentID string `json:"experiment_id" validate:"required"`
}

// GetExperimentByNameRequest represents a request to get an experiment by name
type GetExperimentByNameRequest struct {
	ExperimentName string `json:"experiment_name" validate:"required"`
}

// GetExperimentResponse represents the response from getting an experiment
type GetExperimentResponse struct {
	Experiment Experiment `json:"experiment" validate:"required"`
}
