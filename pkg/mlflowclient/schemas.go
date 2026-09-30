package mlflowclient

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	mlflow "github.com/opendatahub-io/mlflow-go/mlflow"
	"github.com/opendatahub-io/mlflow-go/mlflow/tracking"

	"github.com/eval-hub/eval-hub/pkg/api"
)

// APIError represents an error from the MLflow API. It preserves the historical
// eval-hub shape; errors from mlflow-go are mapped onto it by mapError so the
// IsResource* predicates keep working.
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

// mapError converts an mlflow-go error into the eval-hub *APIError shape when it
// carries API status information, preserving status code and MLflow error code so
// IsResourceAlreadyExistsError / IsResourceDoesNotExistError continue to work.
// Non-API errors (network failures, validation errors) are returned unchanged.
func mapError(err error) error {
	if err == nil {
		return nil
	}
	var apiErr *mlflow.APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	return &APIError{
		StatusCode: apiErr.StatusCode,
		// Preserve the raw error code in the body too so the substring
		// fallbacks in the IsResource* predicates keep working.
		ResponseBody: fmt.Sprintf("{\"error_code\":%q,\"message\":%q}", apiErr.Code, apiErr.Message),
		MLFlowError: &MLFlowError{
			ErrorCode: apiErr.Code,
			Message:   apiErr.Message,
		},
	}
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

// Workspace is an MLflow workspace (MLflow 3.10+).
type Workspace struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// GetWorkspaceResponse is the JSON body from GET /api/3.0/mlflow/workspaces/{name}.
type GetWorkspaceResponse struct {
	Workspace Workspace `json:"workspace"`
}

// CreateWorkspaceRequest is the JSON body for POST /api/3.0/mlflow/workspaces.
type CreateWorkspaceRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// createExperimentOptions maps an eval-hub CreateExperimentRequest onto the
// mlflow-go option-function API.
func createExperimentOptions(req *CreateExperimentRequest) []tracking.CreateExperimentOption {
	if req == nil {
		return nil
	}
	opts := make([]tracking.CreateExperimentOption, 0, 2)
	if strings.TrimSpace(req.ArtifactLocation) != "" {
		opts = append(opts, tracking.WithArtifactLocation(req.ArtifactLocation))
	}
	if len(req.Tags) > 0 {
		opts = append(opts, tracking.WithExperimentTags(experimentTagsToMap(req.Tags)))
	}
	return opts
}

func experimentTagsToMap(tags []api.ExperimentTag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	m := make(map[string]string, len(tags))
	for _, t := range tags {
		m[t.Key] = t.Value
	}
	return m
}

func experimentTagsFromMap(m map[string]string) []api.ExperimentTag {
	if len(m) == 0 {
		return nil
	}
	tags := make([]api.ExperimentTag, 0, len(m))
	for k, v := range m {
		tags = append(tags, api.ExperimentTag{Key: k, Value: v})
	}
	return tags
}

// experimentFromSDK converts an mlflow-go experiment into the eval-hub shape.
func experimentFromSDK(exp *tracking.Experiment) Experiment {
	if exp == nil {
		return Experiment{}
	}
	return Experiment{
		ExperimentID:     exp.ID,
		Name:             exp.Name,
		ArtifactLocation: exp.ArtifactLocation,
		LifecycleStage:   exp.LifecycleStage,
		CreationTime:     exp.CreationTime.UnixMilli(),
		LastUpdateTime:   exp.LastUpdateTime.UnixMilli(),
		Tags:             experimentTagsFromMap(exp.Tags),
	}
}
