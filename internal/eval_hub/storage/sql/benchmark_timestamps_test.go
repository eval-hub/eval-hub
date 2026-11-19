package sql_test

import (
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/common"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func newBenchmarkTimestampJob(t *testing.T, initialStates ...api.State) (abstractions.Storage, *api.EvaluationJobResource) {
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
		state := api.StatePending
		if index < len(initialStates) {
			state = initialStates[index]
			if state == api.StateRunning {
				job.Status.State = api.OverallStateRunning
			}
		}
		job.Benchmarks = append(job.Benchmarks, api.EvaluationBenchmarkConfig{Ref: api.Ref{ID: "repeated"}, ProviderID: "provider"})
		job.Status.Benchmarks = append(job.Status.Benchmarks, api.BenchmarkStatus{
			ID: "repeated", ProviderID: "provider", BenchmarkIndex: index, Status: state,
		})
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

	pending := update(0, api.StatePending, started, "")
	if pending.StartedAt != "" || pending.CompletedAt != "" {
		t.Fatalf("pending event changed lifecycle timestamps: %+v", pending)
	}
	running := update(0, api.StateRunning, started, "")
	if running.StartedAt != started || running.CompletedAt != "" {
		t.Fatalf("unexpected running timestamps: %+v", running)
	}
	untouched := readBenchmarkTimestampStatus(t, store, job.Resource.ID, 1)
	if untouched.Status != api.StatePending || untouched.StartedAt != "" || untouched.CompletedAt != "" {
		t.Fatalf("another occurrence was updated: %+v", untouched)
	}

	// Phase/status events often omit runtime timestamps. Preserve the original start.
	progress := update(0, api.StateRunning, api.DateTimeToString(time.Now()), "")
	if progress.StartedAt != started {
		t.Fatalf("progress lost timestamps: %+v", progress)
	}
	before := time.Now().UTC().Truncate(time.Second)
	completed := update(0, api.StateCompleted, "", "")
	completionTime, err := api.DateTimeFromString(completed.CompletedAt)
	if err != nil || completionTime.Before(before) || completionTime.After(time.Now()) || completed.StartedAt != started {
		t.Fatalf("missing terminal timestamp fallback: %+v", completed)
	}
	duplicate := update(0, api.StateCompleted, started, started)
	if duplicate.CompletedAt != completed.CompletedAt || duplicate.StartedAt != started {
		t.Fatalf("duplicate event rewrote lifecycle timestamps: %+v", duplicate)
	}
	downgrade := update(0, api.StateRunning, "", "")
	if downgrade != duplicate {
		t.Fatalf("ignored downgrade changed the stored benchmark: before=%+v after=%+v", duplicate, downgrade)
	}

	runtimeCompleted := api.DateTimeToString(job.Resource.CreatedAt.Add(2 * time.Minute))
	otherRunning := update(1, api.StateRunning, "", "")
	if otherRunning.StartedAt == "" {
		t.Fatalf("runtime event omitted start fallback: %+v", otherRunning)
	}
	failed := update(1, api.StateFailed, "", runtimeCompleted)
	if failed.CompletedAt != runtimeCompleted || failed.StartedAt != otherRunning.StartedAt {
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
				if status.Status != wantState || status.CompletedAt == "" {
					t.Fatalf("unfinished benchmark was not finalized: %+v", status)
				}
				if (index == 1 && status.StartedAt != running.StartedAt) || (index == 2 && status.StartedAt != "") {
					t.Fatalf("job finalization changed benchmark start: %+v", status)
				}
			}
			if got := readBenchmarkTimestampStatus(t, store, job.Resource.ID, 0); got != completed {
				t.Fatalf("already completed benchmark changed: before=%+v after=%+v", completed, got)
			}
		})
	}
}

func TestBenchmarkTerminalEventWithoutRunning(t *testing.T) {
	for _, state := range []api.State{api.StateCompleted, api.StateFailed, api.StateCancelled} {
		for _, supplied := range []api.DateTime{"", "2026-01-02T05:00:00+02:00"} {
			t.Run(string(state)+"/"+string(supplied), func(t *testing.T) {
				store, job := newBenchmarkTimestampJob(t)
				before := time.Now().UTC().Truncate(time.Second)
				if err := store.UpdateEvaluationJob(job.Resource.ID, &api.StatusEvent{BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
					ID: "repeated", ProviderID: "provider", Status: state, StartedAt: supplied,
				}}); err != nil {
					t.Fatal(err)
				}
				status := readBenchmarkTimestampStatus(t, store, job.Resource.ID, 0)
				completed, err := api.DateTimeFromString(status.CompletedAt)
				if status.StartedAt != supplied || err != nil || completed.Before(before) || completed.After(time.Now()) {
					t.Fatalf("unexpected timestamps for direct terminal event: %+v (%v)", status, err)
				}
				// A duplicate terminal event must not replace or backfill the first start.
				if err := store.UpdateEvaluationJob(job.Resource.ID, &api.StatusEvent{BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
					ID: "repeated", ProviderID: "provider", Status: state, StartedAt: "2000-01-01T00:00:00Z",
				}}); err != nil {
					t.Fatal(err)
				}
				duplicate := readBenchmarkTimestampStatus(t, store, job.Resource.ID, 0)
				if duplicate.StartedAt != supplied || duplicate.CompletedAt != status.CompletedAt {
					t.Fatalf("duplicate terminal event changed timestamps: before=%+v after=%+v", status, duplicate)
				}
			})
		}
	}
}

