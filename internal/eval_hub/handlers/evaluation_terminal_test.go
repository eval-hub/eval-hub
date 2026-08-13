package handlers

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/cards"
)

// violationRecorder captures NotifyThresholdViolation calls for test assertions.
type violationRecorder struct {
	calls []violationCall
}

type violationCall struct {
	benchmarkIndex int
	metricName     string
	actualValue    float32
	threshold      float32
}

func (r *violationRecorder) WithLogger(_ *slog.Logger) abstractions.Runtime     { return r }
func (r *violationRecorder) WithContext(_ context.Context) abstractions.Runtime { return r }
func (r *violationRecorder) Name() string                                       { return "recorder" }
func (r *violationRecorder) RunEvaluationJob(_ *api.EvaluationJobResource, _ []api.EvaluationBenchmarkConfig, _ abstractions.RuntimeStorage) error {
	return nil
}
func (r *violationRecorder) DeleteEvaluationJobResources(_ *api.EvaluationJobResource) error {
	return nil
}
func (r *violationRecorder) GetEvaluationLogs(_ *api.EvaluationJobResource, _ []api.EvaluationBenchmarkConfig, _ *int, _ api.EvaluationLogOptions) (string, error) {
	return "", nil
}
func (r *violationRecorder) ValidateHardwareProfiles(_ []api.EvaluationBenchmarkConfig) error {
	return nil
}
func (r *violationRecorder) NotifyJobPhaseTransition(_ context.Context, _ *api.EvaluationJobResource, _ int, _ api.State) {
}
func (r *violationRecorder) NotifyThresholdViolation(_ context.Context, _ *api.EvaluationJobResource, benchmarkIndex int, metricName string, actualValue, threshold float32) {
	r.calls = append(r.calls, violationCall{benchmarkIndex, metricName, actualValue, threshold})
}

type terminalTestExporter struct {
	called bool
}

func (e *terminalTestExporter) Export(_ context.Context, _ *api.EvaluationJobResource, _ *cards.EvaluationCard) (string, error) {
	e.called = true
	return "https://example.com/card.json", nil
}

type terminalTestStorage struct {
	noopStorage
	job *api.EvaluationJobResource
}

func (s *terminalTestStorage) GetEvaluationJob(_ string) (*api.EvaluationJobResource, error) {
	return s.job, nil
}

func TestOnEvaluationJobUpdatedSkipsExportWhenNotTerminal(t *testing.T) {
	t.Parallel()
	exporter := &terminalTestExporter{}
	storage := &terminalTestStorage{
		job: &api.EvaluationJobResource{
			Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-1"}},
			Status: &api.EvaluationJobStatus{
				EvaluationJobState: api.EvaluationJobState{State: api.OverallStateRunning},
			},
		},
	}
	h := &Handlers{resultsExporter: exporter}

	h.onEvaluationJobUpdated(
		context.Background(),
		storage,
		func() (*api.EvaluationJobResource, error) { return storage.job, nil },
		api.OverallStatePending,
		nil,
	)

	if exporter.called {
		t.Fatal("expected export to be skipped for non-terminal job")
	}
}

func TestOnEvaluationJobUpdatedSkipsExportWhenTerminalStateUnchanged(t *testing.T) {
	t.Parallel()
	exporter := &terminalTestExporter{}
	storage := &terminalTestStorage{
		job: &api.EvaluationJobResource{
			Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-1"}},
			Status: &api.EvaluationJobStatus{
				EvaluationJobState: api.EvaluationJobState{State: api.OverallStateCompleted},
			},
		},
	}
	h := &Handlers{resultsExporter: exporter}

	h.onEvaluationJobUpdated(
		context.Background(),
		storage,
		func() (*api.EvaluationJobResource, error) { return storage.job, nil },
		api.OverallStateCompleted,
		nil,
	)

	if exporter.called {
		t.Fatal("expected export to be skipped when terminal state did not change")
	}
}

func TestOnEvaluationJobUpdatedExportsOnFailedTransition(t *testing.T) {
	t.Parallel()
	exporter := &terminalTestExporter{}
	storage := &terminalTestStorage{
		job: &api.EvaluationJobResource{
			Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-1"}},
			Status: &api.EvaluationJobStatus{
				EvaluationJobState: api.EvaluationJobState{State: api.OverallStateFailed},
			},
		},
	}
	h := &Handlers{resultsExporter: exporter}

	h.onEvaluationJobUpdated(
		context.Background(),
		storage,
		func() (*api.EvaluationJobResource, error) { return storage.job, nil },
		api.OverallStateRunning,
		nil,
	)

	if !exporter.called {
		t.Fatal("expected export when job transitions to failed")
	}
}

