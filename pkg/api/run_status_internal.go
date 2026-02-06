package api

import "time"

type RunStatusInternal struct {
	StatusEvent RunStatusEvent `json:"status_event"`
}

type RunStatusEvent struct {
	ProviderID      string         `json:"provider_id"`
	BenchmarkID     string         `json:"benchmark_id"`
	Status          State          `json:"status,omitempty"`
	Metrics         map[string]any `json:"metrics,omitempty"`
	Artifacts       map[string]any `json:"artifacts,omitempty"`
	ErrorMessage    *MessageInfo   `json:"error_message,omitempty"`
	StartedAt       *time.Time     `json:"started_at,omitempty"`
	CompletedAt     *time.Time     `json:"completed_at,omitempty"`
	DurationSeconds int64          `json:"duration_seconds,omitempty"`
	MLFlowRunID     string         `json:"mlflow_run_id,omitempty"`
}
