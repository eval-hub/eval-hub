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
	Name         string                      `mapstructure:"name" json:"name" validate:"required"`
	Description  string                      `mapstructure:"description" json:"description,omitempty" validate:"omitempty,max=1024,min=1"`
	Category     string                      `mapstructure:"category" json:"category" validate:"required,max=128,min=1"`
	Tags         []string                    `mapstructure:"tags" json:"tags,omitempty" validate:"omitempty,dive,tagname"`
	Custom       *map[string]any             `mapstructure:"custom" json:"custom,omitempty"`
	PassCriteria *PassCriteria               `mapstructure:"pass_criteria" json:"pass_criteria,omitempty"`
	Benchmarks   []CollectionBenchmarkConfig `mapstructure:"benchmarks" json:"benchmarks" validate:"required,min=1,dive"`
	Agent        *CollectionAgentMetadata    `mapstructure:"agent" json:"agent,omitempty"`
}

// CollectionResource represents collection resource
type CollectionResource struct {
	Resource Resource `json:"resource"`
	CollectionConfig
}

// CollectionResourceList represents list of collection resources with pagination
type CollectionResourceList struct {
	Page
	Items []CollectionResource `json:"items"`
}