func TestOnEvaluationJobUpdatedSkipsWhenGetJobFails(t *testing.T) {
	t.Parallel()
	exporter := &terminalTestExporter{}
	h := &Handlers{resultsExporter: exporter}

	h.onEvaluationJobUpdated(
		context.Background(),
		&terminalTestStorage{},
		func() (*api.EvaluationJobResource, error) { return nil, errors.New("load failed") },
		api.OverallStateRunning,
		nil,
	)

	if exporter.called {
		t.Fatal("expected export to be skipped when getJob fails")
	}
}

func TestOnEvaluationJobUpdatedSkipsWhenJobNil(t *testing.T) {
	t.Parallel()
	exporter := &terminalTestExporter{}
	h := &Handlers{resultsExporter: exporter}

	h.onEvaluationJobUpdated(
		context.Background(),
		&terminalTestStorage{},
		func() (*api.EvaluationJobResource, error) { return nil, nil },
		api.OverallStateRunning,
		nil,
	)

	if exporter.called {
		t.Fatal("expected export to be skipped when job is nil")
	}
}

func TestResolveJobBenchmarksForStorageWithCollection(t *testing.T) {
	t.Parallel()
	storage := &collectionTerminalStorage{
		terminalTestStorage: terminalTestStorage{
			job: &api.EvaluationJobResource{
				EvaluationJobConfig: api.EvaluationJobConfig{
					Collection: &api.CollectionRef{ID: "col-1"},
				},
			},
		},
		collection: &api.CollectionResource{
			CollectionConfig: api.CollectionConfig{
				Benchmarks: []api.CollectionBenchmarkConfig{
					{Ref: api.Ref{ID: "arc_easy"}, ProviderID: "lm_evaluation_harness"},
				},
			},
		},
	}
	h := &Handlers{}

	benchmarks, err := h.resolveJobBenchmarksForStorage(storage, storage.job)
	if err != nil {
		t.Fatalf("resolveJobBenchmarksForStorage() err = %v", err)
	}
	if len(benchmarks) != 1 || benchmarks[0].ID != "arc_easy" {
		t.Fatalf("benchmarks = %#v", benchmarks)
	}
}

type collectionTerminalStorage struct {
	terminalTestStorage
	collection *api.CollectionResource
}

func (s *collectionTerminalStorage) GetCollection(_ string) (*api.CollectionResource, error) {
	return s.collection, nil
}

func benchWithTest(index int, pass bool, metric string, score, threshold float32) api.BenchmarkResult {
	return api.BenchmarkResult{
		BenchmarkIndex: index,
		Test: &api.BenchmarkTest{
			Pass:               pass,
			PrimaryScoreMetric: metric,
			PrimaryScore:       score,
			Threshold:          threshold,
		},
	}
}

func TestNotifyThresholdViolations_SkipsNilTest(t *testing.T) {
	t.Parallel()
	rec := &violationRecorder{}
	h := &Handlers{runtime: rec}
	job := &api.EvaluationJobResource{
		Results: &api.EvaluationJobResults{
			Benchmarks: []api.BenchmarkResult{
				{BenchmarkIndex: 0, Test: nil},
			},
		},
	}
	h.notifyThresholdViolations(context.Background(), job, nil)
	if len(rec.calls) != 0 {
		t.Fatalf("expected no calls, got %d", len(rec.calls))
	}
}

func TestNotifyThresholdViolations_SkipsPassingBenchmark(t *testing.T) {
	t.Parallel()
	rec := &violationRecorder{}
	h := &Handlers{runtime: rec}
	job := &api.EvaluationJobResource{
		Results: &api.EvaluationJobResults{
			Benchmarks: []api.BenchmarkResult{
				benchWithTest(0, true, "hellaswag.em", 0.8, 0.25),
			},
		},
	}
	h.notifyThresholdViolations(context.Background(), job, nil)
	if len(rec.calls) != 0 {
		t.Fatalf("expected no calls for passing benchmark, got %d", len(rec.calls))
	}
}

