package cards

import (
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestNewEvaluationCardFromDirectBenchmarkJob(t *testing.T) {
	threshold := float32(0.3)
	job := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{
				ID:        "job-123",
				CreatedAt: mustParseTime(t, "2026-07-07T00:00:00.001Z"),
				UpdatedAt: mustParseTime(t, "2026-07-07T01:00:00Z"),
			},
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{
				URL:     "https://vllm.example.com/v1",
				Name:    "meta-llama/Llama-3.2-1B-Instruct",
				CardURL: "https://example.com/model-card",
			},
			Benchmarks: []api.EvaluationBenchmarkConfig{
				{
					Ref:          api.Ref{ID: "arc_easy"},
					ProviderID:   "lm_evaluation_harness",
					Weight:       0.6,
					PrimaryScore: &api.PrimaryScore{Metric: "accuracy"},
					PassCriteria: &api.PassCriteria{Threshold: &threshold},
					Parameters:   map[string]any{"num_examples": 5},
				},
			},
		},
		Status: &api.EvaluationJobStatus{
			EvaluationJobState: api.EvaluationJobState{
				State: api.OverallStateCompleted,
				Message: &api.MessageInfo{
					Message:     "Evaluation job is completed",
					MessageCode: "evaluation.job.updated",
				},
			},
			Benchmarks: []api.BenchmarkStatus{
				{
					ID:             "arc_easy",
					ProviderID:     "lm_evaluation_harness",
					BenchmarkIndex: 0,
					Status:         api.StateCompleted,
				},
			},
		},
		Results: &api.EvaluationJobResults{
			Benchmarks: []api.BenchmarkResult{
				{
					ID:             "arc_easy",
					ProviderID:     "lm_evaluation_harness",
					BenchmarkIndex: 0,
					Metrics:        map[string]any{"accuracy": 0.95},
					Test: &api.BenchmarkTest{
						PrimaryScore:       0.95,
						PrimaryScoreMetric: "accuracy",
						Threshold:          0.3,
						Pass:               true,
					},
				},
			},
		},
	}

	card := NewEvaluationCard(job)
	if card == nil {
		t.Fatal("expected card")
	}
	if card.Metadata.EvaluationJobID != "job-123" {
		t.Fatalf("evaluation_job_id = %q", card.Metadata.EvaluationJobID)
	}
	if string(card.Metadata.CreatedAt) != "2026-07-07T00:00:00Z" {
		t.Fatalf("created_at = %q", card.Metadata.CreatedAt)
	}
	if string(card.Metadata.UpdatedAt) != "2026-07-07T01:00:00Z" {
		t.Fatalf("updated_at = %q", card.Metadata.UpdatedAt)
	}
	if card.Context.CollectionID != "" {
		t.Fatalf("collection_id = %q, want empty", card.Context.CollectionID)
	}
	if len(card.Context.Benchmarks) != 1 {
		t.Fatalf("context benchmarks len = %d, want 1", len(card.Context.Benchmarks))
	}
	if card.Context.Model.ModelCardURL != "https://example.com/model-card" {
		t.Fatalf("model_card_url = %q", card.Context.Model.ModelCardURL)
	}
	if len(card.Results.Benchmarks) != 1 {
		t.Fatalf("result benchmarks len = %d, want 1", len(card.Results.Benchmarks))
	}
	if card.Results.Benchmarks[0].Status != api.StateCompleted {
		t.Fatalf("status = %q", card.Results.Benchmarks[0].Status)
	}
	if card.Results.Benchmarks[0].Test.PrimaryScore != 0.95 {
		t.Fatalf("primary_score = %v", card.Results.Benchmarks[0].Test.PrimaryScore)
	}
	if card.Results.Status == nil {
		t.Fatal("expected overall job status")
	}
	if card.Results.Status.State != api.OverallStateCompleted {
		t.Fatalf("overall state = %q, want completed", card.Results.Status.State)
	}
	if card.Results.Status.Message == nil || card.Results.Status.Message.Message != "Evaluation job is completed" {
		t.Fatalf("overall message = %#v", card.Results.Status.Message)
	}
	if card.Results.Collection != nil {
		t.Fatal("expected no collection results for direct benchmark job")
	}
}

func TestNewEvaluationCardFromCollectionJob(t *testing.T) {
	threshold := float32(0.7)
	job := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{ID: "job-456"},
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{URL: "https://vllm.example.com/v1", Name: "model"},
			Collection: &api.CollectionRef{
				ID: "my-collection",
			},
		},
		Status: &api.EvaluationJobStatus{
			EvaluationJobState: api.EvaluationJobState{
				State: api.OverallStateCompleted,
				Message: &api.MessageInfo{
					Message:     "Evaluation job is completed",
					MessageCode: "evaluation.job.updated",
				},
			},
			Benchmarks: []api.BenchmarkStatus{
				{ID: "arc_easy", ProviderID: "lm_evaluation_harness", BenchmarkIndex: 0, Status: api.StateCompleted},
			},
		},
		Results: &api.EvaluationJobResults{
			Test: &api.EvaluationTest{Score: 0.8, Threshold: 0.7, Pass: true},
		},
	}

	card := NewEvaluationCard(job)
	if card.Context.CollectionID != "my-collection" {
		t.Fatalf("collection_id = %q", card.Context.CollectionID)
	}
	if len(card.Context.Benchmarks) != 0 {
		t.Fatalf("context benchmarks len = %d, want 0", len(card.Context.Benchmarks))
	}
	if card.Results.Collection == nil || card.Results.Collection.Test == nil {
		t.Fatal("expected collection test result")
	}
	if card.Results.Collection.Test.Score != 0.8 {
		t.Fatalf("collection score = %v", card.Results.Collection.Test.Score)
	}
	if card.Results.Collection.Test.Threshold != threshold {
		t.Fatalf("collection threshold = %v", card.Results.Collection.Test.Threshold)
	}
	if card.Results.Status == nil || card.Results.Status.State != api.OverallStateCompleted {
		t.Fatalf("overall status = %#v", card.Results.Status)
	}
}

