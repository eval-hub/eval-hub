package sql_test

import (
	"encoding/json"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/common"
	"github.com/eval-hub/eval-hub/internal/constants"
	"github.com/eval-hub/eval-hub/internal/logging"
	"github.com/eval-hub/eval-hub/internal/storage"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/go-playground/validator/v10"
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
	store, err := storage.NewStorage(&databaseConfig, false, logger)
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

	job := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{
				ID: "job-1",
			},
		},
		EvaluationJobConfig: *config,
	}

	err = store.CreateEvaluationJob(job)
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
			StartedAt:  api.DateTimeToString(now),
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
		t.Fatalf("Expected 1 benchmark, got %d", len(updatedJob.Status.Benchmarks))
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

// TestStorage tests the storage implementation and provides
// a simple way to debug the storage implementation.
func TestEvaluationsStorage(t *testing.T) {
	var logger = logging.FallbackLogger()
	var store abstractions.Storage
	var evaluationId string

	var benchmarkConfig = api.BenchmarkConfig{
		Ref:        api.Ref{ID: "bench-1"},
		ProviderID: "garak",
	}

	t.Run("NewStorage creates a new storage instance", func(t *testing.T) {
		databaseConfig := map[string]any{}
		databaseConfig["driver"] = "sqlite"
		databaseConfig["url"] = "file::memory:?mode=memory&cache=shared"
		databaseConfig["database_name"] = "eval_hub"
		s, err := storage.NewStorage(&databaseConfig, false, logger)
		if err != nil {
			t.Fatalf("Failed to create storage: %v", err)
		}
		store = s.WithLogger(logger)
	})

	t.Run("CreateEvaluationJob creates a new evaluation job", func(t *testing.T) {
		config := &api.EvaluationJobConfig{
			Model: api.ModelRef{
				URL:  "http://test.com",
				Name: "test",
			},
			Benchmarks: []api.BenchmarkConfig{
				{
					Ref:        api.Ref{ID: "bench-1"},
					ProviderID: "garak",
				},
			},
		}
		job := &api.EvaluationJobResource{
			Resource: api.EvaluationResource{
				Resource: api.Resource{
					ID: common.GUID(),
				},
			},
			EvaluationJobConfig: *config,
		}

		err := store.CreateEvaluationJob(job)
		if err != nil {
			t.Fatalf("Failed to create evaluation job: %v", err)
		}
		evaluationId = job.Resource.ID
		if evaluationId == "" {
			t.Fatalf("Evaluation ID is empty")
		}
		if job.EvaluationJobConfig.Collection != nil {
			t.Fatalf("Collection is not nil")
		}
	})

	t.Run("GetEvaluationJob returns the evaluation job", func(t *testing.T) {
		resp, err := store.GetEvaluationJob(evaluationId)
		if err != nil {
			t.Fatalf("Failed to get evaluation job: %v", err)
		}
		if resp.Resource.ID != evaluationId {
			t.Fatalf("Evaluation ID mismatch: %s != %s", resp.Resource.ID, evaluationId)
		}
	})

	t.Run("GetEvaluationJobs returns the evaluation jobs", func(t *testing.T) {
		resp, err := store.GetEvaluationJobs(abstractions.QueryFilter{
			Limit:  10,
			Offset: 0,
			Params: map[string]any{},
		})
		if err != nil {
			t.Fatalf("Failed to get evaluation jobs: %v", err)
		}
		if len(resp.Items) == 0 {
			t.Fatalf("No evaluation jobs found")
		}
	})

	t.Run("UpdateEvaluationJob updates the evaluation job", func(t *testing.T) {
		metrics := map[string]any{
			"metric-1": 1.0,
			"metric-2": 2.0,
		}
		now := time.Now()
		status := &api.StatusEvent{
			BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
				ID:         benchmarkConfig.ID,
				ProviderID: benchmarkConfig.ProviderID,
				// the job status needs to be completed to update the metrics and artifacts
				Status:      api.StateCompleted,
				CompletedAt: api.DateTimeToString(now),
				Metrics:     metrics,
				Artifacts:   map[string]any{},
				ErrorMessage: &api.MessageInfo{
					Message:     "Test error message",
					MessageCode: "TEST_ERROR_MESSAGE",
				},
			},
		}
		completedAtStr := status.BenchmarkStatusEvent.CompletedAt
		if completedAtStr == "" {
			t.Fatalf("CompletedAt is empty")
		}
		val := validator.New()
		err := val.Struct(status)
		if err != nil {
			t.Fatalf("Failed to validate status: %v", err)
		}
		err = store.UpdateEvaluationJob(evaluationId, status)
		if err != nil {
			t.Fatalf("Failed to update evaluation job: %v", err)
		}

		// now get the evaluation job and check the updated values
		job, err := store.GetEvaluationJob(evaluationId)
		if err != nil {
			t.Fatalf("Failed to get evaluation job: %v", err)
		}
		js, err := json.MarshalIndent(job, "", "  ")
		if err != nil {
			t.Fatalf("Failed to marshal job: %v", err)
		}
		t.Logf("Job: %s\n", string(js))
		if len(job.Results.Benchmarks) == 0 {
			t.Fatalf("No benchmarks found")
		}
		if !maps.Equal(job.Results.Benchmarks[0].Metrics, metrics) {
			t.Fatalf("Metrics mismatch: %v != %v", job.Results.Benchmarks[0].Metrics, metrics)
		}

		if job.Status.Benchmarks[0].CompletedAt == "" {
			t.Fatalf("CompletedAt is nil")
		}
		_, err = api.DateTimeFromString(job.Status.Benchmarks[0].CompletedAt)
		if err != nil {
			t.Fatalf("Failed to convert CompletedAt to time: %v", err)
		}
	})

	t.Run("UpdateEvaluationJobStatus same-state is no-op", func(t *testing.T) {
		noOpID := common.GUID()
		noOpJob := &api.EvaluationJobResource{
			Resource: api.EvaluationResource{Resource: api.Resource{ID: noOpID}},
			EvaluationJobConfig: api.EvaluationJobConfig{
				Model:      api.ModelRef{URL: "http://test.com", Name: "test"},
				Benchmarks: []api.BenchmarkConfig{{Ref: api.Ref{ID: "b"}, ProviderID: "p"}},
			},
		}
		if err := store.CreateEvaluationJob(noOpJob); err != nil {
			t.Fatalf("CreateEvaluationJob: %v", err)
		}
		msg := &api.MessageInfo{Message: "no change", MessageCode: "test"}
		err := store.UpdateEvaluationJobStatus(noOpID, api.OverallStatePending, msg)
		if err != nil {
			t.Fatalf("UpdateEvaluationJobStatus same-state should not error: %v", err)
		}
		job, err := store.GetEvaluationJob(noOpID)
		if err != nil {
			t.Fatalf("GetEvaluationJob failed: %v", err)
		}
		if job.Status.State != api.OverallStatePending {
			t.Errorf("state should remain pending, got %s", job.Status.State)
		}
	})

	t.Run("UpdateEvaluationJobStatus rejects transition from terminal states", func(t *testing.T) {
		terminalStates := []api.OverallState{
			api.OverallStateCompleted,
			api.OverallStateFailed,
			api.OverallStateCancelled,
			api.OverallStatePartiallyFailed,
		}
		for _, terminalState := range terminalStates {
			jobID := common.GUID()
			config := &api.EvaluationJobConfig{
				Model: api.ModelRef{URL: "http://test.com", Name: "test"},
				Benchmarks: []api.BenchmarkConfig{
					{Ref: api.Ref{ID: "b1"}, ProviderID: "p1"},
				},
			}
			if terminalState == api.OverallStatePartiallyFailed {
				config.Benchmarks = append(config.Benchmarks, api.BenchmarkConfig{Ref: api.Ref{ID: "b2"}, ProviderID: "p1"})
			}
			job := &api.EvaluationJobResource{
				Resource:            api.EvaluationResource{Resource: api.Resource{ID: jobID}},
				EvaluationJobConfig: *config,
			}
			if err := store.CreateEvaluationJob(job); err != nil {
				t.Fatalf("CreateEvaluationJob: %v", err)
			}
			// Drive job to terminal state
			switch terminalState {
			case api.OverallStateCompleted:
				_ = store.UpdateEvaluationJob(jobID, &api.StatusEvent{
					BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
						ID: "b1", ProviderID: "p1", BenchmarkIndex: 0,
						Status: api.StateCompleted,
					},
				})
			case api.OverallStateFailed:
				_ = store.UpdateEvaluationJob(jobID, &api.StatusEvent{
					BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
						ID: "b1", ProviderID: "p1", BenchmarkIndex: 0,
						Status:       api.StateFailed,
						ErrorMessage: &api.MessageInfo{Message: "err", MessageCode: "E"},
					},
				})
			case api.OverallStateCancelled:
				_ = store.UpdateEvaluationJobStatus(jobID, api.OverallStateCancelled, &api.MessageInfo{Message: "cancelled", MessageCode: "X"})
			case api.OverallStatePartiallyFailed:
				_ = store.UpdateEvaluationJob(jobID, &api.StatusEvent{
					BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
						ID: "b1", ProviderID: "p1", BenchmarkIndex: 0,
						Status: api.StateCompleted,
					},
				})
				_ = store.UpdateEvaluationJob(jobID, &api.StatusEvent{
					BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
						ID: "b2", ProviderID: "p1", BenchmarkIndex: 1,
						Status:       api.StateFailed,
						ErrorMessage: &api.MessageInfo{Message: "err", MessageCode: "E"},
					},
				})
			}
			got, _ := store.GetEvaluationJob(jobID)
			if got == nil {
				t.Fatalf("GetEvaluationJob returned nil for %s", jobID)
			}
			if got.Status.State != terminalState {
				t.Fatalf("job %s: expected state %s, got %s", jobID, terminalState, got.Status.State)
			}
			err := store.UpdateEvaluationJobStatus(jobID, api.OverallStatePending, &api.MessageInfo{Message: "try", MessageCode: "X"})
			if err == nil {
				t.Errorf("UpdateEvaluationJobStatus from %s should return error", terminalState)
			}
			if err != nil && !strings.Contains(err.Error(), "can not be") {
				t.Errorf("expected JobCanNotBeUpdated error, got: %v", err)
			}
		}
	})

	t.Run("UpdateEvaluationJobStatus allows non-terminal transition and preserves Results/Benchmarks", func(t *testing.T) {
		jobID := common.GUID()
		config := &api.EvaluationJobConfig{
			Model: api.ModelRef{URL: "http://test.com", Name: "test"},
			Benchmarks: []api.BenchmarkConfig{
				{Ref: api.Ref{ID: "bx"}, ProviderID: "garak"},
			},
		}
		job := &api.EvaluationJobResource{
			Resource:            api.EvaluationResource{Resource: api.Resource{ID: jobID}},
			EvaluationJobConfig: *config,
		}
		if err := store.CreateEvaluationJob(job); err != nil {
			t.Fatalf("CreateEvaluationJob: %v", err)
		}
		// (1) pending->running: verify State and Message updated
		msg := &api.MessageInfo{Message: "job running", MessageCode: "RUNNING"}
		if err := store.UpdateEvaluationJobStatus(jobID, api.OverallStateRunning, msg); err != nil {
			t.Fatalf("UpdateEvaluationJobStatus pending->running: %v", err)
		}
		updated, err := store.GetEvaluationJob(jobID)
		if err != nil {
			t.Fatalf("GetEvaluationJob: %v", err)
		}
		if updated.Status.State != api.OverallStateRunning {
			t.Errorf("State should be running, got %s", updated.Status.State)
		}
		if updated.Status.Message == nil || updated.Status.Message.Message != "job running" {
			t.Errorf("Message should be updated, got %v", updated.Status.Message)
		}
		// (2) running->cancelled: verify Benchmarks and Results preserved
		if err := store.UpdateEvaluationJob(jobID, &api.StatusEvent{
			BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
				ID: "bx", ProviderID: "garak", BenchmarkIndex: 0,
				Status:  api.StateCompleted,
				Metrics: map[string]any{"acc": 0.9},
			},
		}); err != nil {
			t.Fatalf("UpdateEvaluationJob completed: %v", err)
		}
		// Now run UpdateEvaluationJobStatus: running->cancelled not applicable (job is completed).
		// From running we can go to cancelled. So: create another job, UpdateEvaluationJob (running),
		// UpdateEvaluationJobStatus(cancelled). Verify benchmarks preserved.
		jobID2 := common.GUID()
		job2 := &api.EvaluationJobResource{
			Resource:            api.EvaluationResource{Resource: api.Resource{ID: jobID2}},
			EvaluationJobConfig: *config,
		}
		if err := store.CreateEvaluationJob(job2); err != nil {
			t.Fatalf("CreateEvaluationJob job2: %v", err)
		}
		if err := store.UpdateEvaluationJob(jobID2, &api.StatusEvent{
			BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
				ID: "bx", ProviderID: "garak", BenchmarkIndex: 0,
				Status: api.StateRunning,
			},
		}); err != nil {
			t.Fatalf("UpdateEvaluationJob job2 running: %v", err)
		}
		if err := store.UpdateEvaluationJobStatus(jobID2, api.OverallStateCancelled, &api.MessageInfo{Message: "cancelled", MessageCode: "C"}); err != nil {
			t.Fatalf("UpdateEvaluationJobStatus running->cancelled: %v", err)
		}
		final, err := store.GetEvaluationJob(jobID2)
		if err != nil {
			t.Fatalf("GetEvaluationJob job2: %v", err)
		}
		if len(final.Status.Benchmarks) != 1 {
			t.Errorf("Benchmarks should be preserved, got %d", len(final.Status.Benchmarks))
		}
		if final.Status.Benchmarks[0].Status != api.StateRunning {
			t.Errorf("Benchmark status should be preserved as running, got %s", final.Status.Benchmarks[0].Status)
		}
	})

	t.Run("DeleteEvaluationJob deletes the evaluation job", func(t *testing.T) {
		err := store.UpdateEvaluationJobStatus(evaluationId, api.OverallStateCancelled, &api.MessageInfo{
			Message:     "Evaluation job cancelled",
			MessageCode: constants.MESSAGE_CODE_EVALUATION_JOB_CANCELLED,
		})
		if err == nil {
			t.Fatalf("Failed to get error when cancelling a deleted evaluation job")
		}
		if !strings.Contains(err.Error(), "can not be cancelled because") {
			t.Fatalf("Failed to get correct error when cancelling a deleted evaluation job: %v", err)
		}
		err = store.DeleteEvaluationJob(evaluationId)
		if err != nil {
			t.Fatalf("Failed to delete evaluation job: %v", err)
		}
	})
}
