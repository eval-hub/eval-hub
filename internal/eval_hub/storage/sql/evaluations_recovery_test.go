package sql_test

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/common"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestUpdateEvaluationJobConditionalFailure(t *testing.T) {
	testUpdateEvaluationJobConditionalFailure(t, drivers[0], getDBName())
}

func testUpdateEvaluationJobConditionalFailure(t *testing.T, driver, databaseName string) {
	const recoveryCode = "KUBERNETES_JOB_FAILURE"

	createJob := func(t *testing.T, benchmarks ...string) (abstractions.Storage, string) {
		t.Helper()
		store, err := getTestStorage(t, driver, databaseName)
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC()
		job := &api.EvaluationJobResource{
			Resource: api.EvaluationResource{Resource: api.Resource{ID: common.GUID(), CreatedAt: now, UpdatedAt: now}},
			EvaluationJobConfig: api.EvaluationJobConfig{
				Model: &api.ModelRef{Name: "test", URL: "http://test.com"},
			},
			Status: &api.EvaluationJobStatus{EvaluationJobState: api.EvaluationJobState{State: api.OverallStatePending}},
		}
		for _, id := range benchmarks {
			job.Benchmarks = append(job.Benchmarks, api.EvaluationBenchmarkConfig{Ref: api.Ref{ID: id}, ProviderID: "p1"})
		}
		if err := store.CreateEvaluationJob(job); err != nil {
			t.Fatalf("create evaluation: %v", err)
		}
		return store, job.Resource.ID
	}

	update := func(store abstractions.Storage, id string, event api.BenchmarkStatusEvent) error {
		event.ProviderID = "p1"
		event.ID = "b1"
		return store.UpdateEvaluationJob(id, &api.StatusEvent{BenchmarkStatusEvent: &event})
	}

	read := func(t *testing.T, store abstractions.Storage, id string) *api.EvaluationJobResource {
		t.Helper()
		job, err := store.GetEvaluationJob(id)
		if err != nil {
			t.Fatalf("read evaluation: %v", err)
		}
		if len(job.Status.Benchmarks) != 1 {
			t.Fatalf("expected one recorded benchmark: %+v", job.Status)
		}
		return job
	}

	for _, tt := range []struct {
		name        string
		reported    *api.MessageInfo
		completedAt api.DateTime
		message     string
	}{
		{name: "default details", message: "Kubernetes marked the evaluation Job as failed."},
		{name: "supplied details", reported: &api.MessageInfo{Message: "Container ran out of memory."},
			completedAt: "2026-11-24T12:00:00Z", message: "Container ran out of memory."},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store, id := createJob(t, "b1")
			warning := &api.MessageInfo{Message: "Adapter warning.", MessageCode: "ADAPTER_WARNING", MessageOrigin: api.MessageOriginRuntime}
			phase := api.JobPhasePersistingArtifacts
			if err := update(store, id, api.BenchmarkStatusEvent{Status: api.StateRunning, Phase: phase, WarningMessage: warning}); err != nil {
				t.Fatalf("record progress: %v", err)
			}

			failure := api.BenchmarkStatusEvent{
				Status: api.StateFailed, Precondition: api.BenchmarkStatusPreconditionNonterminal,
				ErrorMessage: tt.reported, CompletedAt: tt.completedAt,
			}
			before := time.Now().UTC().Truncate(time.Second)
			if err := update(store, id, failure); err != nil {
				t.Fatalf("record operator failure: %v", err)
			}
			job := read(t, store, id)
			benchmark := job.Status.Benchmarks[0]
			wantError := &api.MessageInfo{Message: tt.message, MessageCode: recoveryCode, MessageOrigin: api.MessageOriginServer}
			if job.Status.State != api.OverallStateFailed || benchmark.Status != api.StateFailed ||
				!reflect.DeepEqual(benchmark.ErrorMessage, wantError) {
				t.Fatalf("unexpected recorded failure: overall=%q benchmark=%+v error=%+v", job.Status.State, benchmark, benchmark.ErrorMessage)
			}
			if benchmark.Phase != phase || !reflect.DeepEqual(benchmark.WarningMessage, warning) {
				t.Errorf("failure lost the previous phase or warning: %+v", benchmark)
			}
			if tt.completedAt == "" {
				got, err := api.DateTimeFromString(benchmark.CompletedAt)
				if err != nil || got.Before(before) || got.After(time.Now()) {
					t.Errorf("missing server failure time: got %q, err=%v", benchmark.CompletedAt, err)
				}
			} else if benchmark.CompletedAt != tt.completedAt {
				t.Errorf("failure time = %q, want %q", benchmark.CompletedAt, tt.completedAt)
			}

			if err := update(store, id, failure); !errors.Is(err, abstractions.ErrBenchmarkAlreadyTerminal) {
				t.Errorf("duplicate conditional report = %v, want already-terminal outcome", err)
			}

			final := api.BenchmarkStatusEvent{
				Status: api.StateCompleted, CompletedAt: api.DateTimeToString(time.Now().Add(time.Minute).UTC()),
				Metrics: map[string]any{"accuracy": 0.75},
			}
			if err := update(store, id, final); err != nil {
				t.Fatalf("replace operator failure with adapter result: %v", err)
			}
			job = read(t, store, id)
			benchmark = job.Status.Benchmarks[0]
			if job.Status.State != api.OverallStateCompleted || benchmark.Status != api.StateCompleted ||
				benchmark.ErrorMessage != nil || benchmark.CompletedAt != final.CompletedAt {
				t.Fatalf("unexpected corrected evaluation: overall=%q benchmark=%+v", job.Status.State, benchmark)
			}
			if job.Results == nil || len(job.Results.Benchmarks) != 1 || job.Results.Benchmarks[0].Metrics["accuracy"] != 0.75 {
				t.Errorf("adapter result was not stored: %+v", job.Results)
			}
		})
	}

	t.Run("preserves final adapter failure with a pending sibling", func(t *testing.T) {
		store, id := createJob(t, "b1", "b2")
		final := api.BenchmarkStatusEvent{
			Status: api.StateFailed, LogsPath: "/data/logs/adapter.log",
			Artifacts:    map[string]any{"report": "file:///tmp/adapter-report.json"},
			ErrorMessage: &api.MessageInfo{Message: "Adapter failure.", MessageCode: recoveryCode, MessageOrigin: api.MessageOriginServer},
		}
		if err := update(store, id, final); err != nil {
			t.Fatalf("record adapter failure: %v", err)
		}
		before := read(t, store, id)
		if before.Status.State != api.OverallStateRunning || before.Status.Benchmarks[0].ErrorMessage == nil ||
			before.Status.Benchmarks[0].ErrorMessage.MessageOrigin != api.MessageOriginRuntime {
			t.Fatalf("expected an ordinary adapter failure with an active sibling: %+v", before.Status)
		}
		if before.Results == nil || len(before.Results.Benchmarks) != 1 ||
			before.Results.Benchmarks[0].LogsPath != final.LogsPath ||
			!reflect.DeepEqual(before.Results.Benchmarks[0].Artifacts, final.Artifacts) {
			t.Fatalf("expected the adapter's logs and artifacts to be saved: %+v", before.Results)
		}

		report := api.BenchmarkStatusEvent{Status: api.StateFailed, Precondition: api.BenchmarkStatusPreconditionNonterminal}
		if err := update(store, id, report); !errors.Is(err, abstractions.ErrBenchmarkAlreadyTerminal) {
			t.Errorf("late conditional report = %v, want already-terminal outcome", err)
		}
		if err := update(store, id, api.BenchmarkStatusEvent{Status: api.StateCompleted}); err == nil {
			t.Error("an ordinary adapter failure must not be replaced by a later completion")
		}
		after := read(t, store, id)
		if !reflect.DeepEqual(after.Status, before.Status) || !reflect.DeepEqual(after.Results, before.Results) {
			t.Errorf("rejected reports changed the adapter status or results: status=%+v results=%+v", after.Status, after.Results)
		}
	})
}
