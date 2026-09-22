package server

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/evalhubclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// EvalHubToolClient is the subset of evalhubclient.Client methods used by MCP
// tool handlers. Accepting an interface keeps handlers testable without a
// running eval-hub backend.
type EvalHubToolClient interface {
	CreateJob(config api.EvaluationJobConfig) (*api.EvaluationJobResource, error)
	CancelJob(id string) error
	GetJob(id string) (*api.EvaluationJobResource, error)
	ListProviders(opts ...evalhubclient.ListOption) (*api.ProviderResourceList, error)
	GetProvider(id string) (*api.ProviderResource, error)
	GetBenchmark(id string) (*api.BenchmarkResource, error)
	CreateCollection(config api.CollectionConfig) (*api.CollectionResource, error)
}

// --- input types ---

type SubmitEvaluationInput struct {
	Name        string                          `json:"name" jsonschema:"Name for the evaluation job"`
	Description string                          `json:"description,omitempty" jsonschema:"Human-readable description of what this evaluation measures"`
	Tags        []string                        `json:"tags,omitempty" jsonschema:"Tags for categorizing the evaluation"`
	Model       api.ModelRef                    `json:"model" jsonschema:"Model endpoint to evaluate: url and name required; optional parameters and auth.secret_ref (same fields as POST /api/v1/evaluations)"`
	Benchmarks  []api.EvaluationBenchmarkConfig `json:"benchmarks,omitempty" jsonschema:"List of benchmarks to run; provide benchmarks OR collection, not both"`
	Collection  *api.CollectionRef              `json:"collection,omitempty" jsonschema:"Benchmark collection to run; provide collection OR benchmarks, not both"`
	Experiment  *ExperimentInput                `json:"experiment,omitempty" jsonschema:"Optional MLflow experiment tracking configuration"`
}

type ExperimentInput struct {
	Name             string            `json:"name,omitempty" jsonschema:"MLflow experiment name"`
	Tags             map[string]string `json:"tags,omitempty" jsonschema:"Key-value tags for the MLflow experiment"`
	ArtifactLocation string            `json:"artifact_location,omitempty" jsonschema:"Storage location for experiment artifacts"`
}

type DiscoverProvidersInput struct {
	TargetType string   `json:"target_type,omitempty" jsonschema:"Filter by target type: model, agent, or inference_server"`
	Evaluates  []string `json:"evaluates,omitempty" jsonschema:"Filter to providers that evaluate all of these capabilities (e.g. safety, robustness)"`
}

type CancelJobInput struct {
	JobID string `json:"job_id" jsonschema:"ID of the evaluation job to cancel"`
}

type GetJobStatusInput struct {
	JobID string `json:"job_id" jsonschema:"ID of the evaluation job to check"`
}

type CreateCollectionInput struct {
	Name         string                          `json:"name" jsonschema:"Collection name"`
	Description  string                          `json:"description,omitempty" jsonschema:"Human-readable description of what this collection evaluates"`
	Domains      []string                        `json:"domains" jsonschema:"High-level evaluation domains in snake_case (e.g. safety, instruction_following, long_context). At least one domain is required."`
	Tags         []string                        `json:"tags,omitempty" jsonschema:"Tags for categorizing the collection"`
	PassCriteria *api.PassCriteria               `json:"pass_criteria,omitempty" jsonschema:"Collection-level pass/fail threshold (weighted average of benchmark thresholds)"`
	Benchmarks   []api.CollectionBenchmarkConfig `json:"benchmarks" jsonschema:"List of benchmarks with weights, metrics, thresholds, and parameters"`
}

type GetBenchmarkInput struct {
	BenchmarkID string `json:"benchmark_id" jsonschema:"ID of the benchmark to look up (e.g. ifeval, toxigen, mmlu)"`
	ProviderID  string `json:"provider_id,omitempty" jsonschema:"Optional owning provider ID to disambiguate when the same benchmark ID is offered by multiple providers"`
}

type DesignCollectionInput struct {
	EvaluationGoal string `json:"evaluation_goal" jsonschema:"Natural-language description of the evaluation use-case (e.g. 'enterprise deployment requiring safety, instruction following, and long-context support')"`
	ProviderFilter string `json:"provider_filter,omitempty" jsonschema:"Comma-separated provider IDs to restrict benchmark selection (e.g. 'lm_evaluation_harness,lighteval'); omit to consider all providers"`
	MaxBenchmarks  *int   `json:"max_benchmarks,omitempty" jsonschema:"Maximum number of benchmarks to include (default 12)"`
	Strictness     string `json:"strictness,omitempty" jsonschema:"Threshold strictness: lenient, moderate, or strict; shifts thresholds down/center/up respectively (default moderate)"`
}

