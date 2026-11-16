package sql_test

import (
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/common"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func newBenchmarkTimestampJob(t *testing.T) (abstractions.Storage, *api.EvaluationJobResource) {
	t.Helper()
	store, err := getTestStorage(t, drivers[0], getDBName())
	if err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	job := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{Resource: api.Resource{ID: common.GUID(), CreatedAt: created}},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: &api.ModelRef{Name: "test", URL: "http://test.com"},
		},
		Status: &api.EvaluationJobStatus{EvaluationJobState: api.EvaluationJobState{State: api.OverallStatePending}},
	}
	for index := range 3 {
		job.Benchmarks = append(job.Benchmarks, api.EvaluationBenchmarkConfig{Ref: api.Ref{ID: "repeated"}, ProviderID: "provider"})
		status := api.BenchmarkStatus{ID: "repeated", ProviderID: "provider", BenchmarkIndex: index, Status: api.StatePending}
		job.Status.Benchmarks = append(job.Status.Benchmarks, status)
	}
	if err := store.CreateEvaluationJob(job); err != nil {
		t.Fatal(err)
	}
	return store, job
}

func readBenchmarkTimestampStatus(t *testing.T, store abstractions.Storage, id string, index int) api.BenchmarkStatus {
	t.Helper()
	job, err := store.GetEvaluationJob(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range job.Status.Benchmarks {
		if status.BenchmarkIndex == index {
			return status
		}
	}
	t.Fatalf("benchmark index %d is missing", index)
	return api.BenchmarkStatus{}
}

func TestEvaluationBenchmarkTimestampsLifecycle(t *testing.T) {
	store, job := newBenchmarkTimestampJob(t)
	started := api.DateTimeToString(job.Resource.CreatedAt.Add(time.Minute))
	update := func(index int, state api.State, startedAt, completedAt api.DateTime) api.BenchmarkStatus {
		t.Helper()
		err := store.UpdateEvaluationJob(job.Resource.ID, &api.StatusEvent{BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
			ID: "repeated", ProviderID: "provider", BenchmarkIndex: index,
			Status: state, StartedAt: startedAt, CompletedAt: completedAt,
		}})
		if err != nil {
			t.Fatal(err)
		}
		return readBenchmarkTimestampStatus(t, store, job.Resource.ID, index)
	}

	pending := update(0, api.StatePending, "", "")
	if pending.CreatedAt != "" || pending.CompletedAt != "" || pending.UpdatedAt == "" {
		t.Fatalf("pending event created the benchmark too early: %+v", pending)
	}
	before := time.Now().UTC().Truncate(time.Second)
	running := update(0, api.StateRunning, started, "")
	created := running.CreatedAt
	updated, err := api.DateTimeFromString(running.UpdatedAt)
	if err != nil || updated.Before(before) || updated.After(time.Now()) {
		t.Fatalf("invalid server update time %q: %v", running.UpdatedAt, err)
	}
	if created == "" || created == api.DateTimeToString(job.Resource.CreatedAt) || running.CreatedAt != running.UpdatedAt || running.StartedAt != started || running.CompletedAt != "" {
		t.Fatalf("unexpected running timestamps: %+v", running)
	}
	untouched := readBenchmarkTimestampStatus(t, store, job.Resource.ID, 1)
	if untouched.CreatedAt != "" || untouched.UpdatedAt != "" || untouched.CompletedAt != "" {
		t.Fatalf("another occurrence was updated: %+v", untouched)
	}

	// Phase/status events often omit runtime timestamps. Preserve the original start.
	progress := update(0, api.StateRunning, "", "")
	if progress.StartedAt != started || progress.CreatedAt != created {
		t.Fatalf("progress lost timestamps: %+v", progress)
	}
	completed := update(0, api.StateCompleted, "", "")
	if completed.CompletedAt == "" || completed.CompletedAt != completed.UpdatedAt || completed.StartedAt != started {
		t.Fatalf("missing terminal timestamp fallback: %+v", completed)
	}
	duplicate := update(0, api.StateCompleted, created, created)
	if duplicate.CompletedAt != completed.CompletedAt || duplicate.StartedAt != started || duplicate.CreatedAt != created {
		t.Fatalf("duplicate event rewrote lifecycle timestamps: %+v", duplicate)
	}
	downgrade := update(0, api.StateRunning, "", "")
	if downgrade != duplicate {
		t.Fatalf("ignored downgrade changed the stored benchmark: before=%+v after=%+v", duplicate, downgrade)
	}

	runtimeCompleted := api.DateTimeToString(job.Resource.CreatedAt.Add(2 * time.Minute))
	otherRunning := update(1, api.StateRunning, "", "")
	failed := update(1, api.StateFailed, "", runtimeCompleted)
	if failed.CreatedAt != otherRunning.CreatedAt || failed.CompletedAt != runtimeCompleted || failed.UpdatedAt == "" {
		t.Fatalf("runtime completion time was not retained: %+v", failed)
	}
	if got := readBenchmarkTimestampStatus(t, store, job.Resource.ID, 0); got != duplicate {
		t.Fatalf("updating another occurrence changed completed benchmark: %+v", got)
	}
}

func TestEvaluationBenchmarkTimestampsJobFinalization(t *testing.T) {
	for _, state := range []api.OverallState{api.OverallStateCancelled, api.OverallStateFailed} {
		t.Run(string(state), func(t *testing.T) {
			store, job := newBenchmarkTimestampJob(t)
			for index, benchmarkState := range []api.State{api.StateCompleted, api.StateRunning} {
				if err := store.UpdateEvaluationJob(job.Resource.ID, &api.StatusEvent{BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
					ID: "repeated", ProviderID: "provider", BenchmarkIndex: index, Status: benchmarkState,
				}}); err != nil {
					t.Fatal(err)
				}
			}
			completed := readBenchmarkTimestampStatus(t, store, job.Resource.ID, 0)
			running := readBenchmarkTimestampStatus(t, store, job.Resource.ID, 1)
			if err := store.UpdateEvaluationJobStatus(job.Resource.ID, state, nil); err != nil {
				t.Fatal(err)
			}
			wantState := api.StateCancelled
			if state == api.OverallStateFailed {
				wantState = api.StateFailed
			}
			for _, index := range []int{1, 2} {
				status := readBenchmarkTimestampStatus(t, store, job.Resource.ID, index)
				wantCreated := api.DateTime("")
				if index == 1 {
					wantCreated = running.CreatedAt
				}
				if status.Status != wantState || status.CompletedAt == "" || status.UpdatedAt != status.CompletedAt || status.CreatedAt != wantCreated {
					t.Fatalf("unfinished benchmark was not finalized: %+v", status)
				}
			}
			if got := readBenchmarkTimestampStatus(t, store, job.Resource.ID, 0); got != completed {
				t.Fatalf("already completed benchmark changed: before=%+v after=%+v", completed, got)
			}
		})
	}
}

func TestEvaluationBenchmarkTimestampsFailureBeforeRunning(t *testing.T) {
	store, job := newBenchmarkTimestampJob(t)
	if err := store.UpdateEvaluationJob(job.Resource.ID, &api.StatusEvent{BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
		ID: "repeated", ProviderID: "provider", Status: api.StateFailed,
	}}); err != nil {
		t.Fatal(err)
	}
	status := readBenchmarkTimestampStatus(t, store, job.Resource.ID, 0)
	if status.CreatedAt != "" || status.UpdatedAt == "" || status.CompletedAt == "" {
		t.Fatalf("unexpected timestamps for a benchmark that never ran: %+v", status)
	}
}
