package handlers

import (
	"context"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/metrics"
	"github.com/eval-hub/eval-hub/pkg/api"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func setupMetricsForTest(t *testing.T) *metric.ManualReader {
	t.Helper()
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	otel.SetMeterProvider(provider)
	if err := metrics.Init(); err != nil {
		t.Fatalf("metrics.Init: %v", err)
	}
	return reader
}

func collectMetricNames(t *testing.T, reader *metric.ManualReader) map[string]int {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	counts := make(map[string]int)
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			switch data := m.Data.(type) {
			case metricdata.Histogram[float64]:
				for _, dp := range data.DataPoints {
					counts[m.Name] += int(dp.Count)
				}
			case metricdata.Sum[int64]:
				counts[m.Name] += len(data.DataPoints)
			default:
				counts[m.Name]++
			}
		}
	}
	return counts
}

func TestRecordEvaluationJobTerminalStateAfterUpdate(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	otel.SetMeterProvider(provider)

	if err := metrics.Init(); err != nil {
		t.Fatalf("metrics.Init: %v", err)
	}

	recordEvaluationJobTerminalStateAfterUpdate(
		context.Background(),
		func() (*api.EvaluationJobResource, error) {
			return &api.EvaluationJobResource{
				Status: &api.EvaluationJobStatus{
					EvaluationJobState: api.EvaluationJobState{State: api.OverallStateCompleted},
				},
			}, nil
		},
		api.OverallStateRunning,
	)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	found := false
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "evalhub.evaluation_job_completions" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("expected evalhub.evaluation_job_completions metric")
	}
}

// TestRecordEvaluationJobTerminalTransition covers the runtime-start-failure and
// cancellation paths in evaluations.go, which (unlike job-update-triggered terminal
// transitions handled by recordEvaluationJobTerminalStateAfterUpdate) know the terminal
// state up front and must still observe the job duration histogram — previously a gap.
func TestRecordEvaluationJobTerminalTransition(t *testing.T) {
	reader := setupMetricsForTest(t)

	createdAt := time.Now().Add(-5 * time.Second)
	recordEvaluationJobTerminalTransition(
		context.Background(),
		api.OverallStatePending,
		api.OverallStateFailed,
		[]string{"prov-a", "prov-b"},
		"coll-x",
		createdAt,
		"tenant-a",
	)

	counts := collectMetricNames(t, reader)
	if counts["evalhub.eval.job_duration"] == 0 {
		t.Error("expected evalhub.eval.job_duration to be recorded for runtime-start-failure/cancel terminal transition")
	}
	if counts["evalhub.evaluation_job_completions"] == 0 {
		t.Error("expected evalhub.evaluation_job_completions to be recorded")
	}
	if counts["evalhub.eval.job_state_transitions"] == 0 {
		t.Error("expected evalhub.eval.job_state_transitions to be recorded per provider")
	}
}

// TestRecordBenchmarkDurationsRecordsCompletionCounter covers the benchmark success/failure
// counter gap: recordBenchmarkDurations should record a completion for every benchmark in a
// terminal state (completed, failed, cancelled), independent of whether start/end timestamps
// are present for the duration histogram.
func TestRecordBenchmarkDurationsRecordsCompletionCounter(t *testing.T) {
	reader := setupMetricsForTest(t)

	job := &api.EvaluationJobResource{
		Status: &api.EvaluationJobStatus{
			Benchmarks: []api.BenchmarkStatus{
				{ID: "bench-completed", ProviderID: "prov-a", Status: api.StateCompleted},
				{ID: "bench-failed", ProviderID: "prov-a", Status: api.StateFailed},
				{ID: "bench-running", ProviderID: "prov-a", Status: api.StateRunning},
			},
		},
	}

	recordBenchmarkDurations(context.Background(), job, "tenant-a")

	counts := collectMetricNames(t, reader)
	if got := counts["evalhub.eval.benchmark_completions"]; got != 2 {
		t.Errorf("evalhub.eval.benchmark_completions data points = %d, want 2 (only terminal-state benchmarks)", got)
	}
}
