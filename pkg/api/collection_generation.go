package api

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

	// Model identifies the generator model and its endpoint. Generation requires
	// both the model name and URL; credentials, when needed, are referenced through
	// the model's auth.secret_ref field.
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
