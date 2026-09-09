package metrics

import (
	"context"

	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const instrumentationScope = "github.com/eval-hub/eval-hub/internal/eval_hub/metrics"

// Separate OTEL meter scope for evaluation-domain instruments.
// Distinct from instrumentationScope so the bridged Prometheus names
// (evalhub_eval_*) don't collide with the promauto names (evalhub_evaluation_*).
const evaluationInstrumentationScope = "evalhub.evaluation"

// Existing OTEL-only instruments (dot-naming convention).
var (
	evaluationJobsTotal         metric.Int64Counter
	evaluationJobCompletions    metric.Int64Counter
	benchmarkRuntimeErrorsTotal metric.Int64Counter
)

// OTEL evaluation-domain instruments for OTLP export.
// These use the evaluationInstrumentationScope meter so their bridged
// Prometheus names (evalhub_eval_*) are distinct from the promauto names.
var (
	otelEvalJobStateTransitions metric.Int64Counter
	otelEvalJobDuration         metric.Float64Histogram
	otelEvalActiveJobs          metric.Int64UpDownCounter
	otelEvalQueueDepth          metric.Int64UpDownCounter
	otelEvalErrors              metric.Int64Counter
	otelEvalBenchmarkDuration   metric.Float64Histogram
	otelEvalAPIRequestDuration  metric.Float64Histogram
)

// Prometheus-native evaluation-domain metrics.
// Labels are bounded by provider/collection configuration (tens, not thousands).
var (
	promEvalJobsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "evalhub_evaluation_jobs_total",
		Help: "Total evaluation job state transitions by provider, collection, and status.",
	}, []string{"provider", "collection", "status"})

	promEvalJobDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "evalhub_evaluation_job_duration_seconds",
		Help:    "Wall-clock duration from job creation to terminal state.",
		Buckets: prometheus.ExponentialBuckets(1, 2, 14),
	}, []string{"provider", "collection"})

	promEvalJobsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "evalhub_evaluation_jobs_active",
		Help: "Current count of non-terminal evaluation jobs (pending + running).",
	})

	promEvalQueueDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "evalhub_evaluation_queue_depth",
		Help: "Current count of evaluation jobs in pending state.",
	})

	promEvalErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "evalhub_evaluation_errors_total",
		Help: "Evaluation errors by type and provider.",
	}, []string{"error_type", "provider"})

	promBenchmarkDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "evalhub_benchmark_duration_seconds",
		Help:    "Per-benchmark execution duration.",
		Buckets: prometheus.ExponentialBuckets(1, 2, 14),
	}, []string{"benchmark_name", "provider"})

	promAPIRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "evalhub_api_request_duration_seconds",
		Help:    "API request duration with domain-enriched labels.",
		Buckets: prometheus.DefBuckets,
	}, []string{"endpoint", "method", "collection", "provider"})
)

// Init creates OTEL evaluation job instruments. Call once after otel.SetupOTEL configures the global MeterProvider.
func Init() error {
	meter := otel.Meter(instrumentationScope)

	var err error
	evaluationJobsTotal, err = meter.Int64Counter(
		"evalhub.evaluation_job_actions",
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

	if err := initHTTPMetrics(meter); err != nil {
		return err
	}

	return initEvaluationOTELMetrics()
}

func initEvaluationOTELMetrics() error {
	evalMeter := otel.Meter(evaluationInstrumentationScope)

	var err error
	otelEvalJobStateTransitions, err = evalMeter.Int64Counter(
		"evalhub.eval.job_state_transitions",
		metric.WithDescription("Evaluation job state transitions by provider, collection, and status"),
	)
	if err != nil {
		return err
	}

	otelEvalJobDuration, err = evalMeter.Float64Histogram(
		"evalhub.eval.job_duration",
		metric.WithDescription("Wall-clock duration from job creation to terminal state"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return err
	}

	otelEvalActiveJobs, err = evalMeter.Int64UpDownCounter(
		"evalhub.eval.active_jobs",
		metric.WithDescription("Current count of non-terminal evaluation jobs (pending + running)"),
	)
	if err != nil {
		return err
	}

	otelEvalQueueDepth, err = evalMeter.Int64UpDownCounter(
		"evalhub.eval.queue_depth",
		metric.WithDescription("Current count of evaluation jobs in pending state"),
	)
	if err != nil {
		return err
	}

	otelEvalErrors, err = evalMeter.Int64Counter(
		"evalhub.eval.errors",
		metric.WithDescription("Evaluation errors by type and provider"),
	)
	if err != nil {
		return err
	}

	otelEvalBenchmarkDuration, err = evalMeter.Float64Histogram(
		"evalhub.eval.benchmark_duration",
		metric.WithDescription("Per-benchmark execution duration"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return err
	}

	otelEvalAPIRequestDuration, err = evalMeter.Float64Histogram(
		"evalhub.eval.api_request_duration",
		metric.WithDescription("API request duration with domain-enriched labels"),
		metric.WithUnit("s"),
	)
	return err
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
