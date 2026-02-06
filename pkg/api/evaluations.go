package api

import "time"

// State represents the evaluation state enum
type State string

const (
	StatePending         State = "pending"
	StateRunning         State = "running"
	StateCompleted       State = "completed"
	StateFailed          State = "failed"
	StateCancelled       State = "cancelled"
	StatePartiallyFailed State = "partially_failed"
)

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
	ID          string               `json:"id"`
	State       State                `json:"state" validate:"required,oneof=pending running completed failed cancelled"`
	StartedAt   *time.Time           `json:"started_at,omitempty"`
	CompletedAt *time.Time           `json:"completed_at,omitempty"`
	Message     *MessageInfo         `json:"message,omitempty"`
	Logs        *BenchmarkStatusLogs `json:"logs,omitempty"`
}

type EvaluationJobState struct {
	State   State        `json:"state" validate:"required,oneof=pending running completed failed cancelled partially_failed"`
	Message *MessageInfo `json:"message" validate:"required"`
}

// EvaluationStatus represents evaluation status
type EvaluationJobStatus struct {
	EvaluationJobState
	Benchmarks []BenchmarkStatus `json:"benchmarks,omitempty"`
}

type StatusEvent struct {
	StatusEvent *EvaluationJobStatus `json:"status_event" validate:"required"`
}

// EvaluationJobBenchmarkResult represents benchmark result in evaluation job
type EvaluationJobBenchmarkResult struct {
	ID          string         `json:"id"`
	State       State          `json:"state"`
	StartedAt   *time.Time     `json:"started_at,omitempty"`
	CompletedAt *time.Time     `json:"completed_at,omitempty"`
	Metrics     map[string]any `json:"metrics,omitempty"`
	Error       *MessageInfo   `json:"error,omitempty"`
	Artifacts   map[string]any `json:"artifacts,omitempty"`
}

// EvaluationJobResults represents results section for EvaluationJobResource
type EvaluationJobResults struct {
	TotalEvaluations     int                            `json:"total_evaluations"`
	CompletedEvaluations int                            `json:"completed_evaluations,omitempty"`
	FailedEvaluations    int                            `json:"failed_evaluations,omitempty"`
	Benchmarks           []EvaluationJobBenchmarkResult `json:"benchmarks,omitempty"`
	AggregatedMetrics    map[string]any                 `json:"aggregated_metrics,omitempty"`
	MLFlowExperimentURL  *string                        `json:"mlflow_experiment_url,omitempty"`
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
	MLFlowExperimentID string `json:"mlflow_experiment_id,omitempty"`
}

// EvaluationJobResource represents evaluation job resource response
type EvaluationJobResource struct {
	Resource EvaluationResource `json:"resource"`
	EvaluationJobConfig
	Status  *EvaluationJobStatus  `json:"status"`
	Results *EvaluationJobResults `json:"results,omitempty"`
}

// EvaluationJobResourceList represents list of evaluation job resources with pagination
type EvaluationJobResourceList struct {
	Page
	Items []EvaluationJobResource `json:"items"`
}