func TestBenchmarkStartRequiresPendingTransition(t *testing.T) {
	for _, state := range []api.State{api.StateRunning, api.StateCompleted, api.StateFailed, api.StateCancelled} {
		for _, supplied := range []api.DateTime{"", "2026-01-02T05:00:00+02:00"} {
			t.Run(string(state)+"/"+string(supplied), func(t *testing.T) {
				// An existing running status without a start must not be backfilled.
				store, job := newBenchmarkTimestampJob(t, api.StateRunning)
				if err := store.UpdateEvaluationJob(job.Resource.ID, &api.StatusEvent{BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
					ID: "repeated", ProviderID: "provider", Status: state, StartedAt: supplied,
				}}); err != nil {
					t.Fatal(err)
				}
				status := readBenchmarkTimestampStatus(t, store, job.Resource.ID, 0)
				if status.StartedAt != "" {
					t.Fatalf("%s update backfilled a running benchmark's start: %+v", state, status)
				}
			})
		}
	}
}

func TestBenchmarkIgnoresPendingAfterRunning(t *testing.T) {
	for _, supplied := range []api.DateTime{"", "2026-01-02T05:00:00+02:00"} {
		t.Run(string(supplied), func(t *testing.T) {
			store, job := newBenchmarkTimestampJob(t, api.StateRunning)
			before := readBenchmarkTimestampStatus(t, store, job.Resource.ID, 0)
			if err := store.UpdateEvaluationJob(job.Resource.ID, &api.StatusEvent{BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
				ID: "repeated", ProviderID: "provider", Status: api.StatePending,
				Phase: "initializing", StartedAt: "2000-01-01T00:00:00Z",
			}}); err != nil {
				t.Fatal(err)
			}
			if got := readBenchmarkTimestampStatus(t, store, job.Resource.ID, 0); got != before {
				t.Fatalf("late pending event changed running benchmark: before=%+v after=%+v", before, got)
			}
			for _, state := range []api.State{api.StateRunning, api.StateCompleted} {
				if err := store.UpdateEvaluationJob(job.Resource.ID, &api.StatusEvent{BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
					ID: "repeated", ProviderID: "provider", Status: state, StartedAt: supplied,
				}}); err != nil {
					t.Fatal(err)
				}
				if got := readBenchmarkTimestampStatus(t, store, job.Resource.ID, 0); got.StartedAt != "" {
					t.Fatalf("%s event backfilled the original running benchmark's start: %+v", state, got)
				}
			}
		})
	}
}

func TestBenchmarkStartUsesOnlyFirstRunningEvent(t *testing.T) {
	for _, supplied := range []api.DateTime{"", "2026-01-02T05:00:00+02:00"} {
		t.Run(string(supplied), func(t *testing.T) {
			store, job := newBenchmarkTimestampJob(t)
			before := time.Now().UTC().Truncate(time.Second)
			first := &api.StatusEvent{BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
				ID: "repeated", ProviderID: "provider", Status: api.StateRunning, StartedAt: supplied,
			}}
			if err := store.UpdateEvaluationJob(job.Resource.ID, first); err != nil {
				t.Fatal(err)
			}
			want := readBenchmarkTimestampStatus(t, store, job.Resource.ID, 0).StartedAt
			if supplied != "" {
				if want != supplied {
					t.Fatalf("first runtime start was replaced: got %q, want %q", want, supplied)
				}
			} else {
				started, err := api.DateTimeFromString(want)
				if err != nil || started.Before(before) || started.After(time.Now()) {
					t.Fatalf("invalid internal runtime start fallback %q: %v", want, err)
				}
			}
			// Even a valid earlier runtime timestamp on a later event must be ignored.
			for _, state := range []api.State{api.StateRunning, api.StatePending, api.StateRunning, api.StateCompleted} {
				if err := store.UpdateEvaluationJob(job.Resource.ID, &api.StatusEvent{BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
					ID: "repeated", ProviderID: "provider", Status: state, StartedAt: "2000-01-01T00:00:00Z",
				}}); err != nil {
					t.Fatal(err)
				}
				got := readBenchmarkTimestampStatus(t, store, job.Resource.ID, 0)
				if got.StartedAt != want {
					t.Fatalf("%s event changed first start: got %q, want %q", state, got.StartedAt, want)
				}
				if state == api.StatePending && got.Status != api.StateRunning {
					t.Fatalf("late pending event regressed a running benchmark: %+v", got)
				}
			}
		})
	}
}