type SearchBenchmarksInput struct {
	Query      string   `json:"query,omitempty" jsonschema:"Case-insensitive substring matched against benchmark id, name, description, and tags"`
	Labels     []string `json:"labels,omitempty" jsonschema:"Filter to benchmarks tagged with all of these labels (matched case-insensitively against tags)"`
	Category   string   `json:"category,omitempty" jsonschema:"Filter by benchmark category (e.g. safety, reasoning, instruction_following)"`
	ProviderID string   `json:"provider_id,omitempty" jsonschema:"Filter to benchmarks belonging to this provider id"`
	Limit      int      `json:"limit,omitempty" jsonschema:"Maximum number of benchmarks to return (default 50)"`
}

// --- output types ---

type SubmitEvaluationOutput struct {
	JobID string `json:"job_id"`
	State string `json:"state"`
}

type CancelJobOutput struct {
	JobID   string `json:"job_id"`
	Message string `json:"message"`
}

type GetJobStatusOutput struct {
	JobID      string                  `json:"job_id"`
	State      string                  `json:"state"`
	Progress   int                     `json:"progress_percent"`
	Benchmarks []BenchmarkStatusOutput `json:"benchmarks,omitempty"`
	CreatedAt  string                  `json:"created_at,omitempty"`
	StartedAt  string                  `json:"started_at,omitempty"`
}

type BenchmarkStatusOutput struct {
	ID                   string   `json:"id"`
	ProviderID           string   `json:"provider_id"`
	Status               string   `json:"status"`
	StartedAt            string   `json:"started_at,omitempty"`
	CompletedAt          string   `json:"completed_at,omitempty"`
	ResultInterpretation string   `json:"result_interpretation,omitempty"`
	Complements          []string `json:"complements,omitempty"`
}

type CreateCollectionOutput struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Owner     string `json:"owner"`
	CreatedAt string `json:"created_at,omitempty"`
}

type ProviderSummaryOutput struct {
	ID                   string   `json:"id"`
	Name                 string   `json:"name"`
	Title                string   `json:"title"`
	Summary              string   `json:"summary,omitempty"`
	TargetType           string   `json:"target_type,omitempty"`
	Evaluates            []string `json:"evaluates,omitempty"`
	Hints                []string `json:"hints,omitempty"`
	ResultInterpretation []string `json:"result_interpretation,omitempty"`
	Complements          []string `json:"complements,omitempty"`
	RecommendedWhen      []string `json:"recommended_when,omitempty"`
}

type DiscoverProvidersOutput struct {
	Providers []ProviderSummaryOutput `json:"providers"`
}

// BenchmarkOutput is a benchmark enriched with the owning provider_id, which the
// raw api.BenchmarkResource does not carry but callers need for submit_evaluation
// and create_collection.
type BenchmarkOutput struct {
	ID           string            `json:"id"`
	ProviderID   string            `json:"provider_id"`
	Name         string            `json:"name,omitempty"`
	Description  string            `json:"description,omitempty"`
	Category     string            `json:"category,omitempty"`
	Metrics      []string          `json:"metrics,omitempty"`
	Tags         []string          `json:"tags,omitempty"`
	NumFewShot   int               `json:"num_few_shot,omitempty"`
	DatasetSize  int               `json:"dataset_size,omitempty"`
	PrimaryScore *api.PrimaryScore `json:"primary_score,omitempty"`
	PassCriteria *api.PassCriteria `json:"pass_criteria,omitempty"`
	Domains      []string          `json:"domains,omitempty"`
	Tasks        []string          `json:"tasks,omitempty"`
}

type SearchBenchmarksOutput struct {
	Benchmarks []BenchmarkOutput `json:"benchmarks"`
	Total      int               `json:"total"`
}

type DesignCollectionOutput struct {
	// Guidance holds the calibration instructions only (benchmark selection,
	// weighting, and threshold guidance). The benchmark catalog and reference
	// collections are returned as the structured Benchmarks and CollectionExamples
	// fields rather than embedded in the guidance text, so callers can consume them
	// directly instead of re-querying search_benchmarks.
	Guidance           string                  `json:"guidance"`
	Benchmarks         []benchmarkCatalogEntry `json:"benchmarks"`
	CollectionExamples []collectionExample     `json:"collection_examples"`
}

// --- registration ---

