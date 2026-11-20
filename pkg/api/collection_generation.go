package api

import "time"

const (
	// MaxCollectionGenerationGoalLength is the maximum evaluation_goal length in characters.
	MaxCollectionGenerationGoalLength = 10_000
	// MaxCollectionGenerationBenchmarks is the maximum number of benchmarks a proposal may select.
	MaxCollectionGenerationBenchmarks = 100
	// DefaultCollectionGenerationBenchmarks is used when max_benchmarks is omitted.
	DefaultCollectionGenerationBenchmarks = 12
)

// CollectionGenerationRequest contains the goal and options for a collection
// generation request. The service normalizes the optional settings to their
// documented defaults before returning the request in a generation resource.
type CollectionGenerationRequest struct {
	// EvaluationGoal is a non-blank natural-language goal, up to 10,000 characters.
	EvaluationGoal string `json:"evaluation_goal"`

	// CollectionName, when supplied, is the exact name requested for the
	// proposed collection. The runner generates a name when it is omitted.
	CollectionName string `json:"collection_name,omitempty"`

	// CollectionDescription, when supplied, is the exact description requested
	// for the proposed collection. The runner generates one when it is omitted.
	CollectionDescription string `json:"collection_description,omitempty"`

	// Model identifies the generator model and its endpoint. Generation requires
	// both the model name and URL; credentials, when needed, are referenced through
	// the model's auth.secret_ref field and must not be included in Parameters.
	Model ModelRef `json:"model"`

	// ProviderFilter optionally limits benchmark discovery to comma-separated
	// provider IDs. An empty or omitted value searches all visible providers.
	ProviderFilter string `json:"provider_filter,omitempty"`

	// MaxBenchmarks is the maximum number of benchmarks to include. It defaults
	// to 12 and must be between 1 and 100.
	MaxBenchmarks int `json:"max_benchmarks,omitempty"`

	// Strictness controls how conservatively benchmarks are selected and
	// calibrated. It defaults to moderate; accepted values are lenient, moderate,
	// and strict.
	Strictness string `json:"strictness,omitempty"`

	// GenerationEffort selects a service-capped generation budget. It defaults
	// to standard; accepted values are low, standard, and high.
	GenerationEffort string `json:"generation_effort,omitempty"`
}

// CollectionGenerationProgressEvent is the runner-authored event payload. The server supplies
// timestamps when it accepts the event; timestamps from a worker are never trusted.
type CollectionGenerationProgressEvent struct {
	Sequence            uint64 `json:"sequence" validate:"required,min=1"`
	Phase               string `json:"phase" validate:"required,oneof=guardrails catalog_discovery model_generation validation finalization"`
	LastCompletedAction string `json:"last_completed_action,omitempty" validate:"omitempty,oneof=guardrails_check search_benchmarks get_provider_details list_collections validate_proposal calibrate_proposal finalize_proposal"`
}

// CollectionGenerationProgress is durable server-timestamped progress exposed while a generation runs.
type CollectionGenerationProgress struct {
	Sequence            uint64                              `json:"sequence,omitempty"`
	Phase               string                              `json:"phase"`
	UpdatedAt           time.Time                           `json:"updated_at"`
	LastCompletedAction *CollectionGenerationProgressAction `json:"last_completed_action,omitempty"`
}

// CollectionGenerationProgressAction identifies the last completed allowlisted runner action.
type CollectionGenerationProgressAction struct {
	Name        string    `json:"name"`
	CompletedAt time.Time `json:"completed_at"`
}

// CollectionGenerationAction is a bounded, redacted action summary in a completed proposal.
type CollectionGenerationAction struct {
	Name    string `json:"name" validate:"required,min=1,max=64"`
	Outcome string `json:"outcome" validate:"required,oneof=success error"`
	Summary string `json:"summary" validate:"required,min=1,max=300"`
}

// CollectionGenerationBenchmarkReason provides a reason for a benchmark selection decision.
type CollectionGenerationBenchmarkReason struct {
	ProviderID string `json:"provider_id" validate:"required,min=1"`
	ID         string `json:"id" validate:"required,min=1"`
	Reason     string `json:"reason" validate:"required,min=1,max=300"`
}

// CollectionGenerationExplanation explains selected and rejected catalog candidates.
type CollectionGenerationExplanation struct {
	SelectedBenchmarks []CollectionGenerationBenchmarkReason `json:"selected_benchmarks" validate:"required,min=1,dive"`
	RejectedBenchmarks []CollectionGenerationBenchmarkReason `json:"rejected_benchmarks" validate:"max=5,dive"`
}

// CollectionGenerationResult is a dry-run collection proposal; it never creates the collection.
type CollectionGenerationResult struct {
	Collection       CollectionConfig                `json:"collection" validate:"required"`
	YAML             string                          `json:"yaml" validate:"required,min=1"`
	Rationale        string                          `json:"rationale" validate:"required,min=1"`
	Explanation      CollectionGenerationExplanation `json:"explanation" validate:"required"`
	Actions          []CollectionGenerationAction    `json:"actions" validate:"max=50,dive"`
	ActionsTruncated bool                            `json:"actions_truncated,omitempty"`
}

// CollectionGenerationError is a sanitized terminal failure that is safe to return to a tenant.
type CollectionGenerationError struct {
	Code     string `json:"code" validate:"required,oneof=guardrails_rejected guardrails_unavailable model_failed tool_failed validation_failed no_relevant_benchmarks timeout launch_failed workload_failed"`
	Message  string `json:"message" validate:"required,min=1"`
	Category string `json:"category,omitempty" validate:"omitempty,oneof=prompt_injection exfiltration topic_relevance unsupported_capability"`
}
