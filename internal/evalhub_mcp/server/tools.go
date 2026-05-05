package server

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// EvalHubToolClient is the subset of evalhubclient.Client methods used by MCP
// tool handlers. Accepting an interface keeps handlers testable without a
// running eval-hub backend.
type EvalHubToolClient interface {
	CreateJob(config api.EvaluationJobConfig) (*api.EvaluationJobResource, error)
	CancelJob(id string) error
	GetJob(id string) (*api.EvaluationJobResource, error)
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
	URL        string `json:"url" jsonschema:"URL of the model inference endpoint"`
	Name       string `json:"name" jsonschema:"Display name of the model"`
	AuthSecret string `json:"auth_secret,omitempty" jsonschema:"Kubernetes secret reference for model authentication"`
}

type BenchmarkInput struct {
	ID         string `json:"id" jsonschema:"Benchmark identifier"`
	ProviderID string `json:"provider_id" jsonschema:"Evaluation provider that runs this benchmark"`
}

type CollectionInput struct {
	ID string `json:"id" jsonschema:"Collection identifier"`
}

type ExperimentInput struct {
	Name             string            `json:"name,omitempty" jsonschema:"MLflow experiment name"`
	Tags             map[string]string `json:"tags,omitempty" jsonschema:"Key-value tags for the MLflow experiment"`
	ArtifactLocation string            `json:"artifact_location,omitempty" jsonschema:"Storage location for experiment artifacts"`
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
	ID          string `json:"id"`
	ProviderID  string `json:"provider_id"`
	Status      string `json:"status"`
	StartedAt   string `json:"started_at,omitempty"`
	CompletedAt string `json:"completed_at,omitempty"`
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
}

// --- handlers ---

func submitEvaluationHandler(client EvalHubToolClient, logger *slog.Logger) mcp.ToolHandlerFor[SubmitEvaluationInput, SubmitEvaluationOutput] {
	return func(ctx context.Context, req *mcp.CallToolRequest, input SubmitEvaluationInput) (*mcp.CallToolResult, SubmitEvaluationOutput, error) {
		logger.Debug("submit_evaluation called", "name", input.Name)

		if len(input.Benchmarks) == 0 && input.Collection == nil {
			return errorResult("validation error: provide at least one of 'benchmarks' or 'collection'"), SubmitEvaluationOutput{}, nil
		}
		if len(input.Benchmarks) > 0 && input.Collection != nil {
			return errorResult("validation error: provide 'benchmarks' or 'collection', not both"), SubmitEvaluationOutput{}, nil
		}

		config := buildJobConfig(input)

		job, err := client.CreateJob(config)
		if err != nil {
			logger.Error("submit_evaluation failed", "error", err)
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
		logger.Debug("cancel_job called", "job_id", input.JobID)

		if input.JobID == "" {
			return errorResult("validation error: 'job_id' is required"), CancelJobOutput{}, nil
		}

		err := client.CancelJob(input.JobID)
		if err != nil {
			logger.Error("cancel_job failed", "job_id", input.JobID, "error", err)
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
		logger.Debug("get_job_status called", "job_id", input.JobID)

		if input.JobID == "" {
			return errorResult("validation error: 'job_id' is required"), GetJobStatusOutput{}, nil
		}

		job, err := client.GetJob(input.JobID)
		if err != nil {
			logger.Error("get_job_status failed", "job_id", input.JobID, "error", err)
			return errorResult(fmt.Sprintf("failed to get job status for %s: %v", input.JobID, err)), GetJobStatusOutput{}, nil
		}

		out := buildJobStatusOutput(job)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Job %s: %s (%d%% complete)", out.JobID, out.State, out.Progress)},
			},
		}, out, nil
	}
}

// --- helpers ---

func buildJobConfig(input SubmitEvaluationInput) api.EvaluationJobConfig {
	config := api.EvaluationJobConfig{
		Name: input.Name,
		Tags: input.Tags,
		Model: api.ModelRef{
			URL:  input.Model.URL,
			Name: input.Model.Name,
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