func registerTools(srv *mcp.Server, client EvalHubToolClient, ds EvalHubDiscovery, logger *slog.Logger) error {
	submitIn, err := mcpToolSchema[SubmitEvaluationInput]()
	if err != nil {
		return fmt.Errorf("submit_evaluation input schema: %w", err)
	}
	submitOut, err := mcpToolSchema[SubmitEvaluationOutput]()
	if err != nil {
		return fmt.Errorf("submit_evaluation output schema: %w", err)
	}
	cancelIn, err := mcpToolSchema[CancelJobInput]()
	if err != nil {
		return fmt.Errorf("cancel_job input schema: %w", err)
	}
	cancelOut, err := mcpToolSchema[CancelJobOutput]()
	if err != nil {
		return fmt.Errorf("cancel_job output schema: %w", err)
	}
	statusIn, err := mcpToolSchema[GetJobStatusInput]()
	if err != nil {
		return fmt.Errorf("get_job_status input schema: %w", err)
	}
	statusOut, err := mcpToolSchema[GetJobStatusOutput]()
	if err != nil {
		return fmt.Errorf("get_job_status output schema: %w", err)
	}
	discoverIn, err := mcpToolSchema[DiscoverProvidersInput]()
	if err != nil {
		return fmt.Errorf("discover_providers input schema: %w", err)
	}
	discoverOut, err := mcpToolSchema[DiscoverProvidersOutput]()
	if err != nil {
		return fmt.Errorf("discover_providers output schema: %w", err)
	}
	collectionIn, err := mcpToolSchema[CreateCollectionInput]()
	if err != nil {
		return fmt.Errorf("create_collection input schema: %w", err)
	}
	collectionOut, err := mcpToolSchema[CreateCollectionOutput]()
	if err != nil {
		return fmt.Errorf("create_collection output schema: %w", err)
	}
	getBenchmarkIn, err := mcpToolSchema[GetBenchmarkInput]()
	if err != nil {
		return fmt.Errorf("get_benchmark input schema: %w", err)
	}
	getBenchmarkOut, err := mcpToolSchema[BenchmarkOutput]()
	if err != nil {
		return fmt.Errorf("get_benchmark output schema: %w", err)
	}
	searchBenchmarksIn, err := mcpToolSchema[SearchBenchmarksInput]()
	if err != nil {
		return fmt.Errorf("search_benchmarks input schema: %w", err)
	}
	searchBenchmarksOut, err := mcpToolSchema[SearchBenchmarksOutput]()
	if err != nil {
		return fmt.Errorf("search_benchmarks output schema: %w", err)
	}
	designCollectionIn, err := mcpToolSchema[DesignCollectionInput]()
	if err != nil {
		return fmt.Errorf("design_collection input schema: %w", err)
	}
	designCollectionOut, err := mcpToolSchema[DesignCollectionOutput]()
	if err != nil {
		return fmt.Errorf("design_collection output schema: %w", err)
	}

	// The design_collection tool mirrors the design_collection prompt: it reuses
	// the same YAML guidance templates so both channels stay in sync.
	prompts, err := loadPrompts()
	if err != nil {
		return fmt.Errorf("loading prompts for design_collection tool: %w", err)
	}
	designPrompt, ok := prompts[PromptNameDesignCollection]
	if !ok || designPrompt.Result == nil {
		return fmt.Errorf("design_collection prompt config is missing")
	}

	mcp.AddTool(srv, &mcp.Tool{
		Name:         "submit_evaluation",
		Description:  "Submit a new model evaluation job. Specify benchmarks (a list of benchmark IDs with their provider) OR a collection (a pre-defined set of benchmarks), plus the model endpoint to evaluate. Returns the job ID and initial state for tracking.",
		InputSchema:  submitIn,
		OutputSchema: submitOut,
	}, submitEvaluationHandler(client, logger))

	mcp.AddTool(srv, &mcp.Tool{
		Name:         "cancel_job",
		Description:  "Cancel a running or pending evaluation job. The job will be stopped and its benchmarks marked as cancelled. Use get_job_status to verify the final state.",
		InputSchema:  cancelIn,
		OutputSchema: cancelOut,
	}, cancelJobHandler(client, logger))

	mcp.AddTool(srv, &mcp.Tool{
		Name:         "get_job_status",
		Description:  "Get the current status of an evaluation job including overall state, progress percentage, and per-benchmark status with timestamps. Designed for polling: call repeatedly to monitor a running evaluation.",
		InputSchema:  statusIn,
		OutputSchema: statusOut,
	}, getJobStatusHandler(client, logger))

	mcp.AddTool(srv, &mcp.Tool{
		Name:         "discover_providers",
		Description:  "Discover evaluation providers. Filter by target_type (model, agent, inference_server) and/or evaluates (e.g. safety, robustness) to find the right provider for your use case. Each result includes a summary, usage hints, result interpretation guidance, and complementary provider suggestions.",
		InputSchema:  discoverIn,
		OutputSchema: discoverOut,
	}, discoverProvidersHandler(client, logger))

	mcp.AddTool(srv, &mcp.Tool{
		Name:         "create_collection",
		Description:  "Create a new benchmark collection. Use after designing a collection (via the design_collection prompt or manually) to persist it in eval-hub. The collection can then be used with submit_evaluation to run evaluations.",
		InputSchema:  collectionIn,
		OutputSchema: collectionOut,
	}, createCollectionHandler(client, logger))

	mcp.AddTool(srv, &mcp.Tool{
		Name:         "search_benchmarks",
		Description:  "Search the live benchmark catalog and return matching benchmarks with their id and provider_id. Filter by query (substring), labels (tags), category, and/or provider_id. Prefer this over discover_providers when you need actual benchmark IDs to build a collection or submit an evaluation — discover_providers returns provider-level metadata only, not benchmark IDs.",
		InputSchema:  searchBenchmarksIn,
		OutputSchema: searchBenchmarksOut,
	}, searchBenchmarksHandler(client, logger))

	mcp.AddTool(srv, &mcp.Tool{
		Name:         "get_benchmark",
		Description:  "Look up a single benchmark by its ID. Returns full detail including the provider_id (required to run it), name, description, category, metrics, primary_score, and pass_criteria threshold. Use this to confirm a benchmark exists and to obtain the provider_id needed by submit_evaluation and create_collection.",
		InputSchema:  getBenchmarkIn,
		OutputSchema: getBenchmarkOut,
	}, getBenchmarkToolHandler(client, logger))

	mcp.AddTool(srv, &mcp.Tool{
		Name:         "design_collection",
		Description:  "Design a benchmark collection from a natural-language evaluation goal. Fetches the live provider catalog and curated collection examples and returns guidance for benchmark selection, weight assignment, and threshold calibration. Returns guidance only — follow it up with create_collection to persist the design. This is the model-callable equivalent of the design_collection prompt.",
		InputSchema:  designCollectionIn,
		OutputSchema: designCollectionOut,
	}, designCollectionToolHandler(ds, designPrompt.Result, logger))

	return nil
}

