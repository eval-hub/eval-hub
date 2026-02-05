package mlflowclient

import (
	"errors"
	"strconv"
	"strings"

	"github.com/eval-hub/eval-hub/pkg/api"
)

// APIError represents an error from the MLflow API
type APIError struct {
	StatusCode   int          `json:"status_code" validate:"required"`
	ResponseBody string       `json:"response_body,omitempty"`
	MLFlowError  *MLFlowError `json:"error,omitempty"`
}

type MLFlowError struct {
	ErrorCode string `json:"error_code"`
	Message   string `json:"message"`
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

func IsResourceAlreadyExistsError(err error) bool {
	apiError := &APIError{}
	if errors.As(err, &apiError) && (apiError.StatusCode == 400) {
		if apiError.MLFlowError != nil && apiError.MLFlowError.ErrorCode == "RESOURCE_ALREADY_EXISTS" {
			return true
		}
		if strings.Contains(apiError.ResponseBody, "RESOURCE_ALREADY_EXISTS") {
			return true
		}
	}
	return false
}

func IsResourceDoesNotExistError(err error) bool {
	apiError := &APIError{}
	if errors.As(err, &apiError) && (apiError.StatusCode == 404) {
		if apiError.MLFlowError != nil && apiError.MLFlowError.ErrorCode == "RESOURCE_DOES_NOT_EXIST" {
			return true
		}
		if strings.Contains(apiError.ResponseBody, "RESOURCE_DOES_NOT_EXIST") {
			return true
		}
	}
	return false
}

// Experiment represents an MLflow experiment
type Experiment struct {
	ExperimentID     string              `json:"experiment_id"`
	Name             string              `json:"name"`
	ArtifactLocation string              `json:"artifact_location"`
	LifecycleStage   string              `json:"lifecycle_stage"`
	LastUpdateTime   int64               `json:"last_update_time"`
	CreationTime     int64               `json:"creation_time"`
	Tags             []api.ExperimentTag `json:"tags"`
}

// CreateExperimentRequest represents a request to create an experiment
type CreateExperimentRequest struct {
	Name             string              `json:"name" validate:"required"`
	ArtifactLocation string              `json:"artifact_location,omitempty" validate:"omitempty"`
	Tags             []api.ExperimentTag `json:"tags,omitempty" validate:"omitempty,dive"`
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
