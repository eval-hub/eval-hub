package sql_test

import (
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/common"
	"github.com/eval-hub/eval-hub/internal/eval_hub/constants"
	"github.com/eval-hub/eval-hub/internal/eval_hub/storage/sql"
	"github.com/eval-hub/eval-hub/internal/eval_hub/validation"
	"github.com/eval-hub/eval-hub/pkg/api"
)

var (
	drivers = []string{"sqlite", "postgres"}
)

// TestGetEvaluationJobs_TenantFilter verifies that WithTenant scopes list results
// to only the jobs belonging to that tenant.
func TestGetEvaluationJobs_TenantFilter(t *testing.T) {
	testGetEvaluationJobs_TenantFilter(t, drivers[0], getDBName())
}

func TestUpdateEvaluationJob_PreservesProviderID(t *testing.T) {
	testUpdateEvaluationJob_PreservesProviderID(t, drivers[0], getDBName())
}

// TestStorage tests the storage implementation and provides
// a simple way to debug the storage implementation.
func TestEvaluationsStorage(t *testing.T) {
	testEvaluationsStorage(t, drivers[0], getDBName())
}

func TestGetEvaluationJobs_Postgres(t *testing.T) {
	image := false
	databaseName := getDBName()
	user, err := getPostgresUser()
	if err != nil {
		t.Skipf("Failed to get Postgres user: %v", err)
	}
	if err := startPostgres(t, databaseName, user, image); err != nil {
		t.Skipf("Skipping postgres tests: %v", err)
	}

	// we need to stop postgres after the test finishes
	t.Cleanup(func() {
		stopPostgres(t, databaseName, user, image)
	})

	testGetEvaluationJobs_TenantFilter(t, drivers[1], databaseName)
	testUpdateEvaluationJob_PreservesProviderID(t, drivers[1], databaseName)
	testEvaluationsStorage(t, drivers[1], databaseName)
}

func testGetEvaluationJobs_TenantFilter(t *testing.T, driver string, databaseName string) {
	store, err := getTestStorage(t, driver, databaseName)
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
				Benchmarks: []api.EvaluationBenchmarkConfig{{Ref: api.Ref{ID: "b"}, ProviderID: "p"}},
			},
		}
	}

	tenantA := getTenant("team-a")
	tenantB := getTenant("team-b")

	jobID1 := common.GUID()
	if err := store.CreateEvaluationJob(makeJob(jobID1, tenantA)); err != nil {
		t.Fatalf("create job-team-a-1: %v", err)
	}
	jobID2 := common.GUID()
	if err := store.CreateEvaluationJob(makeJob(jobID2, tenantA)); err != nil {
		t.Fatalf("create job-team-a-2: %v", err)
	}
	jobID3 := common.GUID()
	if err := store.CreateEvaluationJob(makeJob(jobID3, tenantB)); err != nil {
		t.Fatalf("create job-team-b-1: %v", err)
	}

	filter := &abstractions.QueryFilter{Limit: 50, Offset: 0, Params: map[string]any{}}

	t.Run("team-a sees only its own jobs", func(t *testing.T) {
		res, err := store.WithTenant(api.Tenant(tenantA)).GetEvaluationJobs(filter)
		if err != nil {
			t.Fatalf("GetEvaluationJobs: %v", err)
		}
		if len(res.Items) != 2 {
			t.Fatalf("expected 2 jobs for team-a, got %d", len(res.Items))
		}
		for _, j := range res.Items {
			if j.Resource.Tenant.String() != tenantA {
				t.Fatalf("unexpected tenant %q in result", j.Resource.Tenant)
			}
		}
	})

	t.Run("team-b sees only its own jobs", func(t *testing.T) {
		res, err := store.WithTenant(api.Tenant(tenantB)).GetEvaluationJobs(filter)
		if err != nil {
			t.Fatalf("GetEvaluationJobs: %v", err)
		}
		if len(res.Items) != 1 {
			t.Fatalf("expected 1 job for team-b, got %d", len(res.Items))
		}
		if res.Items[0].Resource.ID != jobID3 {
			t.Fatalf("expected job-team-b-1, got %q", res.Items[0].Resource.ID)
		}
	})

	t.Run("unknown tenant sees no jobs", func(t *testing.T) {
		res, err := store.WithTenant(api.Tenant(getTenant("team-c"))).GetEvaluationJobs(filter)
		if err != nil {
			t.Fatalf("GetEvaluationJobs: %v", err)
		}
		if len(res.Items) != 0 {
			t.Fatalf("expected 0 jobs for team-c, got %d", len(res.Items))
		}
	})
}