// --- handlers ---

func submitEvaluationHandler(client EvalHubToolClient, logger *slog.Logger) mcp.ToolHandlerFor[SubmitEvaluationInput, SubmitEvaluationOutput] {
	return func(ctx context.Context, req *mcp.CallToolRequest, input SubmitEvaluationInput) (*mcp.CallToolResult, SubmitEvaluationOutput, error) {
		log := requestLogger(ctx, logger)
		client := evalHubToolClientForRequest(ctx, client, logger)
		log.Debug("submit_evaluation called", "name", input.Name)

		if len(input.Benchmarks) == 0 && input.Collection == nil {
			return errorResult("validation error: provide at least one of 'benchmarks' or 'collection'"), SubmitEvaluationOutput{}, nil
		}
		if len(input.Benchmarks) > 0 && input.Collection != nil {
			return errorResult("validation error: provide 'benchmarks' or 'collection', not both"), SubmitEvaluationOutput{}, nil
		}

		config := buildJobConfig(input)

		job, err := client.CreateJob(config)
		if err != nil {
			log.Error("submit_evaluation failed", "error", err)
			return errorResult(fmt.Sprintf("failed to create evaluation job: %v", err)), SubmitEvaluationOutput{}, nil
		}

		state := "pending"
		if job.Status != nil {
			state = job.Status.State.String()
		}

		out := SubmitEvaluationOutput{
			JobID: job.Resource.ID,
			State: state,
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Evaluation job created: %s (state: %s)", out.JobID, out.State)},
			},
		}, out, nil
	}
}

func cancelJobHandler(client EvalHubToolClient, logger *slog.Logger) mcp.ToolHandlerFor[CancelJobInput, CancelJobOutput] {
	return func(ctx context.Context, req *mcp.CallToolRequest, input CancelJobInput) (*mcp.CallToolResult, CancelJobOutput, error) {
		log := requestLogger(ctx, logger)
		client := evalHubToolClientForRequest(ctx, client, logger)
		log.Debug("cancel_job called", "job_id", input.JobID)

		if input.JobID == "" {
			return errorResult("validation error: 'job_id' is required"), CancelJobOutput{}, nil
		}

		err := client.CancelJob(input.JobID)
		if err != nil {
			log.Error("cancel_job failed", "job_id", input.JobID, "error", err)
			return errorResult(fmt.Sprintf("failed to cancel job %s: %v", input.JobID, err)), CancelJobOutput{}, nil
		}

		out := CancelJobOutput{
			JobID:   input.JobID,
			Message: fmt.Sprintf("Job %s cancelled successfully", input.JobID),
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: out.Message},
			},
		}, out, nil
	}
}

