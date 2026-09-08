package metrics

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// RecordEvaluationJobStateTransition increments evalhub_evaluation_jobs_total (Prometheus)
// and evalhub.eval.job_state_transitions (OTEL) for a state transition.
func RecordEvaluationJobStateTransition(ctx context.Context, provider, collection, status string) {
	promEvalJobsTotal.WithLabelValues(provider, collection, status).Inc()
	if otelEvalJobStateTransitions != nil {
		otelEvalJobStateTransitions.Add(ctx, 1, metric.WithAttributes(
			attribute.String("provider", provider),
			attribute.String("collection", collection),
			attribute.String("status", status),
		))
	}
}

// ObserveEvaluationJobDuration records the wall-clock duration from job creation to terminal state.
func ObserveEvaluationJobDuration(ctx context.Context, provider, collection string, durationSeconds float64) {
	promEvalJobDuration.WithLabelValues(provider, collection).Observe(durationSeconds)
	if otelEvalJobDuration != nil {
		otelEvalJobDuration.Record(ctx, durationSeconds, metric.WithAttributes(
			attribute.String("provider", provider),
			attribute.String("collection", collection),
		))
	}
}

// IncActiveJobs increments the active-jobs gauge (call on job creation).
func IncActiveJobs(ctx context.Context) {
	promEvalJobsActive.Inc()
	if otelEvalActiveJobs != nil {
		otelEvalActiveJobs.Add(ctx, 1)
	}
}

// DecActiveJobs decrements the active-jobs gauge (call on terminal state or cancellation).
func DecActiveJobs(ctx context.Context) {
	promEvalJobsActive.Dec()
	if otelEvalActiveJobs != nil {
		otelEvalActiveJobs.Add(ctx, -1)
	}
}

// IncQueueDepth increments the queue-depth gauge (call when a job enters pending).
func IncQueueDepth(ctx context.Context) {
	promEvalQueueDepth.Inc()
	if otelEvalQueueDepth != nil {
		otelEvalQueueDepth.Add(ctx, 1)
	}
}

// DecQueueDepth decrements the queue-depth gauge (call when a pending job starts running or reaches terminal state).
func DecQueueDepth(ctx context.Context) {
	promEvalQueueDepth.Dec()
	if otelEvalQueueDepth != nil {
		otelEvalQueueDepth.Add(ctx, -1)
	}
}

// RecordEvaluationError increments evalhub_evaluation_errors_total (Prometheus)
// and evalhub.eval.errors (OTEL) with the given error_type and provider.
func RecordEvaluationError(ctx context.Context, errorType, provider string) {
	promEvalErrorsTotal.WithLabelValues(errorType, provider).Inc()
	if otelEvalErrors != nil {
		otelEvalErrors.Add(ctx, 1, metric.WithAttributes(
			attribute.String("error_type", errorType),
			attribute.String("provider", provider),
		))
	}
}

// ObserveBenchmarkDuration records per-benchmark execution duration.
func ObserveBenchmarkDuration(ctx context.Context, benchmarkName, provider string, durationSeconds float64) {
	promBenchmarkDuration.WithLabelValues(benchmarkName, provider).Observe(durationSeconds)
	if otelEvalBenchmarkDuration != nil {
		otelEvalBenchmarkDuration.Record(ctx, durationSeconds, metric.WithAttributes(
			attribute.String("benchmark_name", benchmarkName),
			attribute.String("provider", provider),
		))
	}
}

// ObserveAPIRequestDuration records API request duration with enriched domain labels.
func ObserveAPIRequestDuration(ctx context.Context, endpoint, method, collection, provider string, durationSeconds float64) {
	promAPIRequestDuration.WithLabelValues(endpoint, method, collection, provider).Observe(durationSeconds)
	if otelEvalAPIRequestDuration != nil {
		otelEvalAPIRequestDuration.Record(ctx, durationSeconds, metric.WithAttributes(
			attribute.String("endpoint", endpoint),
			attribute.String("method", method),
			attribute.String("collection", collection),
			attribute.String("provider", provider),
		))
	}
}
