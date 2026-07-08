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
				CreatedAt: mustParseTime(t, "2026-07-07T00:00:00Z"),
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
	if card.Results.Benchmarks[0].Test.PrimaryScore != "0.95" {
		t.Fatalf("primary_score = %q", card.Results.Benchmarks[0].Test.PrimaryScore)
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
}

func mustParseTime(t *testing.T, value string) (parsed time.Time) {
	t.Helper()
	parsed, err := api.DateTimeFromString(api.DateTime(value))
	if err != nil {
		t.Fatalf("parse time: %v", err)
	}
	return parsed
}
