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
	return newBenchmarkTimestampJobWithStatus(t, true)
}

func newBenchmarkTimestampJobWithStatus(t *testing.T, initializeStatus bool) (abstractions.Storage, *api.EvaluationJobResource) {
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
		if initializeStatus {
			status := api.BenchmarkStatus{
				ID: "repeated", ProviderID: "provider", BenchmarkIndex: index, Status: api.StatePending,
				CreatedAt: api.DateTimeToString(created), UpdatedAt: api.DateTimeToString(created),
			}
			job.Status.Benchmarks = append(job.Status.Benchmarks, status)
		}
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
	created := api.DateTimeToString(job.Resource.CreatedAt)
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

	pending := update(0, api.StatePending, started, "")
	if pending.CreatedAt != created || pending.StartedAt != "" || pending.CompletedAt != "" || pending.UpdatedAt == "" {
		t.Fatalf("pending event changed lifecycle timestamps: %+v", pending)
	}
	before := time.Now().UTC().Truncate(time.Second)
	running := update(0, api.StateRunning, started, "")
	updated, err := api.DateTimeFromString(running.UpdatedAt)
	if err != nil || updated.Before(before) || updated.After(time.Now()) {
		t.Fatalf("invalid server update time %q: %v", running.UpdatedAt, err)
	}
	if running.CreatedAt != created || running.StartedAt != started || running.CompletedAt != "" {
		t.Fatalf("unexpected running timestamps: %+v", running)
	}
	untouched := readBenchmarkTimestampStatus(t, store, job.Resource.ID, 1)
	if untouched.CreatedAt != created || untouched.UpdatedAt != created || untouched.CompletedAt != "" {
		t.Fatalf("another occurrence was updated: %+v", untouched)
	}

	// Phase/status events often omit runtime timestamps. Preserve the original start.
	progress := update(0, api.StateRunning, api.DateTimeToString(time.Now()), "")
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
	if otherRunning.CreatedAt != created || otherRunning.StartedAt == "" {
		t.Fatalf("runtime event lost job creation time or omitted start fallback: %+v", otherRunning)
	}
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
			if err := store.UpdateEvaluationJobStatus(job.Resource.ID, state, nil); err != nil {
				t.Fatal(err)
			}
			wantState := api.StateCancelled
			if state == api.OverallStateFailed {
				wantState = api.StateFailed
			}
			for _, index := range []int{1, 2} {
				status := readBenchmarkTimestampStatus(t, store, job.Resource.ID, index)
				wantCreated := api.DateTimeToString(job.Resource.CreatedAt)
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
		ID: "repeated", ProviderID: "provider", Status: api.StateFailed, StartedAt: "2026-01-02T03:05:05Z",
	}}); err != nil {
		t.Fatal(err)
	}
	status := readBenchmarkTimestampStatus(t, store, job.Resource.ID, 0)
	if status.CreatedAt != api.DateTimeToString(job.Resource.CreatedAt) || status.StartedAt != "" || status.UpdatedAt == "" || status.CompletedAt == "" {
		t.Fatalf("unexpected timestamps for a benchmark that never ran: %+v", status)
	}
}

func TestBenchmarkStartUsesOnlyFirstRunningEvent(t *testing.T) {
	for _, supplied := range []api.DateTime{"", "2026-01-02T05:00:00+02:00"} {
		t.Run(string(supplied), func(t *testing.T) {
			store, job := newBenchmarkTimestampJob(t)
			received := job.Resource.CreatedAt.Add(time.Minute)
			first := &api.StatusEvent{ReceivedAt: received, BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
				ID: "repeated", ProviderID: "provider", Status: api.StateRunning, StartedAt: supplied,
			}}
			if err := store.UpdateEvaluationJob(job.Resource.ID, first); err != nil {
				t.Fatal(err)
			}
			want := supplied
			if want == "" {
				want = api.DateTimeToString(received)
			}
			// Even a valid earlier runtime timestamp on a later event must be ignored.
			for _, state := range []api.State{api.StateRunning, api.StatePending, api.StateRunning, api.StateCompleted} {
				if err := store.UpdateEvaluationJob(job.Resource.ID, &api.StatusEvent{BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
					ID: "repeated", ProviderID: "provider", Status: state, StartedAt: "2000-01-01T00:00:00Z",
				}}); err != nil {
					t.Fatal(err)
				}
				if got := readBenchmarkTimestampStatus(t, store, job.Resource.ID, 0); got.StartedAt != want {
					t.Fatalf("%s event changed first start: got %q, want %q", state, got.StartedAt, want)
				}
			}
		})
	}
}

// Jobs persisted before benchmark timestamps were introduced may have no
// benchmark status entries, or entries without creation metadata.
func TestBenchmarkEventUsesJobCreationTimeWhenMissing(t *testing.T) {
	for _, initializeStatus := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing status", true: "missing timestamps"}[initializeStatus], func(t *testing.T) {
			store, job := newBenchmarkTimestampJobWithStatus(t, false)
			if initializeStatus {
				job.Status.Benchmarks = []api.BenchmarkStatus{{ID: "repeated", ProviderID: "provider", Status: api.StatePending}}
				if err := store.DeleteEvaluationJob(job.Resource.ID); err != nil {
					t.Fatal(err)
				}
				if err := store.CreateEvaluationJob(job); err != nil {
					t.Fatal(err)
				}
			}
			for _, state := range []api.State{api.StateRunning, api.StateCompleted} {
				if err := store.UpdateEvaluationJob(job.Resource.ID, &api.StatusEvent{BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
					ID: "repeated", ProviderID: "provider", Status: state,
				}}); err != nil {
					t.Fatal(err)
				}
				status := readBenchmarkTimestampStatus(t, store, job.Resource.ID, 0)
				if status.CreatedAt != api.DateTimeToString(job.Resource.CreatedAt) {
					t.Fatalf("%s event did not use job creation time: %+v", state, status)
				}
			}
		})
	}
}
