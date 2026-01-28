package api

import "time"

// State represents the evaluation state enum
type State string

const (
	StatePending   State = "pending"
	StateRunning   State = "running"
	StateCompleted State = "completed"
	StateFailed    State = "failed"
	StateCancelled State = "cancelled"
)

// ModelRef represents model specification for evaluation requests
type ModelRef struct {
	URL  string `json:"url"`
	Name string `json:"name"`
}

// BenchmarkConfig represents a reference to a benchmark
type BenchmarkConfig struct {
	Ref
	Limit      *int           `json:"limit,omitempty"`
	Parameters map[string]any `json:"parameters,omitempty"`
}

// ExperimentConfig represents configuration for MLFlow experiment tracking
type ExperimentConfig struct {
	Name string            `json:"name"`
	Tags map[string]string `json:"tags,omitempty"`
}

// BenchmarkStatusLogs represents logs information for benchmark status
type BenchmarkStatusLogs struct {
	Path string `json:"path,omitempty"`
}

// BenchmarkStatus represents status of individual benchmark in evaluation
type BenchmarkStatus struct {
	Name        string               `json:"name"`
	State       State                `json:"state"`
	StartedAt   *time.Time           `json:"started_at,omitempty"`
	CompletedAt *time.Time           `json:"completed_at,omitempty"`
	Message     string               `json:"message,omitempty"`
	Logs        *BenchmarkStatusLogs `json:"logs,omitempty"`
}

type EvaluationJobState struct {
	State   State  `json:"state"`
	Message string `json:"message"`
}

// EvaluationStatus represents evaluation status
type EvaluationJobStatus struct {
	EvaluationJobState
	Benchmarks []BenchmarkStatus `json:"benchmarks,omitempty"`
}

// EvaluationJobBenchmarkResult represents benchmark result in evaluation job
type EvaluationJobBenchmarkResult struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	State       State          `json:"state"`
	StartedAt   *time.Time     `json:"started_at,omitempty"`
	CompletedAt *time.Time     `json:"completed_at,omitempty"`
	Metrics     map[string]any `json:"metrics,omitempty"`
	Error       *string        `json:"error,omitempty"`
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
	Benchmarks     []BenchmarkConfig `json:"benchmarks"`
	Collection     Ref               `json:"collection"`
	Experiment     ExperimentConfig  `json:"experiment"`
	TimeoutMinutes *int              `json:"timeout_minutes,omitempty"`
	RetryAttempts  *int              `json:"retry_attempts,omitempty"`
	CallbackURL    *string           `json:"callback_url,omitempty"`
}

// EvaluationJobResource represents evaluation job resource response
type EvaluationJobResource struct {
	Resource
	EvaluationJobConfig
	Status  EvaluationJobStatus   `json:"status"`
	Results *EvaluationJobResults `json:"results,omitempty"`
}

// EvaluationJobResourceList represents list of evaluation job resources with pagination
type EvaluationJobResourceList struct {
	Page
	Items []EvaluationJobResource `json:"items"`
}
