package metrics

import (
	"context"

	"github.com/eval-hub/eval-hub/pkg/api"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const instrumentationScope = "github.com/eval-hub/eval-hub/internal/eval_hub/metrics"

var (
	evaluationJobsTotal         metric.Int64Counter
	evaluationJobCompletions    metric.Int64Counter
	benchmarkRuntimeErrorsTotal metric.Int64Counter
)

// Init creates OTEL evaluation job instruments. Call once after otel.SetupOTEL configures the global MeterProvider.
func Init() error {
	meter := otel.Meter(instrumentationScope)

	var err error
	evaluationJobsTotal, err = meter.Int64Counter(
		"evalhub.evaluation_jobs",
		metric.WithDescription("Evaluation job lifecycle events"),
	)
	if err != nil {
		return err
	}

	evaluationJobCompletions, err = meter.Int64Counter(
		"evalhub.evaluation_job_completions",
		metric.WithDescription("Evaluation jobs reaching a terminal state"),
	)
	if err != nil {
		return err
	}

	benchmarkRuntimeErrorsTotal, err = meter.Int64Counter(
		"evalhub.benchmark_runtime_errors",
		metric.WithDescription("Benchmark scheduling or start errors by runtime"),
	)
	if err != nil {
		return err
	}

	return initHTTPMetrics(meter)
}

// RecordEvaluationJobCreated increments the counter when a job is persisted successfully.
func RecordEvaluationJobCreated(ctx context.Context, runtime string) {
	if evaluationJobsTotal == nil {
		return
	}
	evaluationJobsTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("action", "created"),
		attribute.String("runtime", runtime),
	))
}

// RecordEvaluationJobCancelled increments the counter when a job is cancelled (soft delete).
func RecordEvaluationJobCancelled(ctx context.Context) {
	if evaluationJobsTotal == nil {
		return
	}
	evaluationJobsTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("action", "cancelled"),
	))
}

// RecordEvaluationJobRuntimeStartFailed increments the counter when the runtime fails to start a job.
func RecordEvaluationJobRuntimeStartFailed(ctx context.Context, runtime string) {
	if evaluationJobsTotal == nil {
		return
	}
	evaluationJobsTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("action", "runtime_start_failed"),
		attribute.String("runtime", runtime),
	))
}

// RecordEvaluationJobTerminalState records a transition into a terminal job state.
func RecordEvaluationJobTerminalState(ctx context.Context, previous, newState api.OverallState) {
	if evaluationJobCompletions == nil {
		return
	}
	if !newState.IsTerminalState() || previous == newState {
		return
	}
	evaluationJobCompletions.Add(ctx, 1, metric.WithAttributes(
		attribute.String("state", string(newState)),
	))
}

// RecordBenchmarkRuntimeError increments the counter when a runtime fails to schedule or start a benchmark.
func RecordBenchmarkRuntimeError(ctx context.Context, runtime string) {
	if benchmarkRuntimeErrorsTotal == nil {
		return
	}
	benchmarkRuntimeErrorsTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("runtime", runtime),
	))
}