func getJobStatusHandler(client EvalHubToolClient, logger *slog.Logger) mcp.ToolHandlerFor[GetJobStatusInput, GetJobStatusOutput] {
	return func(ctx context.Context, req *mcp.CallToolRequest, input GetJobStatusInput) (*mcp.CallToolResult, GetJobStatusOutput, error) {
		log := requestLogger(ctx, logger)
		client := evalHubToolClientForRequest(ctx, client, logger)
		log.Debug("get_job_status called", "job_id", input.JobID)

		if input.JobID == "" {
			return errorResult("validation error: 'job_id' is required"), GetJobStatusOutput{}, nil
		}

		job, err := client.GetJob(input.JobID)
		if err != nil {
			log.Error("get_job_status failed", "job_id", input.JobID, "error", err)
			return errorResult(fmt.Sprintf("failed to get job status for %s: %v", input.JobID, err)), GetJobStatusOutput{}, nil
		}

		out := buildJobStatusOutput(job)
		enrichBenchmarkStatuses(client, log, out.Benchmarks)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Job %s: %s (%d%% complete)", out.JobID, out.State, out.Progress)},
			},
		}, out, nil
	}
}

func discoverProvidersHandler(client EvalHubToolClient, logger *slog.Logger) mcp.ToolHandlerFor[DiscoverProvidersInput, DiscoverProvidersOutput] {
	return func(ctx context.Context, req *mcp.CallToolRequest, input DiscoverProvidersInput) (*mcp.CallToolResult, DiscoverProvidersOutput, error) {
		log := requestLogger(ctx, logger)
		client := evalHubToolClientForRequest(ctx, client, logger)
		log.Debug("discover_providers called", "target_type", input.TargetType, "evaluates", input.Evaluates)

		list, err := client.ListProviders()
		if err != nil {
			log.Error("discover_providers failed", "error", err)
			return errorResult(fmt.Sprintf("failed to list providers: %v", err)), DiscoverProvidersOutput{}, nil
		}

		var targetTypes []string
		var evaluates []string

		hasFilter := input.TargetType != "" || len(input.Evaluates) > 0
		var providers []ProviderSummaryOutput
		for _, p := range list.Items {
			if hasFilter && p.Agent == nil {
				continue
			}
			if p.Agent != nil {
				targetTypes = append(targetTypes, p.Agent.TargetType)
				evaluates = append(evaluates, p.Agent.Evaluates...)
			}
			if input.TargetType != "" && (p.Agent == nil || p.Agent.TargetType != input.TargetType) {
				continue
			}
			if len(input.Evaluates) > 0 && !agentEvaluatesAll(p.Agent, input.Evaluates) {
				continue
			}
			providers = append(providers, toProviderSummary(p))
		}

		if providers == nil {
			providers = []ProviderSummaryOutput{}
		}

		out := DiscoverProvidersOutput{Providers: providers}
		return &mcp.CallToolResult{
			Meta: mcp.Meta{
				"target_types_found": strings.Join(targetTypes, ","),
				"target_type":        input.TargetType,
				"evaluates_found":    strings.Join(evaluates, ","),
				"evaluates":          strings.Join(input.Evaluates, ","),
			},
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Found %d providers", len(providers))},
			},
		}, out, nil
	}
}

func createCollectionHandler(client EvalHubToolClient, logger *slog.Logger) mcp.ToolHandlerFor[CreateCollectionInput, CreateCollectionOutput] {
	return func(ctx context.Context, req *mcp.CallToolRequest, input CreateCollectionInput) (*mcp.CallToolResult, CreateCollectionOutput, error) {
		log := requestLogger(ctx, logger)
		client := evalHubToolClientForRequest(ctx, client, logger)
		log.Debug("create_collection called", "name", input.Name)

		if input.Name == "" {
			return errorResult("validation error: 'name' is required"), CreateCollectionOutput{}, nil
		}
		// Collections are classified by domains only. category is the deprecated
		// predecessor (#1028) and is intentionally not exposed by this tool.
		if len(input.Domains) == 0 {
			return errorResult("validation error: at least one domain is required"), CreateCollectionOutput{}, nil
		}
		if len(input.Benchmarks) == 0 {
			return errorResult("validation error: at least one benchmark is required"), CreateCollectionOutput{}, nil
		}

		config := api.CollectionConfig{
			Name:         input.Name,
			Description:  input.Description,
			Domains:      input.Domains,
			Tags:         input.Tags,
			PassCriteria: input.PassCriteria,
			Benchmarks:   input.Benchmarks,
		}

		collection, err := client.CreateCollection(config)
		if err != nil {
			log.Error("create_collection failed", "error", err)
			return errorResult(fmt.Sprintf("failed to create collection: %v", err)), CreateCollectionOutput{}, nil
		}

		out := CreateCollectionOutput{
			ID:        collection.Resource.ID,
			Name:      collection.Name,
			Owner:     string(collection.Resource.Owner),
			CreatedAt: collection.Resource.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Collection created: %s (id: %s)", out.Name, out.ID)},
			},
		}, out, nil
	}
}