// TestUpdateEvaluationJob_PreservesProviderID verifies that provider_id is
// preserved when creating benchmark statuses via status updates.
//
// Regression test for: provider_id was empty in results because the fallback
// path in findAndUpdateBenchmarkStatus didn't preserve it from the status event.
func testUpdateEvaluationJob_PreservesProviderID(t *testing.T, driver string, databaseName string) {
	// Setup storage
	store, err := getTestStorage(t, driver, databaseName)
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
		Benchmarks: []api.EvaluationBenchmarkConfig{
			{
				Ref: api.Ref{
					ID: "arc_easy",
				},
				ProviderID: "lm_evaluation_harness",
			},
		},
	}

	now := time.Now()
	jobID := common.GUID()
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

func testEvaluationsStorage(t *testing.T, driver string, databaseName string) {
	var store abstractions.Storage
	var evaluationId string
	var tenant string

	var benchmarkConfig = api.EvaluationBenchmarkConfig{
		Ref:        api.Ref{ID: "bench-1"},
		ProviderID: "garak",
	}

	t.Run("NewStorage creates a new storage instance", func(t *testing.T) {
		s, err := getTestStorage(t, driver, databaseName)
		if err != nil {
			t.Fatalf("Failed to create storage: %v", err)
		}
		store = s
	})

	t.Run("CreateEvaluationJob creates a new evaluation job", func(t *testing.T) {
		config := &api.EvaluationJobConfig{
			Model: api.ModelRef{
				URL:  "http://test.com",
				Name: "test",
			},
			Benchmarks: []api.EvaluationBenchmarkConfig{
				{
					Ref:        api.Ref{ID: "bench-1"},
					ProviderID: "garak",
				},
			},
		}

		now := time.Now()
		tenant = getTenant("tenant-1")
		store = store.WithTenant(api.Tenant(tenant))

		job := &api.EvaluationJobResource{
			Resource: api.EvaluationResource{
				Resource: api.Resource{
					ID:        common.GUID(),
					Tenant:    api.Tenant(tenant),
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
		val := validation.NewValidator()
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
		/* js */ _, err = json.MarshalIndent(job, "", "  ")
		if err != nil {
			t.Fatalf("Failed to marshal job: %v", err)
		}
		// t.Logf("Job: %s\n", string(js))
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

	t.Run("UpdateEvaluationJobStatus running to pending updates state", func(t *testing.T) {
		jobID := common.GUID()
		now := time.Now()

		jobRes := &api.EvaluationJobResource{
			Resource: api.EvaluationResource{
				Resource: api.Resource{
					ID:        jobID,
					Tenant:    api.Tenant(tenant),
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
				Benchmarks: []api.EvaluationBenchmarkConfig{{Ref: api.Ref{ID: "b"}, ProviderID: "p"}},
			},
		}
		if err := store.CreateEvaluationJob(jobRes); err != nil {
			t.Fatalf("CreateEvaluationJob: %v", err)
		}
		msg := &api.MessageInfo{Message: "now pending", MessageCode: "test"}
		err := store.UpdateEvaluationJobStatus(jobID, api.OverallStatePending, msg)
		if err != nil {
			t.Fatalf("UpdateEvaluationJobStatus running->pending: %v", err)
		}
		got, err := store.GetEvaluationJob(jobID)
		if err != nil {
			t.Fatalf("GetEvaluationJob failed: %v", err)
		}
		if got.Status.State != api.OverallStatePending {
			t.Errorf("state should be pending, got %s", got.Status.State)
		}
	})

	t.Run("UpdateEvaluationJobStatus same-state persists message when message changes", func(t *testing.T) {
		jobID := common.GUID()
		now := time.Now()
		initialMsg := &api.MessageInfo{Message: "Evaluation job created", MessageCode: "created"}

		j := &api.EvaluationJobResource{
			Resource: api.EvaluationResource{
				Resource: api.Resource{
					ID:        jobID,
					Tenant:    api.Tenant(tenant),
					CreatedAt: now,
					UpdatedAt: now,
				},
				MLFlowExperimentID: "experiment-1",
			},
			Status: &api.EvaluationJobStatus{
				EvaluationJobState: api.EvaluationJobState{
					State:   api.OverallStatePending,
					Message: initialMsg,
				},
			},
			EvaluationJobConfig: api.EvaluationJobConfig{
				Model:      api.ModelRef{URL: "http://test.com", Name: "test"},
				Benchmarks: []api.EvaluationBenchmarkConfig{{Ref: api.Ref{ID: "b"}, ProviderID: "p"}},
			},
		}
		if err := store.CreateEvaluationJob(j); err != nil {
			t.Fatalf("CreateEvaluationJob: %v", err)
		}
		newMsg := &api.MessageInfo{
			Message:     "Evaluation job created but no runtime configured",
			MessageCode: "updated",
		}
		if err := store.UpdateEvaluationJobStatus(jobID, api.OverallStatePending, newMsg); err != nil {
			t.Fatalf("UpdateEvaluationJobStatus same-state message: %v", err)
		}
		got, err := store.GetEvaluationJob(jobID)
		if err != nil {
			t.Fatalf("GetEvaluationJob: %v", err)
		}
		if got.Status.State != api.OverallStatePending {
			t.Fatalf("state should stay pending, got %s", got.Status.State)
		}
		if got.Status.Message == nil || got.Status.Message.Message != newMsg.Message || got.Status.Message.MessageCode != newMsg.MessageCode {
			t.Fatalf("message not updated: %+v", got.Status.Message)
		}
	})

	t.Run("UpdateEvaluationJobStatus same-state nil message is no-op", func(t *testing.T) {
		jobID := common.GUID()
		now := time.Now()
		keepMsg := &api.MessageInfo{Message: "unchanged", MessageCode: "K"}

		j := &api.EvaluationJobResource{
			Resource: api.EvaluationResource{
				Resource: api.Resource{
					ID:        jobID,
					Tenant:    api.Tenant(tenant),
					CreatedAt: now,
					UpdatedAt: now,
				},
				MLFlowExperimentID: "experiment-1",
			},
			Status: &api.EvaluationJobStatus{
				EvaluationJobState: api.EvaluationJobState{
					State:   api.OverallStatePending,
					Message: keepMsg,
				},
			},
			EvaluationJobConfig: api.EvaluationJobConfig{
				Model:      api.ModelRef{URL: "http://test.com", Name: "test"},
				Benchmarks: []api.EvaluationBenchmarkConfig{{Ref: api.Ref{ID: "b"}, ProviderID: "p"}},
			},
		}
		if err := store.CreateEvaluationJob(j); err != nil {
			t.Fatalf("CreateEvaluationJob: %v", err)
		}
		if err := store.UpdateEvaluationJobStatus(jobID, api.OverallStatePending, nil); err != nil {
			t.Fatalf("UpdateEvaluationJobStatus: %v", err)
		}
		got, err := store.GetEvaluationJob(jobID)
		if err != nil {
			t.Fatalf("GetEvaluationJob: %v", err)
		}
		if got.Status.Message == nil || got.Status.Message.Message != keepMsg.Message {
			t.Fatalf("message should be unchanged, got %+v", got.Status.Message)
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
				Benchmarks: []api.EvaluationBenchmarkConfig{
					{Ref: api.Ref{ID: "b1"}, ProviderID: "p1"},
				},
			}
			if terminalState == api.OverallStatePartiallyFailed {
				config.Benchmarks = append(config.Benchmarks, api.EvaluationBenchmarkConfig{Ref: api.Ref{ID: "b2"}, ProviderID: "p1"})
			}
			now := time.Now()
			job := &api.EvaluationJobResource{
				Resource: api.EvaluationResource{
					Resource: api.Resource{
						ID:        jobID,
						Tenant:    api.Tenant(tenant),
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
			Benchmarks: []api.EvaluationBenchmarkConfig{
				{Ref: api.Ref{ID: "bx"}, ProviderID: "garak"},
			},
		}
		now := time.Now()
		job := &api.EvaluationJobResource{
			Resource: api.EvaluationResource{
				Resource: api.Resource{
					ID:        jobID,
					Tenant:    api.Tenant(tenant),
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
					Tenant:    api.Tenant(tenant),
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
			Benchmarks: []api.EvaluationBenchmarkConfig{
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
					Tenant:    api.Tenant(tenant),
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
	jsonBytes, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(jsonBytes)
}

func getTenant(tenant string) string {
	return tenant + common.GUID()
}

func TestGetEvaluationJobs_PassCriteria(t *testing.T) {
	zero := float32(0)
	point5 := float32(0.5)
	point9 := float32(0.9)
	// this is the default if nothing is set
	if err := testGetEvaluationJobs_PassCriteria(nil, nil, 0.5); err != nil {
		t.Fatalf("Pass criteria threshold test failed: %v", err)
	}
	// make sure that we can explicitly set 0
	if err := testGetEvaluationJobs_PassCriteria(&zero, nil, 0); err != nil {
		t.Fatalf("Pass criteria threshold test failed: %v", err)
	}
	if err := testGetEvaluationJobs_PassCriteria(nil, &zero, 0); err != nil {
		t.Fatalf("Pass criteria threshold test failed: %v", err)
	}
	// make sure that we can explicitly set a non-zero and non-default value
	if err := testGetEvaluationJobs_PassCriteria(&point9, nil, 0.9); err != nil {
		t.Fatalf("Pass criteria threshold test failed: %v", err)
	}
	if err := testGetEvaluationJobs_PassCriteria(nil, &point9, 0.9); err != nil {
		t.Fatalf("Pass criteria threshold test failed: %v", err)
	}
	// other checks
	if err := testGetEvaluationJobs_PassCriteria(&point9, &point9, 0.9); err != nil {
		t.Fatalf("Pass criteria threshold test failed: %v", err)
	}
	if err := testGetEvaluationJobs_PassCriteria(&zero, &point9, 0); err != nil {
		t.Fatalf("Pass criteria threshold test failed: %v", err)
	}
	if err := testGetEvaluationJobs_PassCriteria(&point9, &zero, 0.9); err != nil {
		t.Fatalf("Pass criteria threshold test failed: %v", err)
	}
	if err := testGetEvaluationJobs_PassCriteria(&point5, &point5, 0.5); err != nil {
		t.Fatalf("Pass criteria threshold test failed: %v", err)
	}
	if err := testGetEvaluationJobs_PassCriteria(&zero, &point5, 0); err != nil {
		t.Fatalf("Pass criteria threshold test failed: %v", err)
	}
	if err := testGetEvaluationJobs_PassCriteria(&point5, &zero, 0.5); err != nil {
		t.Fatalf("Pass criteria threshold test failed: %v", err)
	}
	if err := testGetEvaluationJobs_PassCriteria(&zero, &zero, 0); err != nil {
		t.Fatalf("Pass criteria threshold test failed: %v", err)
	}
}

func testGetEvaluationJobs_PassCriteria(jobThreshold *float32, collectionThreshold *float32, result float32) error {
	config := api.EvaluationJobConfig{
		Model: api.ModelRef{URL: "http://test.com", Name: "test"},
		Benchmarks: []api.EvaluationBenchmarkConfig{
			{
				Ref:        api.Ref{ID: "b1"},
				ProviderID: "prov1",
			},
		},
		PassCriteria: &api.PassCriteria{
			Threshold: jobThreshold,
		},
	}
	job := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{
				ID:        common.GUID(),
				Tenant:    api.Tenant("tenant1"),
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
		},
		EvaluationJobConfig: config,
	}
	var collection *api.CollectionResource
	if collectionThreshold != nil {
		collection = &api.CollectionResource{
			Resource: api.Resource{
				ID:        common.GUID(),
				Tenant:    api.Tenant("tenant1"),
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
			CollectionConfig: api.CollectionConfig{
				PassCriteria: &api.PassCriteria{
					Threshold: collectionThreshold,
				},
			},
		}
	}
	v := sql.GetPassCriteriaThreshold(job, collection)
	if v != result {
		return fmt.Errorf("Expected threshold to be %v, got %v", result, v)
	}
	return nil
}
