package cards

import (
	"encoding/json"
	"testing"

	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestEvaluationCardJSONRoundTrip(t *testing.T) {
	threshold := float32(0.3)
	card := EvaluationCard{
		CardVersion:   CardVersion,
		SchemaVersion: SchemaVersion,
		Metadata: EvaluationCardMetadata{
			EvaluationJobID: "job-123",
			CreatedAt:       "2026-07-07T00:00:00Z",
			UpdatedAt:       "2026-07-07T01:00:00Z",
		},
		Context: EvaluationCardContext{
			Model: CardModelRef{
				URL:  "https://vllm.example.com/v1",
				Name: "meta-llama/Llama-3.2-1B-Instruct",
			},
			Benchmarks: []CardBenchmarkConfig{
				{
					ID:         "arc_easy",
					ProviderID: "lm_evaluation_harness",
					Parameters: map[string]any{
						"num_examples": float64(5),
						"tokenizer":    "google/flan-t5-small",
					},
					PrimaryScore: &api.PrimaryScore{
						Metric:        "accuracy",
						LowerIsBetter: false,
					},
					PassCriteria: &api.PassCriteria{Threshold: &threshold},
					Weight:       0.6,
				},
			},
		},
		Results: &EvaluationCardResults{
			Benchmarks: []CardBenchmarkResult{
				{
					ID:         "arc_easy",
					ProviderID: "lm_evaluation_harness",
					Contacts:   []string{"team@example.com"},
					Status:     api.StateCompleted,
					Metrics: map[string]any{
						"accuracy": 0.95,
					},
					AdditionalInfo: map[string]any{
						"dataset_sha": "abc123",
					},
					Test: &CardBenchmarkTest{
						PrimaryScore: "0.95",
						Threshold:    "0.3",
						Pass:         true,
					},
				},
			},
			Collection: &CardCollectionResult{
				Test: &CardCollectionTest{
					Score:     0.80,
					Threshold: 0.70,
					Pass:      true,
				},
			},
		},
	}

	data, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded EvaluationCard
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.CardVersion != CardVersion {
		t.Errorf("card_version = %q, want %q", decoded.CardVersion, CardVersion)
	}
	if decoded.Context.Model.Name != card.Context.Model.Name {
		t.Errorf("model name = %q, want %q", decoded.Context.Model.Name, card.Context.Model.Name)
	}
	if len(decoded.Context.Benchmarks) != 1 {
		t.Fatalf("benchmarks len = %d, want 1", len(decoded.Context.Benchmarks))
	}
	if decoded.Context.Benchmarks[0].PrimaryScore.Metric != "accuracy" {
		t.Errorf("primary_score.metric = %q, want accuracy", decoded.Context.Benchmarks[0].PrimaryScore.Metric)
	}
	if decoded.Results.Benchmarks[0].Test.Pass != true {
		t.Error("benchmark test pass = false, want true")
	}
	if decoded.Results.Collection.Test.Score != 0.80 {
		t.Errorf("collection score = %v, want 0.80", decoded.Results.Collection.Test.Score)
	}
}