// defaultSearchBenchmarksLimit bounds search_benchmarks output when the caller
// does not set an explicit limit.
const defaultSearchBenchmarksLimit = 50

func searchBenchmarksHandler(client EvalHubToolClient, logger *slog.Logger) mcp.ToolHandlerFor[SearchBenchmarksInput, SearchBenchmarksOutput] {
	return func(ctx context.Context, req *mcp.CallToolRequest, input SearchBenchmarksInput) (*mcp.CallToolResult, SearchBenchmarksOutput, error) {
		log := requestLogger(ctx, logger)
		client := evalHubToolClientForRequest(ctx, client, logger)
		log.Debug("search_benchmarks called",
			"query", input.Query, "labels", input.Labels, "category", input.Category, "provider_id", input.ProviderID)

		benchmarks, err := collectBenchmarks(client)
		if err != nil {
			log.Error("search_benchmarks failed", "error", err)
			return errorResult(fmt.Sprintf("failed to list benchmarks: %v", err)), SearchBenchmarksOutput{}, nil
		}

		query := strings.ToLower(strings.TrimSpace(input.Query))
		category := strings.ToLower(strings.TrimSpace(input.Category))
		providerID := strings.TrimSpace(input.ProviderID)
		labels := make([]string, 0, len(input.Labels))
		for _, l := range input.Labels {
			if l = strings.ToLower(strings.TrimSpace(l)); l != "" {
				labels = append(labels, l)
			}
		}

		matched := make([]BenchmarkOutput, 0)
		for _, b := range benchmarks {
			if providerID != "" && b.ProviderID != providerID {
				continue
			}
			if category != "" && strings.ToLower(b.Category) != category {
				continue
			}
			if len(labels) > 0 && !benchmarkHasAllLabels(b, labels) {
				continue
			}
			if query != "" && !benchmarkMatchesQuery(b, query) {
				continue
			}
			matched = append(matched, b)
		}

		total := len(matched)
		limit := input.Limit
		if limit <= 0 {
			limit = defaultSearchBenchmarksLimit
		}
		if len(matched) > limit {
			matched = matched[:limit]
		}

		out := SearchBenchmarksOutput{Benchmarks: matched, Total: total}
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Found %d benchmarks (returning %d)", total, len(matched))},
			},
		}, out, nil
	}
}

func getBenchmarkToolHandler(client EvalHubToolClient, logger *slog.Logger) mcp.ToolHandlerFor[GetBenchmarkInput, BenchmarkOutput] {
	return func(ctx context.Context, req *mcp.CallToolRequest, input GetBenchmarkInput) (*mcp.CallToolResult, BenchmarkOutput, error) {
		log := requestLogger(ctx, logger)
		client := evalHubToolClientForRequest(ctx, client, logger)
		id := strings.TrimSpace(input.BenchmarkID)
		providerID := strings.TrimSpace(input.ProviderID)
		log.Debug("get_benchmark called", "benchmark_id", id, "provider_id", providerID)

		if id == "" {
			return errorResult("validation error: 'benchmark_id' is required"), BenchmarkOutput{}, nil
		}

		benchmarks, err := collectBenchmarks(client)
		if err != nil {
			log.Error("get_benchmark failed", "error", err)
			return errorResult(fmt.Sprintf("failed to list benchmarks: %v", err)), BenchmarkOutput{}, nil
		}

		var matches []BenchmarkOutput
		for _, b := range benchmarks {
			if b.ID != id {
				continue
			}
			if providerID != "" && b.ProviderID != providerID {
				continue
			}
			matches = append(matches, b)
		}

		switch len(matches) {
		case 0:
			if providerID != "" {
				return errorResult(fmt.Sprintf("benchmark %q not found for provider %q; use search_benchmarks to find valid benchmark ids", id, providerID)), BenchmarkOutput{}, nil
			}
			return errorResult(fmt.Sprintf("benchmark %q not found; use search_benchmarks to find valid benchmark ids", id)), BenchmarkOutput{}, nil
		case 1:
			b := matches[0]
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: fmt.Sprintf("Benchmark %s (provider: %s)", b.ID, b.ProviderID)},
				},
			}, b, nil
		default:
			providers := make([]string, 0, len(matches))
			for _, b := range matches {
				providers = append(providers, b.ProviderID)
			}
			return errorResult(fmt.Sprintf("benchmark %q is offered by multiple providers (%s); set 'provider_id' to disambiguate", id, strings.Join(providers, ", "))), BenchmarkOutput{}, nil
		}
	}
}

