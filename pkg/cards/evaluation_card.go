package cards

import "github.com/eval-hub/eval-hub/pkg/api"

const (
	CardVersion   = "1.0"
	SchemaVersion = "1.0"
)

// EvaluationCard is the top-level evaluation card document.
//
// Differences from api.EvaluationJobResource:
//   - Adds card_version and schema_version; no api.Resource (id, tenant, owner), status, or job config fields (name, description, tags, experiment, custom, exports, queue).
//   - Splits content into metadata, context, and results instead of resource + status + EvaluationJobConfig.
type EvaluationCard struct {
	CardVersion   string                 `json:"card_version"`
	SchemaVersion string                 `json:"schema_version"`
	Metadata      EvaluationCardMetadata `json:"metadata"`
	Context       EvaluationCardContext  `json:"context"`
	Results       *EvaluationCardResults `json:"results,omitempty"`
}

// EvaluationCardMetadata holds card-level metadata tied to the evaluation job.
//
// Differences from api.Resource:
//   - Uses evaluation_job_id instead of id; no tenant or owner.
//   - created_at and updated_at mirror the evaluation job resource timestamps (api.DateTime strings).
type EvaluationCardMetadata struct {
	EvaluationJobID string       `json:"evaluation_job_id"`
	CreatedAt       api.DateTime `json:"created_at,omitempty"`
	UpdatedAt       api.DateTime `json:"updated_at,omitempty"`
}

// EvaluationCardContext describes the evaluation inputs: model, collection, and benchmarks.
// For collection jobs only collection_id is set; benchmarks are not expanded into context.
// For direct benchmark jobs, benchmarks are taken from job.Benchmarks.
//
// Differences from api.EvaluationJobConfig:
//   - Contains only model, collection_id, and benchmarks; omits name, description, tags, pass_criteria, experiment, custom, exports, and queue.
//   - collection_id is a flat string field; api uses a nested api.CollectionRef (id plus optional benchmark overrides).
type EvaluationCardContext struct {
	Model        CardModelRef          `json:"model"`
	CollectionID string                `json:"collection_id,omitempty"`
	Benchmarks   []CardBenchmarkConfig `json:"benchmarks,omitempty"`
}

// CardModelRef identifies the model under evaluation in the card context.
//
// Differences from api.ModelRef:
//   - Uses model_card_url instead of card_url.
//   - Omits auth and parameters.
type CardModelRef struct {
	URL          string `json:"url"`
	Name         string `json:"name"`
	ModelCardURL string `json:"model_card_url,omitempty"`
}

// CardBenchmarkConfig describes a benchmark in the card context section.
//
// Differences from api.EvaluationBenchmarkConfig:
//   - id is an inline field; api embeds api.Ref.
//   - Adds contacts and config.
//   - Omits hardware_config and test_data_ref.
//   - Reuses api.PrimaryScore and api.PassCriteria for shared fields.
type CardBenchmarkConfig struct {
	ID           string            `json:"id"`
	ProviderID   string            `json:"provider_id"`
	Contacts     []string          `json:"contacts,omitempty"`
	Config       map[string]any    `json:"config,omitempty"`
	Parameters   map[string]any    `json:"parameters,omitempty"`
	PrimaryScore *api.PrimaryScore `json:"primary_score,omitempty"`
	PassCriteria *api.PassCriteria `json:"pass_criteria,omitempty"`
	Weight       float32           `json:"weight,omitempty"`
}

// EvaluationCardResults holds benchmark and collection-level results.
//
// Differences from api.EvaluationJobResults:
//   - Collection-level test is nested under collection instead of a top-level test field.
//   - Omits mlflow_experiment_url and evaluation_card_url.
//   - Uses CardBenchmarkResult instead of api.BenchmarkResult.
type EvaluationCardResults struct {
	Benchmarks []CardBenchmarkResult `json:"benchmarks,omitempty"`
	Collection *CardCollectionResult `json:"collection,omitempty"`
}

// CardBenchmarkResult holds the outcome of a single benchmark run.
//
// Differences from api.BenchmarkResult:
//   - Adds status (from api.BenchmarkStatus / api.BenchmarkStatusEvent).
//   - Omits benchmark_index.
//   - Uses CardBenchmarkTest instead of api.BenchmarkTest.
type CardBenchmarkResult struct {
	ID             string             `json:"id"`
	ProviderID     string             `json:"provider_id"`
	Contacts       []string           `json:"contacts,omitempty"`
	Status         api.State          `json:"status,omitempty"`
	Metrics        map[string]any     `json:"metrics,omitempty"`
	AdditionalInfo map[string]any     `json:"additional_info,omitempty"`
	Artifacts      map[string]any     `json:"artifacts,omitempty"`
	MLFlowRunID    string             `json:"mlflow_run_id,omitempty"`
	LogsPath       string             `json:"logs_path,omitempty"`
	Test           *CardBenchmarkTest `json:"test,omitempty"`
}

// CardBenchmarkTest holds pass/fail details for a single benchmark in the card.
//
// Differences from api.BenchmarkTest:
//   - primary_score and threshold are strings instead of float32.
//   - Omits primary_score_metric.
type CardBenchmarkTest struct {
	PrimaryScore string `json:"primary_score,omitempty"`
	Threshold    string `json:"threshold,omitempty"`
	Pass         bool   `json:"pass"`
}

// CardCollectionResult holds collection-level results in the card.
//
// Differences from api.EvaluationJobResults:
//   - Wraps only the collection test; api exposes collection-level test as a top-level test field on EvaluationJobResults.
type CardCollectionResult struct {
	Test *CardCollectionTest `json:"test,omitempty"`
}

// CardCollectionTest holds pass/fail details for the overall collection in the card.
//
// Differences from api.EvaluationTest:
//   - Same fields (score, threshold, pass) and types; no structural differences.
type CardCollectionTest struct {
	Score     float32 `json:"score"`
	Threshold float32 `json:"threshold"`
	Pass      bool    `json:"pass"`
}
