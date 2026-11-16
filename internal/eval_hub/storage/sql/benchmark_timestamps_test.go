package sql_test

import (
	"sync"
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
			status := api.BenchmarkStatus{ID: "repeated", ProviderID: "provider", BenchmarkIndex: index, Status: api.StatePending}
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
	resourceCreated := job.Resource.CreatedAt.Add(30 * time.Second)
	if err := store.RecordBenchmarkCreation(job.Resource.ID, 0, resourceCreated); err != nil {
		t.Fatal(err)
	}
	created := api.DateTimeToString(resourceCreated)
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
	if untouched.CreatedAt != "" || untouched.UpdatedAt != "" || untouched.CompletedAt != "" {
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
	if otherRunning.CreatedAt != "" || otherRunning.StartedAt == "" {
		t.Fatalf("runtime event invented creation or omitted start fallback: %+v", otherRunning)
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
				if err := store.RecordBenchmarkCreation(job.Resource.ID, index, job.Resource.CreatedAt.Add(time.Second)); err != nil {
					t.Fatal(err)
				}
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
		ID: "repeated", ProviderID: "provider", Status: api.StateFailed, StartedAt: "2026-01-02T03:05:05Z",
	}}); err != nil {
		t.Fatal(err)
	}
	status := readBenchmarkTimestampStatus(t, store, job.Resource.ID, 0)
	if status.CreatedAt != "" || status.StartedAt != "" || status.UpdatedAt == "" || status.CompletedAt == "" {
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

func TestRecordBenchmarkCreationMergesExistingStatus(t *testing.T) {
	for _, state := range []api.State{api.StatePending, api.StateRunning, api.StateCompleted, api.StateFailed} {
		t.Run(string(state), func(t *testing.T) {
			store, job := newBenchmarkTimestampJob(t)
			if err := store.UpdateEvaluationJob(job.Resource.ID, &api.StatusEvent{BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
				ID: "repeated", ProviderID: "provider", Status: state, StartedAt: "2026-01-02T03:05:05Z",
			}}); err != nil {
				t.Fatal(err)
			}
			before := readBenchmarkTimestampStatus(t, store, job.Resource.ID, 0)
			created := job.Resource.CreatedAt.Add(time.Second)
			if err := store.RecordBenchmarkCreation(job.Resource.ID, 0, created); err != nil {
				t.Fatal(err)
			}
			after := readBenchmarkTimestampStatus(t, store, job.Resource.ID, 0)
			if after.CreatedAt != api.DateTimeToString(created) || after.Status != before.Status ||
				after.StartedAt != before.StartedAt || after.CompletedAt != before.CompletedAt {
				t.Fatalf("creation overwrote status: before=%+v after=%+v", before, after)
			}
			if err := store.RecordBenchmarkCreation(job.Resource.ID, 0, created.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			if duplicate := readBenchmarkTimestampStatus(t, store, job.Resource.ID, 0); duplicate != after {
				t.Fatalf("duplicate creation changed status: %+v", duplicate)
			}
			untouched := readBenchmarkTimestampStatus(t, store, job.Resource.ID, 1)
			if untouched.CreatedAt != "" || untouched.UpdatedAt != "" {
				t.Fatalf("creation updated another occurrence: %+v", untouched)
			}
		})
	}
}

func TestRecordBenchmarkCreationAfterTerminalJob(t *testing.T) {
	for _, state := range []api.OverallState{api.OverallStateCancelled, api.OverallStateFailed, api.OverallStateCompleted} {
		t.Run(string(state), func(t *testing.T) {
			store, job := newBenchmarkTimestampJob(t)
			if state == api.OverallStateCompleted {
				for index := range job.Benchmarks {
					if err := store.UpdateEvaluationJob(job.Resource.ID, &api.StatusEvent{BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
						ID: "repeated", ProviderID: "provider", BenchmarkIndex: index, Status: api.StateCompleted,
					}}); err != nil {
						t.Fatal(err)
					}
				}
			} else if err := store.UpdateEvaluationJobStatus(job.Resource.ID, state, nil); err != nil {
				t.Fatal(err)
			}
			before := readBenchmarkTimestampStatus(t, store, job.Resource.ID, 0)
			if err := store.RecordBenchmarkCreation(job.Resource.ID, 0, job.Resource.CreatedAt.Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			after := readBenchmarkTimestampStatus(t, store, job.Resource.ID, 0)
			if after.CreatedAt == "" || after.StartedAt != "" || after.Status != before.Status || after.CompletedAt != before.CompletedAt {
				t.Fatalf("late creation changed terminal status: before=%+v after=%+v", before, after)
			}
			stored, err := store.GetEvaluationJob(job.Resource.ID)
			if err != nil || stored.Status.State != state {
				t.Fatalf("late creation changed overall state: %+v, %v", stored, err)
			}
		})
	}
}

func TestRecordBenchmarkCreationConcurrentWithRunning(t *testing.T) {
	store, job := newBenchmarkTimestampJob(t)
	var wg sync.WaitGroup
	errors := make(chan error, 2)
	wg.Go(func() { errors <- store.RecordBenchmarkCreation(job.Resource.ID, 0, job.Resource.CreatedAt) })
	wg.Go(func() {
		errors <- store.UpdateEvaluationJob(job.Resource.ID, &api.StatusEvent{BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
			ID: "repeated", ProviderID: "provider", Status: api.StateRunning,
		}})
	})
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	status := readBenchmarkTimestampStatus(t, store, job.Resource.ID, 0)
	if status.Status != api.StateRunning || status.CreatedAt == "" || status.StartedAt == "" {
		t.Fatalf("concurrent updates lost lifecycle data: %+v", status)
	}
}

func TestRecordBenchmarkCreationInitializesMissingStatus(t *testing.T) {
	for _, state := range []api.OverallState{api.OverallStatePending, api.OverallStateCancelled, api.OverallStateFailed} {
		t.Run(string(state), func(t *testing.T) {
			store, job := newBenchmarkTimestampJobWithStatus(t, false)
			if state != api.OverallStatePending {
				if err := store.UpdateEvaluationJobStatus(job.Resource.ID, state, nil); err != nil {
					t.Fatal(err)
				}
			}
			if err := store.RecordBenchmarkCreation(job.Resource.ID, 1, job.Resource.CreatedAt); err != nil {
				t.Fatal(err)
			}
			status := readBenchmarkTimestampStatus(t, store, job.Resource.ID, 1)
			if status.ID != "repeated" || status.ProviderID != "provider" || status.Status != api.State(state) ||
				status.CreatedAt == "" || status.UpdatedAt == "" || status.StartedAt != "" {
				t.Fatalf("unexpected initial benchmark status: %+v", status)
			}
			if (state == api.OverallStatePending) != (status.CompletedAt == "") {
				t.Fatalf("completion timestamp does not match state: %+v", status)
			}
		})
	}
}

func TestRecordBenchmarkCreationValidatesInput(t *testing.T) {
	store, job := newBenchmarkTimestampJob(t)
	for _, index := range []int{-1, len(job.Benchmarks)} {
		if err := store.RecordBenchmarkCreation(job.Resource.ID, index, time.Now()); err == nil {
			t.Fatalf("accepted invalid benchmark index %d", index)
		}
	}
	if err := store.RecordBenchmarkCreation(job.Resource.ID, 0, time.Time{}); err == nil {
		t.Fatal("accepted missing creation time")
	}
}