// designCollectionToolHandler is the model-callable equivalent of the
// design_collection prompt. It shares gatherDesignCollection with the prompt so
// the calibration guidance stays identical, but returns the benchmark catalog and
// reference collections as structured fields (rather than embedded JSON) so the
// caller can consume them directly instead of re-querying search_benchmarks.
func designCollectionToolHandler(ds EvalHubDiscovery, result *promptResultConfig, logger *slog.Logger) mcp.ToolHandlerFor[DesignCollectionInput, DesignCollectionOutput] {
	return func(ctx context.Context, req *mcp.CallToolRequest, input DesignCollectionInput) (*mcp.CallToolResult, DesignCollectionOutput, error) {
		log := requestLogger(ctx, logger)
		ds := evalHubDiscoveryForRequest(ctx, ds, logger)

		maxBenchmarksRaw := ""
		if input.MaxBenchmarks != nil {
			maxBenchmarksRaw = strconv.Itoa(*input.MaxBenchmarks)
		}

		log.Debug("design_collection tool called",
			ArgNameEvaluationGoal, input.EvaluationGoal,
			ArgNameProviderFilter, input.ProviderFilter,
			ArgNameMaxBenchmarks, maxBenchmarksRaw,
			ArgNameStrictness, input.Strictness,
		)

		data, err := gatherDesignCollection(ds, result, input.EvaluationGoal, input.ProviderFilter, maxBenchmarksRaw, input.Strictness)
		if err != nil {
			log.Error("design_collection tool failed", "error", err)
			return errorResult(err.Error()), DesignCollectionOutput{}, nil
		}

		// The catalog and examples travel in the structured output fields, so the
		// guidance text points at them instead of embedding the full JSON.
		messages, err := renderDesignCollectionMessages(result, data,
			"(provided as the structured `benchmarks` field of this tool result)",
			"(provided as the structured `collection_examples` field of this tool result)",
		)
		if err != nil {
			log.Error("design_collection tool failed", "error", err)
			return errorResult(err.Error()), DesignCollectionOutput{}, nil
		}

		guidance := promptMessagesText(messages)
		out := DesignCollectionOutput{
			Guidance:           guidance,
			Benchmarks:         data.Benchmarks,
			CollectionExamples: data.Examples,
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: guidance},
			},
		}, out, nil
	}
}

// collectBenchmarks flattens every provider's benchmarks into BenchmarkOutput
// entries, attaching the owning provider_id to each (which the raw benchmark
// resource does not carry). It paginates through all provider pages so the
// catalog is complete even when the provider list exceeds a single page.
func collectBenchmarks(client EvalHubToolClient) ([]BenchmarkOutput, error) {
	providers, err := allProviders(client)
	if err != nil {
		return nil, err
	}
	out := make([]BenchmarkOutput, 0)
	for _, p := range providers {
		for _, b := range p.Benchmarks {
			out = append(out, toBenchmarkOutput(b, p.Resource.ID))
		}
	}
	return out, nil
}

func toBenchmarkOutput(b api.BenchmarkResource, providerID string) BenchmarkOutput {
	return BenchmarkOutput{
		ID:           b.ID,
		ProviderID:   providerID,
		Name:         b.Name,
		Description:  b.Description,
		Category:     b.Category,
		Metrics:      b.Metrics,
		Tags:         b.Tags,
		NumFewShot:   b.NumFewShot,
		DatasetSize:  b.DatasetSize,
		PrimaryScore: b.PrimaryScore,
		PassCriteria: b.PassCriteria,
		Domains:      b.Domains,
		Tasks:        b.Tasks,
	}
}

func benchmarkHasAllLabels(b BenchmarkOutput, labels []string) bool {
	tagset := make(map[string]struct{}, len(b.Tags))
	for _, t := range b.Tags {
		tagset[strings.ToLower(t)] = struct{}{}
	}
	for _, l := range labels {
		if _, ok := tagset[l]; !ok {
			return false
		}
	}
	return true
}

func benchmarkMatchesQuery(b BenchmarkOutput, query string) bool {
	if strings.Contains(strings.ToLower(b.ID), query) ||
		strings.Contains(strings.ToLower(b.Name), query) ||
		strings.Contains(strings.ToLower(b.Description), query) {
		return true
	}
	for _, t := range b.Tags {
		if strings.Contains(strings.ToLower(t), query) {
			return true
		}
	}
	return false
}

