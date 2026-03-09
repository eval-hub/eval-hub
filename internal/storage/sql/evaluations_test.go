package sql_test

import (
	"encoding/json"
	"fmt"
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

var (
	dbIndex = 0
)

func getDBName() string {
	dbIndex++
	return fmt.Sprintf("eval_hub_tenant_test_%d", dbIndex)
}

func getDBInMemoryURL() string {
	// we want each test to use a unique in-memory database
	return fmt.Sprintf("file:%s?mode=memory&cache=shared", getDBName())
}

// TestGetEvaluationJobs_TenantFilter verifies that WithTenant scopes list results
// to only the jobs belonging to that tenant.
func TestGetEvaluationJobs_TenantFilter(t *testing.T) {
	logger := logging.FallbackLogger()
	databaseConfig := map[string]any{
		"driver":        "sqlite",
		"url":           getDBInMemoryURL(),
		"database_name": "eval_hub_tenant_test",
	}
	store, err := storage.NewStorage(&databaseConfig, false, false, logger)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	now := time.Now()
	makeJob := func(id, tenant string) *api.EvaluationJobResource {
		return &api.EvaluationJobResource{
			Resource: api.EvaluationResource{
				Resource: api.Resource{
					ID:        id,
					Tenant:    api.Tenant(tenant),
					CreatedAt: now,
					UpdatedAt: now,
				},
				MLFlowExperimentID: "exp-1",
			},
			Status: &api.EvaluationJobStatus{
				EvaluationJobState: api.EvaluationJobState{State: api.OverallStatePending},
			},
			EvaluationJobConfig: api.EvaluationJobConfig{
				Model:      api.ModelRef{URL: "http://model", Name: "m"},
				Benchmarks: []api.BenchmarkConfig{{Ref: api.Ref{ID: "b"}, ProviderID: "p"}},
			},
		}
	}

	if err := store.CreateEvaluationJob(makeJob("job-team-a-1", "team-a")); err != nil {
		t.Fatalf("create job-team-a-1: %v", err)
	}
	if err := store.CreateEvaluationJob(makeJob("job-team-a-2", "team-a")); err != nil {
		t.Fatalf("create job-team-a-2: %v", err)
	}
	if err := store.CreateEvaluationJob(makeJob("job-team-b-1", "team-b")); err != nil {
		t.Fatalf("create job-team-b-1: %v", err)
	}

	filter := &abstractions.QueryFilter{Limit: 50, Offset: 0, Params: map[string]any{}}

	t.Run("team-a sees only its own jobs", func(t *testing.T) {
		res, err := store.WithTenant(api.Tenant("team-a")).GetEvaluationJobs(filter)
		if err != nil {
			t.Fatalf("GetEvaluationJobs: %v", err)
		}
		if len(res.Items) != 2 {
			t.Errorf("expected 2 jobs for team-a, got %d", len(res.Items))
		}
		for _, j := range res.Items {
			if j.Resource.Tenant != "team-a" {
				t.Errorf("unexpected tenant %q in result", j.Resource.Tenant)
			}
		}
	})

	t.Run("team-b sees only its own jobs", func(t *testing.T) {
		res, err := store.WithTenant(api.Tenant("team-b")).GetEvaluationJobs(filter)
		if err != nil {
			t.Fatalf("GetEvaluationJobs: %v", err)
		}
		if len(res.Items) != 1 {
			t.Errorf("expected 1 job for team-b, got %d", len(res.Items))
		}
		if res.Items[0].Resource.ID != "job-team-b-1" {
			t.Errorf("expected job-team-b-1, got %q", res.Items[0].Resource.ID)
		}
	})

	t.Run("unknown tenant sees no jobs", func(t *testing.T) {
		res, err := store.WithTenant(api.Tenant("team-c")).GetEvaluationJobs(filter)
		if err != nil {
			t.Fatalf("GetEvaluationJobs: %v", err)
		}
		if len(res.Items) != 0 {
			t.Errorf("expected 0 jobs for team-c, got %d", len(res.Items))
		}
	})
}

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
		"url":           getDBInMemoryURL(),
		"database_name": "eval_hub",
	}
	store, err := storage.NewStorage(&databaseConfig, false, false, logger)
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

	now := time.Now()

	job := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{
				ID:        "job-1",
				Tenant:    api.Tenant("tenant-1"),
				CreatedAt: now,
				UpdatedAt: now,
			},
			MLFlowExperimentID: "experiment-1",
		},
		Status: &api.EvaluationJobStatus{
			EvaluationJobState: api.EvaluationJobState{
				State: api.OverallStateRunning,
				Message: &api.MessageInfo{
					Message:     "Job is running",
					MessageCode: "JOB_RUNNING",
				},
			},
		},
		EvaluationJobConfig: *config,
	}

	err = store.CreateEvaluationJob(job)
	if err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

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
		databaseConfig["url"] = getDBInMemoryURL()
		databaseConfig["database_name"] = "eval_hub"
		s, err := storage.NewStorage(&databaseConfig, false, false, logger)
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

		now := time.Now()

		job := &api.EvaluationJobResource{
			Resource: api.EvaluationResource{
				Resource: api.Resource{
					ID:        common.GUID(),
					Tenant:    api.Tenant("tenant-1"),
					CreatedAt: now,
					UpdatedAt: now,
				},
				MLFlowExperimentID: "experiment-1",
			},
			Status: &api.EvaluationJobStatus{
				EvaluationJobState: api.EvaluationJobState{
					State: api.OverallStateRunning,
					Message: &api.MessageInfo{
						Message:     "Job is running",
						MessageCode: "JOB_RUNNING",
					},
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
		resp, err := store.GetEvaluationJobs(&abstractions.QueryFilter{
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

	t.Run("GetEvaluationJobs returns empty list when no pending evaluation jobs are found", func(t *testing.T) {
		resp, err := store.GetEvaluationJobs(&abstractions.QueryFilter{
			Limit:  10,
			Offset: 0,
			Params: map[string]any{"status": "pending"},
		})
		if err != nil {
			t.Fatalf("unexpected error getting evaluation jobs: %v", err)
		}
		if resp.TotalCount != 0 {
			t.Fatalf("Expected 0 evaluation jobs, got %d: %s", resp.TotalCount, prettyPrint(resp))
		}
	})

	t.Run("GetEvaluationJobs returns 1 item querying running evaluation jobs", func(t *testing.T) {
		resp, err := store.GetEvaluationJobs(&abstractions.QueryFilter{
			Limit:  10,
			Offset: 0,
			Params: map[string]any{"status": "running"},
		})
		if err != nil {
			t.Fatalf("unexpected error getting evaluation jobs: %v", err)
		}
		if resp.TotalCount != 1 {
			t.Fatalf("Expected 1 evaluation jobs, got %d", resp.TotalCount)
		}
		if len(resp.Items) != 1 {
			t.Fatalf("Expected 1 evaluation job, got %d", len(resp.Items))
		}
		if resp.Items[0].Status.State != api.OverallStateRunning {
			t.Fatalf("Expected running evaluation job, got %s", resp.Items[0].Status.State)
		}
	})

	t.Run("GetEvaluationJobs rejects disallowed filter columns", func(t *testing.T) {
		_, err := store.GetEvaluationJobs(&abstractions.QueryFilter{
			Limit:  10,
			Offset: 0,
			Params: map[string]any{"name": "test", "evil_column": "x"},
		})
		if err == nil {
			t.Fatal("expected error when using disallowed filter columns")
		}
		if !strings.Contains(err.Error(), "is not a valid query parameter") {
			t.Errorf("expected error to mention 'is not a valid query parameter', got: %v", err)
		}
		if !strings.Contains(err.Error(), "name") || !strings.Contains(err.Error(), "evil_column") {
			t.Errorf("expected error to include offending key names, got: %v", err)
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
		now := time.Now()

		noOpJob := &api.EvaluationJobResource{
			Resource: api.EvaluationResource{
				Resource: api.Resource{
					ID:        noOpID,
					Tenant:    api.Tenant("tenant-1"),
					CreatedAt: now,
					UpdatedAt: now,
				},
				MLFlowExperimentID: "experiment-1",
			},
			Status: &api.EvaluationJobStatus{
				EvaluationJobState: api.EvaluationJobState{
					State: api.OverallStateRunning,
					Message: &api.MessageInfo{
						Message:     "Job is running",
						MessageCode: "JOB_RUNNING",
					},
				},
			},
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
			now := time.Now()
			job := &api.EvaluationJobResource{
				Resource: api.EvaluationResource{
					Resource: api.Resource{
						ID:        jobID,
						Tenant:    api.Tenant("tenant-1"),
						CreatedAt: now,
						UpdatedAt: now,
					},
					MLFlowExperimentID: "experiment-1",
				},
				Status: &api.EvaluationJobStatus{
					EvaluationJobState: api.EvaluationJobState{
						State: api.OverallStateRunning,
						Message: &api.MessageInfo{
							Message:     "Job is running",
							MessageCode: "JOB_RUNNING",
						},
					},
				},
				EvaluationJobConfig: *config,
			}
			if err := store.CreateEvaluationJob(job); err != nil {
				t.Fatalf("CreateEvaluationJob: %v", err)
			}
			// Drive job to terminal state
			switch terminalState {
			case api.OverallStateCompleted:
				if err := store.UpdateEvaluationJob(jobID, &api.StatusEvent{
					BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
						ID: "b1", ProviderID: "p1", BenchmarkIndex: 0,
						Status: api.StateCompleted,
					},
				}); err != nil {
					t.Fatalf("setup for %s: %v", terminalState, err)
				}
			case api.OverallStateFailed:
				if err := store.UpdateEvaluationJob(jobID, &api.StatusEvent{
					BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
						ID: "b1", ProviderID: "p1", BenchmarkIndex: 0,
						Status:       api.StateFailed,
						ErrorMessage: &api.MessageInfo{Message: "err", MessageCode: "E"},
					},
				}); err != nil {
					t.Fatalf("setup for %s: %v", terminalState, err)
				}
			case api.OverallStateCancelled:
				if err := store.UpdateEvaluationJobStatus(jobID, api.OverallStateCancelled, &api.MessageInfo{Message: "cancelled", MessageCode: "X"}); err != nil {
					t.Fatalf("setup for %s: %v", terminalState, err)
				}
			case api.OverallStatePartiallyFailed:
				if err := store.UpdateEvaluationJob(jobID, &api.StatusEvent{
					BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
						ID: "b1", ProviderID: "p1", BenchmarkIndex: 0,
						Status: api.StateCompleted,
					},
				}); err != nil {
					t.Fatalf("setup for %s (b1): %v", terminalState, err)
				}
				if err := store.UpdateEvaluationJob(jobID, &api.StatusEvent{
					BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
						ID: "b2", ProviderID: "p1", BenchmarkIndex: 1,
						Status:       api.StateFailed,
						ErrorMessage: &api.MessageInfo{Message: "err", MessageCode: "E"},
					},
				}); err != nil {
					t.Fatalf("setup for %s (b2): %v", terminalState, err)
				}
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
		now := time.Now()
		job := &api.EvaluationJobResource{
			Resource: api.EvaluationResource{
				Resource: api.Resource{
					ID:        jobID,
					Tenant:    api.Tenant("tenant-1"),
					CreatedAt: now,
					UpdatedAt: now,
				},
				MLFlowExperimentID: "experiment-1",
			},
			Status: &api.EvaluationJobStatus{
				EvaluationJobState: api.EvaluationJobState{
					State: api.OverallStatePending,
					Message: &api.MessageInfo{
						Message:     "Job pending",
						MessageCode: "JOB_PENDING",
					},
				},
			},
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
			Resource: api.EvaluationResource{
				Resource: api.Resource{
					ID:        jobID2,
					Tenant:    api.Tenant("tenant-1"),
					CreatedAt: now,
					UpdatedAt: now,
				},
				MLFlowExperimentID: "experiment-1",
			},
			Status: &api.EvaluationJobStatus{
				EvaluationJobState: api.EvaluationJobState{
					State: api.OverallStateRunning,
					Message: &api.MessageInfo{
						Message:     "Job is running",
						MessageCode: "JOB_RUNNING",
					},
				},
			},
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
		if final.Status.Benchmarks[0].Status != api.StateCancelled {
			t.Errorf("Benchmark status should be cancelled, got %s", final.Status.Benchmarks[0].Status)
		}
		if final.Status.Benchmarks[0].ErrorMessage == nil {
			t.Fatal("Benchmark error_message should be set after cancellation")
		}
		if final.Status.Benchmarks[0].ErrorMessage.Message != "cancelled" {
			t.Errorf("Benchmark error_message.message should be 'cancelled', got %s", final.Status.Benchmarks[0].ErrorMessage.Message)
		}
		if final.Status.Benchmarks[0].ErrorMessage.MessageCode != "C" {
			t.Errorf("Benchmark error_message.message_code should be 'C', got %s", final.Status.Benchmarks[0].ErrorMessage.MessageCode)
		}
	})

	t.Run("CancelEvaluationJob cascades only to non-terminal benchmarks", func(t *testing.T) {
		jobID := common.GUID()
		config := &api.EvaluationJobConfig{
			Model: api.ModelRef{URL: "http://test.com", Name: "test"},
			Benchmarks: []api.BenchmarkConfig{
				{Ref: api.Ref{ID: "b1"}, ProviderID: "prov1"},
				{Ref: api.Ref{ID: "b2"}, ProviderID: "prov2"},
				{Ref: api.Ref{ID: "b3"}, ProviderID: "prov3"},
			},
		}
		now := time.Now()
		job := &api.EvaluationJobResource{
			Resource: api.EvaluationResource{
				Resource: api.Resource{
					ID:        jobID,
					Tenant:    api.Tenant("tenant-1"),
					CreatedAt: now,
					UpdatedAt: now,
				},
			},
			Status: &api.EvaluationJobStatus{
				EvaluationJobState: api.EvaluationJobState{
					State: api.OverallStateRunning,
				},
			},
			EvaluationJobConfig: *config,
		}
		if err := store.CreateEvaluationJob(job); err != nil {
			t.Fatalf("CreateEvaluationJob: %v", err)
		}
		// Set benchmark 0 to running, benchmark 1 to completed, benchmark 2 to pending
		if err := store.UpdateEvaluationJob(jobID, &api.StatusEvent{
			BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
				ID: "b1", ProviderID: "prov1", BenchmarkIndex: 0,
				Status: api.StateRunning,
			},
		}); err != nil {
			t.Fatalf("UpdateEvaluationJob b1 running: %v", err)
		}
		if err := store.UpdateEvaluationJob(jobID, &api.StatusEvent{
			BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
				ID: "b2", ProviderID: "prov2", BenchmarkIndex: 1,
				Status:  api.StateCompleted,
				Metrics: map[string]any{"acc": 0.95},
			},
		}); err != nil {
			t.Fatalf("UpdateEvaluationJob b2 completed: %v", err)
		}
		if err := store.UpdateEvaluationJob(jobID, &api.StatusEvent{
			BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
				ID: "b3", ProviderID: "prov3", BenchmarkIndex: 2,
				Status: api.StatePending,
			},
		}); err != nil {
			t.Fatalf("UpdateEvaluationJob b3 pending: %v", err)
		}

		cancelMsg := &api.MessageInfo{
			Message:     "Evaluation job cancelled",
			MessageCode: constants.MESSAGE_CODE_EVALUATION_JOB_CANCELLED,
		}
		if err := store.UpdateEvaluationJobStatus(jobID, api.OverallStateCancelled, cancelMsg); err != nil {
			t.Fatalf("UpdateEvaluationJobStatus running->cancelled: %v", err)
		}
		final, err := store.GetEvaluationJob(jobID)
		if err != nil {
			t.Fatalf("GetEvaluationJob: %v", err)
		}
		if len(final.Status.Benchmarks) != 3 {
			t.Fatalf("Expected 3 benchmarks, got %d", len(final.Status.Benchmarks))
		}
		// b1 was running → should be cancelled with error message
		if final.Status.Benchmarks[0].Status != api.StateCancelled {
			t.Errorf("b1 should be cancelled, got %s", final.Status.Benchmarks[0].Status)
		}
		if final.Status.Benchmarks[0].ErrorMessage == nil || final.Status.Benchmarks[0].ErrorMessage.MessageCode != constants.MESSAGE_CODE_EVALUATION_JOB_CANCELLED {
			t.Errorf("b1 should have cancellation error message")
		}
		// b2 was completed → should remain completed
		if final.Status.Benchmarks[1].Status != api.StateCompleted {
			t.Errorf("b2 should remain completed, got %s", final.Status.Benchmarks[1].Status)
		}
		if final.Status.Benchmarks[1].ErrorMessage != nil {
			t.Errorf("b2 should not have error message, got %v", final.Status.Benchmarks[1].ErrorMessage)
		}
		// b3 was pending → should be cancelled with error message
		if final.Status.Benchmarks[2].Status != api.StateCancelled {
			t.Errorf("b3 should be cancelled, got %s", final.Status.Benchmarks[2].Status)
		}
		if final.Status.Benchmarks[2].ErrorMessage == nil || final.Status.Benchmarks[2].ErrorMessage.MessageCode != constants.MESSAGE_CODE_EVALUATION_JOB_CANCELLED {
			t.Errorf("b3 should have cancellation error message")
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

func prettyPrint(v any) string {
	json, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(json)
}
