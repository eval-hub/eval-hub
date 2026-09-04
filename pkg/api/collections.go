package api

// CollectionBenchmarkConfig describes a benchmark entry in a collection. The url field is set by the server on read when known.
type CollectionBenchmarkConfig struct {
	Ref          `mapstructure:",squash"`
	ProviderID   string         `mapstructure:"provider_id" json:"provider_id" validate:"required"`
	URL          string         `mapstructure:"url,omitempty" json:"url,omitempty"`
	Weight       float32        `mapstructure:"weight" json:"weight,omitempty" validate:"omitempty,min=0"`
	PrimaryScore *PrimaryScore  `mapstructure:"primary_score" json:"primary_score,omitempty"`
	PassCriteria *PassCriteria  `mapstructure:"pass_criteria" json:"pass_criteria,omitempty"`
	Parameters   map[string]any `mapstructure:"parameters" json:"parameters,omitempty"`
	TestDataRef  *TestDataRef   `mapstructure:"test_data_ref" json:"test_data_ref,omitempty"`
}

// ToEvaluationBenchmark returns the benchmark spec for evaluation jobs and runtime (strips collection-only url).
func (b CollectionBenchmarkConfig) ToEvaluationBenchmark() EvaluationBenchmarkConfig {
	return EvaluationBenchmarkConfig{
		Ref:          b.Ref,
		ProviderID:   b.ProviderID,
		Weight:       b.Weight,
		PrimaryScore: b.PrimaryScore,
		PassCriteria: b.PassCriteria,
		Parameters:   b.Parameters,
		TestDataRef:  b.TestDataRef,
	}
}

// CollectionAgentMetadata contains structured metadata for AI agent consumption at the collection level.
type CollectionAgentMetadata struct {
	Evaluates            []string `mapstructure:"evaluates" yaml:"evaluates" json:"evaluates,omitempty"`
	RecommendedWhen      []string `mapstructure:"recommended_when" yaml:"recommended_when" json:"recommended_when,omitempty"`
	Summary              string   `mapstructure:"summary" yaml:"summary" json:"summary,omitempty" validate:"omitempty,max=200"`
	Complements          []string `mapstructure:"complements" yaml:"complements" json:"complements,omitempty"`
	Hints                []string `mapstructure:"hints" yaml:"hints" json:"hints,omitempty"`
	ResultInterpretation []string `mapstructure:"result_interpretation" yaml:"result_interpretation" json:"result_interpretation,omitempty"`
}

// CollectionConfig represents request to create a collection
type CollectionConfig struct {
	Name        string `mapstructure:"name" json:"name" validate:"required"`
	Description string `mapstructure:"description" json:"description,omitempty" validate:"omitempty,max=1024,min=1"`
	// Category is deprecated. Use Domains instead. Retained for backwards compatibility.
	// Will be made optional and eventually removed in a future version.
	Category     string                      `mapstructure:"category" json:"category" validate:"required,max=128,min=1"`
	Tags         []string                    `mapstructure:"tags" json:"tags,omitempty" validate:"omitempty,dive,tagname"`
	Custom       *map[string]any             `mapstructure:"custom" json:"custom,omitempty"`
	PassCriteria *PassCriteria               `mapstructure:"pass_criteria" json:"pass_criteria,omitempty"`
	Benchmarks   []CollectionBenchmarkConfig `mapstructure:"benchmarks" json:"benchmarks" validate:"required,min=1,dive"`
	Agent        *CollectionAgentMetadata    `mapstructure:"agent" json:"agent,omitempty"`

	// CurationOrder enables to sort curated collections for display.
	// 0 (or absent) means not curated. Positive integers give display position:
	// lower value = higher on the display (1 appears before 2). Admin-only via YAML;
	// the server rejects writes from tenant API consumers.
	CurationOrder int `mapstructure:"curation_order" json:"curation_order,omitempty" validate:"omitempty,min=0"`

	// Domains lists the high-level evaluation domains (snake_case). Supersedes Category.
	// If not set, the handler auto-computes the union from BenchmarkResource.Domains entries.
	Domains []string `mapstructure:"domains" json:"domains,omitempty"`

	// Tasks lists the ML tasks evaluated (snake_case).
	// If not set, the handler auto-computes the union from BenchmarkResource.Tasks entries.
	Tasks []string `mapstructure:"tasks" json:"tasks,omitempty"`

	// Modalities lists the data modalities (snake_case).
	// If not set, the handler auto-computes the union from BenchmarkResource.Modalities entries.
	Modalities []string `mapstructure:"modalities" json:"modalities,omitempty"`

	// Industries lists the target business industries (snake_case).
	// Collection-level only — same benchmark may serve different industries depending on context.
	Industries []string `mapstructure:"industries" json:"industries,omitempty"`

	// AIEntities lists the AI entity types evaluated (snake_case).
	// Example values: model, agent.
	AIEntities []string `mapstructure:"ai_entities" json:"ai_entities,omitempty"`
}

// CollectionState holds server-managed runtime state for custom (tenant-scoped) collections.
// It is never user-supplied and never written to YAML configuration.
// Absent on system collections.
type CollectionState struct {
	// DerivedFrom is the ID of the collection this was copied from.
	// Set by the server when POST /collections/{id}/clones is called; never user-supplied.
	DerivedFrom string `json:"derived_from,omitempty"`

	// RunCount is the number of EvaluationJobs created from this collection by its owning tenant.
	// Per-tenant counter, incremented at job creation.
	RunCount int `json:"run_count,omitempty"`

	// PinnedOrder is the tenant's personal pin ordering.
	// 0 = not pinned. Positive integers give explicit ordering (ascending).
	PinnedOrder int `json:"pinned_order,omitempty"`
}

// CollectionResource represents collection resource
type CollectionResource struct {
	Resource Resource `json:"resource"`
	CollectionConfig

	// State holds server-managed runtime state. Present only on custom (tenant-scoped)
	// collections; nil for system collections.
	State *CollectionState `json:"state,omitempty"`
}

// CollectionResourceList represents list of collection resources with pagination
type CollectionResourceList struct {
	Page
	Items []CollectionResource `json:"items"`
}