func toProviderSummary(p api.ProviderResource) ProviderSummaryOutput {
	out := ProviderSummaryOutput{
		ID:    p.Resource.ID,
		Name:  p.Name,
		Title: p.Title,
	}
	if p.Agent != nil {
		out.Summary = p.Agent.Summary
		out.TargetType = p.Agent.TargetType
		out.Evaluates = p.Agent.Evaluates
		out.Hints = p.Agent.Hints
		out.ResultInterpretation = p.Agent.ResultInterpretation
		out.Complements = p.Agent.Complements
		out.RecommendedWhen = p.Agent.RecommendedWhen
	}
	return out
}

func enrichBenchmarkStatuses(client EvalHubToolClient, log *slog.Logger, benchmarks []BenchmarkStatusOutput) {
	providerIDs := make(map[string]struct{})
	for _, b := range benchmarks {
		if api.IsBenchmarkTerminalState(api.State(b.Status)) {
			providerIDs[b.ProviderID] = struct{}{}
		}
	}
	if len(providerIDs) == 0 {
		return
	}

	providers := make(map[string]*api.ProviderResource, len(providerIDs))
	for pid := range providerIDs {
		p, err := client.GetProvider(pid)
		if err != nil {
			log.Debug("could not fetch provider for enrichment", "provider_id", pid, "error", err)
			continue
		}
		providers[pid] = p
	}

	for i := range benchmarks {
		b := &benchmarks[i]
		if !api.IsBenchmarkTerminalState(api.State(b.Status)) {
			continue
		}
		p, ok := providers[b.ProviderID]
		if !ok {
			continue
		}
		if p.Agent != nil {
			b.Complements = p.Agent.Complements
		}
		for _, bm := range p.Benchmarks {
			if bm.ID == b.ID && bm.Agent != nil {
				b.ResultInterpretation = bm.Agent.ResultInterpretation
				break
			}
		}
	}
}

func agentEvaluatesAll(agent *api.AgentMetadata, required []string) bool {
	if agent == nil {
		return false
	}
	have := make(map[string]struct{}, len(agent.Evaluates))
	for _, e := range agent.Evaluates {
		have[e] = struct{}{}
	}
	for _, r := range required {
		if _, ok := have[r]; !ok {
			return false
		}
	}
	return true
}

// --- helpers ---

func buildJobConfig(input SubmitEvaluationInput) api.EvaluationJobConfig {
	config := api.EvaluationJobConfig{
		Name:       input.Name,
		Tags:       input.Tags,
		Model:      &input.Model,
		Benchmarks: input.Benchmarks,
	}

	if input.Description != "" {
		config.Description = &input.Description
	}

	if input.Collection != nil {
		config.Collection = input.Collection
	}

	if input.Experiment != nil {
		exp := &api.ExperimentConfig{
			Name:             input.Experiment.Name,
			ArtifactLocation: input.Experiment.ArtifactLocation,
		}
		for k, v := range input.Experiment.Tags {
			exp.Tags = append(exp.Tags, api.ExperimentTag{Key: k, Value: v})
		}
		config.Experiment = exp
	}

	return config
}

func buildJobStatusOutput(job *api.EvaluationJobResource) GetJobStatusOutput {
	out := GetJobStatusOutput{
		JobID:     job.Resource.ID,
		CreatedAt: job.Resource.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		State:     "pending",
	}

	if job.Status != nil {
		out.State = job.Status.State.String()
		out.Progress = computeProgress(job.Status.Benchmarks)

		for _, b := range job.Status.Benchmarks {
			out.Benchmarks = append(out.Benchmarks, BenchmarkStatusOutput{
				ID:          b.ID,
				ProviderID:  b.ProviderID,
				Status:      string(b.Status),
				StartedAt:   string(b.StartedAt),
				CompletedAt: string(b.CompletedAt),
			})
		}

		out.StartedAt = earliestStart(job.Status.Benchmarks)
	}

	return out
}

func computeProgress(benchmarks []api.BenchmarkStatus) int {
	if len(benchmarks) == 0 {
		return 0
	}
	done := 0
	for _, b := range benchmarks {
		if api.IsBenchmarkTerminalState(b.Status) {
			done++
		}
	}
	return (done * 100) / len(benchmarks)
}

func earliestStart(benchmarks []api.BenchmarkStatus) string {
	var earliestTime time.Time
	var earliest string
	for _, b := range benchmarks {
		s := string(b.StartedAt)
		if s == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			continue
		}
		if earliestTime.IsZero() || t.Before(earliestTime) {
			earliestTime = t
			earliest = s
		}
	}
	return earliest
}

func errorResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: msg},
		},
		IsError: true,
	}
}