func TestNotifyThresholdViolations_FiresForFailingBenchmark(t *testing.T) {
	t.Parallel()
	rec := &violationRecorder{}
	h := &Handlers{runtime: rec}
	job := &api.EvaluationJobResource{
		Results: &api.EvaluationJobResults{
			Benchmarks: []api.BenchmarkResult{
				benchWithTest(2, false, "hellaswag.em", 0.1, 0.99),
			},
		},
	}
	h.notifyThresholdViolations(context.Background(), job, nil)
	if len(rec.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(rec.calls))
	}
	c := rec.calls[0]
	if c.benchmarkIndex != 2 || c.metricName != "hellaswag.em" || c.actualValue != 0.1 || c.threshold != 0.99 {
		t.Fatalf("unexpected call args: %+v", c)
	}
}

func TestNotifyThresholdViolations_MultipleFailingBenchmarks(t *testing.T) {
	t.Parallel()
	rec := &violationRecorder{}
	h := &Handlers{runtime: rec}
	job := &api.EvaluationJobResource{
		Results: &api.EvaluationJobResults{
			Benchmarks: []api.BenchmarkResult{
				benchWithTest(0, false, "arc.em", 0.2, 0.8),
				benchWithTest(1, false, "hellaswag.em", 0.1, 0.99),
			},
		},
	}
	h.notifyThresholdViolations(context.Background(), job, nil)
	if len(rec.calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(rec.calls))
	}
}

func TestNotifyThresholdViolations_MixedResults(t *testing.T) {
	t.Parallel()
	rec := &violationRecorder{}
	h := &Handlers{runtime: rec}
	job := &api.EvaluationJobResource{
		Results: &api.EvaluationJobResults{
			Benchmarks: []api.BenchmarkResult{
				benchWithTest(0, true, "arc.em", 0.9, 0.8),         // passing
				{BenchmarkIndex: 1, Test: nil},                     // nil test
				benchWithTest(2, false, "hellaswag.em", 0.1, 0.99), // failing
			},
		},
	}
	h.notifyThresholdViolations(context.Background(), job, nil)
	if len(rec.calls) != 1 {
		t.Fatalf("expected 1 call for the single failing benchmark, got %d", len(rec.calls))
	}
	if rec.calls[0].benchmarkIndex != 2 {
		t.Fatalf("expected violation for benchmark index 2, got %d", rec.calls[0].benchmarkIndex)
	}
}

func TestOnEvaluationJobUpdatedFiresThresholdViolations(t *testing.T) {
	t.Parallel()
	rec := &violationRecorder{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	job := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-tv"}},
		Status: &api.EvaluationJobStatus{
			EvaluationJobState: api.EvaluationJobState{State: api.OverallStateCompleted},
		},
		Results: &api.EvaluationJobResults{
			Benchmarks: []api.BenchmarkResult{
				benchWithTest(0, false, "accuracy", 0.2, 0.8),
			},
		},
	}
	h := &Handlers{runtime: rec}
	h.onEvaluationJobUpdated(
		context.Background(),
		&terminalTestStorage{job: job},
		func() (*api.EvaluationJobResource, error) { return job, nil },
		api.OverallStateRunning,
		logger,
	)
	if len(rec.calls) != 1 {
		t.Fatalf("expected 1 violation call via onEvaluationJobUpdated, got %d", len(rec.calls))
	}
}

func TestOnEvaluationJobUpdatedSkipsThresholdWhenNilResults(t *testing.T) {
	t.Parallel()
	rec := &violationRecorder{}
	job := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-tv-nil"}},
		Status: &api.EvaluationJobStatus{
			EvaluationJobState: api.EvaluationJobState{State: api.OverallStateCompleted},
		},
	}
	h := &Handlers{runtime: rec}
	h.onEvaluationJobUpdated(
		context.Background(),
		&terminalTestStorage{job: job},
		func() (*api.EvaluationJobResource, error) { return job, nil },
		api.OverallStateRunning,
		nil,
	)
	if len(rec.calls) != 0 {
		t.Fatalf("expected no violation calls when Results is nil, got %d", len(rec.calls))
	}
}

func TestNotifyThresholdViolations_WithLogger(t *testing.T) {
	t.Parallel()
	rec := &violationRecorder{}
	h := &Handlers{runtime: rec}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	job := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-log"}},
		Results: &api.EvaluationJobResults{
			Benchmarks: []api.BenchmarkResult{
				benchWithTest(1, false, "f1_score", 0.3, 0.7),
			},
		},
	}
	h.notifyThresholdViolations(context.Background(), job, logger)
	if len(rec.calls) != 1 {
		t.Fatalf("expected 1 violation call with non-nil logger, got %d", len(rec.calls))
	}
}
