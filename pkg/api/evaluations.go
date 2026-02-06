package api

import (
	"fmt"
	"time"
)

// State represents the evaluation state enum
type State string

const (
	StatePending   State = "pending"
	StateRunning   State = "running"
	StateCompleted State = "completed"
	StateFailed    State = "failed"
	StateCancelled State = "cancelled"
)

type OverallState string

const (
	OverallStatePending         OverallState = OverallState(StatePending)
	OverallStateRunning         OverallState = OverallState(StateRunning)
	OverallStateCompleted       OverallState = OverallState(StateCompleted)
	OverallStateFailed          OverallState = OverallState(StateFailed)
	OverallStateCancelled       OverallState = OverallState(StateCancelled)
	OverallStatePartiallyFailed OverallState = "partially_failed"
)

func (o OverallState) String() string {
	return string(o)
}

func GetOverallState(s string) (OverallState, error) {
	switch s {
	case string(OverallStatePending):
		return OverallStatePending, nil
	case string(OverallStateRunning):
		return OverallStateRunning, nil
	case string(OverallStateCompleted):
		return OverallStateCompleted, nil
	case string(OverallStateFailed):
		return OverallStateFailed, nil
	case string(OverallStateCancelled):
		return OverallStateCancelled, nil
	case string(OverallStatePartiallyFailed):
		return OverallStatePartiallyFailed, nil
	default:
		return OverallState(s), fmt.Errorf("invalid overall state: %s", s)
	}
}

// ModelRef represents model specification for evaluation requests
type ModelRef struct {
	URL  string `json:"url" validate:"required"`
	Name string `json:"name" validate:"required"`
}

// MessageInfo represents a message from a downstream service
type MessageInfo struct {
	Message     string `json:"message"`
	MessageCode string `json:"message_code"`
}

// BenchmarkConfig represents a reference to a benchmark
type BenchmarkConfig struct {
	Ref
	ProviderID string         `json:"provider_id"`
	Parameters map[string]any `json:"parameters,omitempty"`
}

// ExperimentTag represents a tag on an experiment
type ExperimentTag struct {
	Key   string `json:"key" validate:"required,max=250"`    // Keys can be up to 250 bytes in size (not characters)
	Value string `json:"value" validate:"required,max=5000"` // Values can be up to 5000 bytes in size (not characters)
}

// ExperimentConfig represents configuration for MLFlow experiment tracking
type ExperimentConfig struct {
	Name             string          `json:"name,omitempty"`
	Tags             []ExperimentTag `json:"tags,omitempty" validate:"omitempty,max=20,dive"`
	ArtifactLocation string          `json:"artifact_location,omitempty"`
}

// BenchmarkStatusLogs represents logs information for benchmark status
type BenchmarkStatusLogs struct {
	Path string `json:"path,omitempty"`
}

// BenchmarkStatus represents status of individual benchmark in evaluation
type BenchmarkStatus struct {
	ProviderID      string         `json:"provider_id"`
	ID              string         `json:"id"`
	Status          State          `json:"status,omitempty"`
	Metrics         map[string]any `json:"metrics,omitempty"`
	Artifacts       map[string]any `json:"artifacts,omitempty"`
	ErrorMessage    *MessageInfo   `json:"error_message,omitempty"`
	StartedAt       *time.Time     `json:"started_at,omitempty"`
	CompletedAt     *time.Time     `json:"completed_at,omitempty"`
	DurationSeconds int64          `json:"duration_seconds,omitempty"`
	MLFlowRunID     string         `json:"mlflow_run_id,omitempty"`
}

type EvaluationJobState struct {
	State   OverallState `json:"state" validate:"required,oneof=pending running completed failed cancelled partially_failed"`
	Message *MessageInfo `json:"message" validate:"required"`
}

type StatusEvent struct {
	BenchmarkStatusEvent *BenchmarkStatus `json:"benchmark_status_event" validate:"required"`
}

// EvaluationJobResults represents results section for EvaluationJobResource
type EvaluationJobResults struct {
	TotalEvaluations     int               `json:"total_evaluations"`
	CompletedEvaluations int               `json:"completed_evaluations,omitempty"`
	FailedEvaluations    int               `json:"failed_evaluations,omitempty"`
	Benchmarks           []BenchmarkStatus `json:"benchmarks,omitempty" validate:"omitempty,dive"`
	AggregatedMetrics    map[string]any    `json:"aggregated_metrics,omitempty"`
	MLFlowExperimentURL  *string           `json:"mlflow_experiment_url,omitempty"`
}

// EvaluationJobConfig represents evaluation job request schema
type EvaluationJobConfig struct {
	Model          ModelRef          `json:"model" validate:"required"`
	Benchmarks     []BenchmarkConfig `json:"benchmarks" validate:"required,min=1,dive"`
	Collection     Ref               `json:"collection"`
	Experiment     *ExperimentConfig `json:"experiment,omitempty"`
	TimeoutMinutes *int              `json:"timeout_minutes,omitempty"`
	RetryAttempts  *int              `json:"retry_attempts,omitempty"`
}

type EvaluationResource struct {
	Resource
	MLFlowExperimentID string       `json:"mlflow_experiment_id,omitempty"`
	Status             OverallState `json:"status"`
	Message            *MessageInfo `json:"message,omitempty"`
}

// EvaluationJobResource represents evaluation job resource response
type EvaluationJobResource struct {
	Resource EvaluationResource `json:"resource"`
	EvaluationJobConfig
	Results *EvaluationJobResults `json:"results,omitempty"`
}

// EvaluationJobResourceList represents list of evaluation job resources with pagination
type EvaluationJobResourceList struct {
	Page
	Items []EvaluationJobResource `json:"items"`
}
