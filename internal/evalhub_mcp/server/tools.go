package server

import (
	"context"
	"fmt"
	"log/slog"
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
}

// --- input types ---

type SubmitEvaluationInput struct {
	Name        string           `json:"name" jsonschema:"Name for the evaluation job"`
	Description string           `json:"description,omitempty" jsonschema:"Human-readable description of what this evaluation measures"`
	Tags        []string         `json:"tags,omitempty" jsonschema:"Tags for categorizing the evaluation"`
	Model       ModelInput       `json:"model" jsonschema:"Model to evaluate"`
	Benchmarks  []BenchmarkInput `json:"benchmarks,omitempty" jsonschema:"List of benchmarks to run; provide benchmarks OR collection, not both"`
	Collection  *CollectionInput `json:"collection,omitempty" jsonschema:"Benchmark collection to run; provide collection OR benchmarks, not both"`
	Experiment  *ExperimentInput `json:"experiment,omitempty" jsonschema:"Optional MLflow experiment tracking configuration"`
}

type ModelInput struct {
	URL        string         `json:"url" jsonschema:"URL of the model inference endpoint"`
	Name       string         `json:"name" jsonschema:"Display name of the model"`
	AuthSecret string         `json:"auth_secret,omitempty" jsonschema:"Kubernetes secret reference for model authentication"`
	Parameters map[string]any `json:"parameters,omitempty" jsonschema:"Optional model inference parameters (e.g. temperature, max_tokens)"`
}

type BenchmarkInput struct {
	ID         string         `json:"id" jsonschema:"Benchmark identifier"`
	ProviderID string         `json:"provider_id" jsonschema:"Evaluation provider that runs this benchmark"`
	Parameters map[string]any `json:"parameters,omitempty" jsonschema:"Optional benchmark-specific parameters passed to the evaluation adapter"`
}

type CollectionInput struct {
	ID string `json:"id" jsonschema:"Collection identifier"`
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

// --- registration ---

func registerTools(srv *mcp.Server, client EvalHubToolClient, logger *slog.Logger) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "submit_evaluation",
		Description: "Submit a new model evaluation job. Specify benchmarks (a list of benchmark IDs with their provider) OR a collection (a pre-defined set of benchmarks), plus the model endpoint to evaluate. Returns the job ID and initial state for tracking.",
	}, submitEvaluationHandler(client, logger))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "cancel_job",
		Description: "Cancel a running or pending evaluation job. The job will be stopped and its benchmarks marked as cancelled. Use get_job_status to verify the final state.",
	}, cancelJobHandler(client, logger))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_job_status",
		Description: "Get the current status of an evaluation job including overall state, progress percentage, and per-benchmark status with timestamps. Designed for polling: call repeatedly to monitor a running evaluation.",
	}, getJobStatusHandler(client, logger))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "discover_providers",
		Description: "Discover evaluation providers. Filter by target_type (model, agent, inference_server) and/or evaluates (e.g. safety, robustness) to find the right provider for your use case. Each result includes a summary, usage hints, result interpretation guidance, and complementary provider suggestions.",
	}, discoverProvidersHandler(client, logger))
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
		Name: input.Name,
		Tags: input.Tags,
		Model: api.ModelRef{
			URL:        input.Model.URL,
			Name:       input.Model.Name,
			Parameters: input.Model.Parameters,
		},
	}

	if input.Description != "" {
		config.Description = &input.Description
	}

	if input.Model.AuthSecret != "" {
		config.Model.Auth = &api.ModelAuth{SecretRef: input.Model.AuthSecret}
	}

	for _, b := range input.Benchmarks {
		config.Benchmarks = append(config.Benchmarks, api.EvaluationBenchmarkConfig{
			Ref:        api.Ref{ID: b.ID},
			ProviderID: b.ProviderID,
			Parameters: b.Parameters,
		})
	}

	if input.Collection != nil {
		config.Collection = &api.CollectionRef{ID: input.Collection.ID}
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
