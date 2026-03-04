package evalhub

import "time"

type State string

const (
	StatePending   State = "pending"
	StateRunning   State = "running"
	StateCompleted State = "completed"
	StateFailed    State = "failed"
	StateCancelled State = "cancelled"
)

// DateTime is a string representation of a date and time in RFC3339 format
type DateTime string

// DateTimeToString converts a time.Time to a DateTime string
func DateTimeToString(date time.Time) DateTime {
	return DateTime(date.Format("2006-01-02T15:04:05Z07:00"))
}

// DateTimeFromString converts a DateTime string to a time.Time
func DateTimeFromString(date DateTime) (time.Time, error) {
	return time.Parse("2006-01-02T15:04:05Z07:00", string(date))
}

type MessageInfo struct {
	Message     string `json:"message"`
	MessageCode string `json:"message_code"`
}

// BenchmarkStatusEvent is used when the job runtime needs to updated the status of a benchmark
type BenchmarkStatusEvent struct {
	ProviderID     string         `json:"provider_id" validate:"required"`
	ID             string         `json:"id" validate:"required"`
	BenchmarkIndex int            `json:"benchmark_index"`
	Status         State          `json:"status" validate:"required,oneof=pending running completed failed cancelled"`
	Metrics        map[string]any `json:"metrics,omitempty"`
	Artifacts      map[string]any `json:"artifacts,omitempty"`
	ErrorMessage   *MessageInfo   `json:"error_message,omitempty"`
	StartedAt      DateTime       `json:"started_at,omitempty" validate:"omitempty,datetime=2006-01-02T15:04:05Z07:00"`
	CompletedAt    DateTime       `json:"completed_at,omitempty" validate:"omitempty,datetime=2006-01-02T15:04:05Z07:00"`
	MLFlowRunID    string         `json:"mlflow_run_id,omitempty"`
	LogsPath       string         `json:"logs_path,omitempty"`
}

type StatusEvent struct {
	BenchmarkStatusEvent *BenchmarkStatusEvent `json:"benchmark_status_event" validate:"required"`
}