func TestNewEvaluationCardPartiallyFailedJobStatus(t *testing.T) {
	job := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{ID: "job-789"},
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{URL: "https://vllm.example.com/v1", Name: "model"},
			Benchmarks: []api.EvaluationBenchmarkConfig{
				{Ref: api.Ref{ID: "arc_easy"}, ProviderID: "lm_evaluation_harness"},
				{Ref: api.Ref{ID: "arc_challenge"}, ProviderID: "lm_evaluation_harness"},
			},
		},
		Status: &api.EvaluationJobStatus{
			EvaluationJobState: api.EvaluationJobState{
				State: api.OverallStatePartiallyFailed,
				Message: &api.MessageInfo{
					Message:     "Some of the benchmarks failed.",
					MessageCode: "evaluation.job.updated",
				},
			},
			Benchmarks: []api.BenchmarkStatus{
				{ID: "arc_easy", ProviderID: "lm_evaluation_harness", BenchmarkIndex: 0, Status: api.StateCompleted},
				{
					ID:             "arc_challenge",
					ProviderID:     "lm_evaluation_harness",
					BenchmarkIndex: 0,
					Status:         api.StateFailed,
					ErrorMessage: &api.MessageInfo{
						Message:     "Benchmark run failed",
						MessageCode: "benchmark.failed",
					},
					WarningMessage: &api.MessageInfo{
						Message:     "Partial dataset used",
						MessageCode: "benchmark.warning",
					},
				},
			},
		},
		Results: &api.EvaluationJobResults{
			Benchmarks: []api.BenchmarkResult{
				{ID: "arc_easy", ProviderID: "lm_evaluation_harness", BenchmarkIndex: 0},
				{ID: "arc_challenge", ProviderID: "lm_evaluation_harness", BenchmarkIndex: 0},
			},
		},
	}

	card := NewEvaluationCard(job)
	if card.Results.Status.State != api.OverallStatePartiallyFailed {
		t.Fatalf("overall state = %q", card.Results.Status.State)
	}
	if card.Results.Status.Message.Message != "Some of the benchmarks failed." {
		t.Fatalf("message = %q", card.Results.Status.Message.Message)
	}
	if len(card.Results.Benchmarks) != 2 {
		t.Fatalf("benchmark results len = %d, want 2", len(card.Results.Benchmarks))
	}
	failed := card.Results.Benchmarks[1]
	if failed.ErrorMessage == nil || failed.ErrorMessage.Message != "Benchmark run failed" {
		t.Fatalf("error_message = %#v", failed.ErrorMessage)
	}
	if failed.WarningMessage == nil || failed.WarningMessage.Message != "Partial dataset used" {
		t.Fatalf("warning_message = %#v", failed.WarningMessage)
	}
}

func mustParseTime(t *testing.T, value string) (parsed time.Time) {
	t.Helper()
	parsed, err := api.DateTimeFromString(api.DateTime(value))
	if err != nil {
		t.Fatalf("parse time: %v", err)
	}
	return parsed
}

func TestNewEvaluationCardNilJob(t *testing.T) {
	if card := NewEvaluationCard(nil); card != nil {
		t.Fatalf("card = %#v, want nil", card)
	}
}

func TestNewEvaluationCardFromResultsWithoutStatusBenchmarks(t *testing.T) {
	job := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-results-only"}},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{URL: "https://vllm.example.com/v1", Name: "model"},
			Benchmarks: []api.EvaluationBenchmarkConfig{
				{Ref: api.Ref{ID: "arc_easy"}, ProviderID: "lm_evaluation_harness"},
			},
		},
		Results: &api.EvaluationJobResults{
			Benchmarks: []api.BenchmarkResult{
				{
					ID:             "arc_easy",
					ProviderID:     "lm_evaluation_harness",
					BenchmarkIndex: 0,
					Metrics:        map[string]any{"accuracy": 0.9},
					Test:           &api.BenchmarkTest{PrimaryScore: 0.9, Threshold: 0.5, Pass: true},
				},
			},
		},
	}

	card := NewEvaluationCard(job)
	if card.Results == nil || len(card.Results.Benchmarks) != 1 {
		t.Fatalf("results = %#v", card.Results)
	}
	if card.Results.Benchmarks[0].Status != "" {
		t.Fatalf("status = %q, want empty when built from results only", card.Results.Benchmarks[0].Status)
	}
	if card.Results.Benchmarks[0].Test.PrimaryScore != 0.9 {
		t.Fatalf("primary_score = %v", card.Results.Benchmarks[0].Test.PrimaryScore)
	}
}

func TestNewEvaluationCardStatusOnlyResults(t *testing.T) {
	job := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-status-only"}},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{URL: "https://vllm.example.com/v1", Name: "model"},
		},
		Status: &api.EvaluationJobStatus{
			EvaluationJobState: api.EvaluationJobState{
				State: api.OverallStateRunning,
				Message: &api.MessageInfo{
					Message:     "Evaluation job is running",
					MessageCode: "evaluation.job.updated",
				},
			},
		},
	}

	card := NewEvaluationCard(job)
	if card.Results == nil || card.Results.Status == nil {
		t.Fatal("expected results with overall status")
	}
	if card.Results.Status.State != api.OverallStateRunning {
		t.Fatalf("state = %q", card.Results.Status.State)
	}
	if len(card.Results.Benchmarks) != 0 {
		t.Fatalf("benchmarks = %#v, want empty", card.Results.Benchmarks)
	}
}
