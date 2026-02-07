package sql_test

import (
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/logging"
	"github.com/eval-hub/eval-hub/internal/storage"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// TestUpdateEvaluationJob_PreservesProviderID verifies that provider_id is
// preserved when creating benchmark statuses via status updates.
//
// Regression test for: provider_id was empty in results because the fallback
// path in findAndUpdateBenchmarkStatus didn't preserve it from the status event.
func TestUpdateEvaluationJob_PreservesProviderID(t *testing.T) {
	// Setup storage
	logger := logging.FallbackLogger()
	databaseConfig := map[string]any{
		"driver":        "sqlite",
		"url":           "file::memory:?mode=memory&cache=shared",
		"database_name": "eval_hub",
	}
	store, err := storage.NewStorage(&databaseConfig, logger)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	// Create job without initializing benchmark statuses
	// (simulating old behavior before initialization was added)
	config := &api.EvaluationJobConfig{
		Model: api.ModelRef{
			URL:  "http://test-model:8000",
			Name: "test-model",
		},
		Benchmarks: []api.BenchmarkConfig{
			{
				Ref: api.Ref{
					ID: "arc_easy",
				},
				ProviderID: "lm_evaluation_harness",
			},
		},
	}

	job, err := store.CreateEvaluationJob(config, "")
	if err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	now := time.Now()
	// Send status update with provider_id (simulating SDK behavior)
	statusUpdate := &api.StatusEvent{
		BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
			ProviderID: "lm_evaluation_harness",
			ID:         "arc_easy",
			Status:     api.StateRunning,
			StartedAt:  &now,
			Metrics: map[string]any{
				"acc":      0.85,
				"acc_norm": 0.87,
			},
		},
	}

	err = store.UpdateEvaluationJob(job.Resource.ID, statusUpdate)
	if err != nil {
		t.Fatalf("Failed to update job: %v", err)
	}

	// Verify provider_id was preserved in status
	updatedJob, err := store.GetEvaluationJob(job.Resource.ID)
	if err != nil {
		t.Fatalf("Failed to get updated job: %v", err)
	}

	if len(updatedJob.Status.Benchmarks) != 1 {
		t.Fatalf("Expected 1 benchmark, got %d", len(updatedJob.Results.Benchmarks))
	}

	benchmark := updatedJob.Results.Benchmarks[0]
	if benchmark.ProviderID != "lm_evaluation_harness" {
		t.Errorf("Expected provider_id=%q, got %q",
			"lm_evaluation_harness", benchmark.ProviderID)
	}

	// Send completion update with results
	completionUpdate := &api.StatusEvent{
		BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
			ProviderID: "lm_evaluation_harness",
			ID:         "arc_easy",
			Status:     api.StateCompleted,
			Metrics: map[string]any{
				"acc":      0.85,
				"acc_norm": 0.87,
			},
		},
	}

	err = store.UpdateEvaluationJob(job.Resource.ID, completionUpdate)
	if err != nil {
		t.Fatalf("Failed to update job with results: %v", err)
	}

	// Verify provider_id is preserved in results
	finalJob, err := store.GetEvaluationJob(job.Resource.ID)
	if err != nil {
		t.Fatalf("Failed to get final job: %v", err)
	}

	if len(finalJob.Results.Benchmarks) != 1 {
		t.Fatalf("Expected 1 benchmark in results, got %d", len(finalJob.Results.Benchmarks))
	}

	result := finalJob.Results.Benchmarks[0]
	if result.ProviderID != "lm_evaluation_harness" {
		t.Errorf("Expected provider_id=%q in results, got %q",
			"lm_evaluation_harness", result.ProviderID)
	}

	// Verify metrics were also stored
	if result.Metrics == nil {
		t.Fatal("Expected metrics to be stored, got nil")
	}

	if acc, ok := result.Metrics["acc"].(float64); !ok || acc != 0.85 {
		t.Errorf("Expected acc=0.85, got %v", result.Metrics["acc"])
	}
}
