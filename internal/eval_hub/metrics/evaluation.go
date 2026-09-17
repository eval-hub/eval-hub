package metrics

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// RecordEvaluationJobStateTransition increments evalhub_evaluation_jobs_total (Prometheus)
// and evalhub.eval.job_state_transitions (OTEL) for a state transition. tenant, when non-empty,
// is attached only to the OTEL instrument (see withTenantAttr) — not to the Prometheus Vec,
// whose label set stays fixed to keep cardinality bounded.
func RecordEvaluationJobStateTransition(ctx context.Context, provider, collection, status, tenant string) {
	promEvalJobsTotal.WithLabelValues(provider, collection, status).Inc()
	if otelEvalJobStateTransitions != nil {
		otelEvalJobStateTransitions.Add(ctx, 1, metric.WithAttributes(withTenantAttr([]attribute.KeyValue{
			attribute.String("provider", provider),
			attribute.String("collection", collection),
			attribute.String("status", status),
		}, tenant)...))
	}
}

// ObserveEvaluationJobDuration records the wall-clock duration from job creation to terminal state.
func ObserveEvaluationJobDuration(ctx context.Context, provider, collection string, durationSeconds float64, tenant string) {
	promEvalJobDuration.WithLabelValues(provider, collection).Observe(durationSeconds)
	if otelEvalJobDuration != nil {
		otelEvalJobDuration.Record(ctx, durationSeconds, metric.WithAttributes(withTenantAttr([]attribute.KeyValue{
			attribute.String("provider", provider),
			attribute.String("collection", collection),
		}, tenant)...))
	}
}

// IncActiveJobs increments the active-jobs gauge (call on job creation).
func IncActiveJobs(ctx context.Context, tenant string) {
	promEvalJobsActive.Inc()
	if otelEvalActiveJobs != nil {
		otelEvalActiveJobs.Add(ctx, 1, metric.WithAttributes(withTenantAttr(nil, tenant)...))
	}
}

// DecActiveJobs decrements the active-jobs gauge (call on terminal state or cancellation).
func DecActiveJobs(ctx context.Context, tenant string) {
	promEvalJobsActive.Dec()
	if otelEvalActiveJobs != nil {
		otelEvalActiveJobs.Add(ctx, -1, metric.WithAttributes(withTenantAttr(nil, tenant)...))
	}
}

// IncQueueDepth increments the queue-depth gauge (call when a job enters pending).
func IncQueueDepth(ctx context.Context, tenant string) {
	promEvalQueueDepth.Inc()
	if otelEvalQueueDepth != nil {
		otelEvalQueueDepth.Add(ctx, 1, metric.WithAttributes(withTenantAttr(nil, tenant)...))
	}
}

// DecQueueDepth decrements the queue-depth gauge (call when a pending job starts running or reaches terminal state).
func DecQueueDepth(ctx context.Context, tenant string) {
	promEvalQueueDepth.Dec()
	if otelEvalQueueDepth != nil {
		otelEvalQueueDepth.Add(ctx, -1, metric.WithAttributes(withTenantAttr(nil, tenant)...))
	}
}

// RecordEvaluationError increments evalhub_evaluation_errors_total (Prometheus)
// and evalhub.eval.errors (OTEL) with the given error_type and provider.
func RecordEvaluationError(ctx context.Context, errorType, provider, tenant string) {
	promEvalErrorsTotal.WithLabelValues(errorType, provider).Inc()
	if otelEvalErrors != nil {
		otelEvalErrors.Add(ctx, 1, metric.WithAttributes(withTenantAttr([]attribute.KeyValue{
			attribute.String("error_type", errorType),
			attribute.String("provider", provider),
		}, tenant)...))
	}
}

// ObserveBenchmarkDuration records per-benchmark execution duration.
func ObserveBenchmarkDuration(ctx context.Context, benchmarkName, provider string, durationSeconds float64, tenant string) {
	promBenchmarkDuration.WithLabelValues(benchmarkName, provider).Observe(durationSeconds)
	if otelEvalBenchmarkDuration != nil {
		otelEvalBenchmarkDuration.Record(ctx, durationSeconds, metric.WithAttributes(withTenantAttr([]attribute.KeyValue{
			attribute.String("benchmark_name", benchmarkName),
			attribute.String("provider", provider),
		}, tenant)...))
	}
}

// RecordBenchmarkCompletion increments evalhub_benchmark_completions_total (Prometheus)
// and evalhub.eval.benchmark_completions (OTEL) for a benchmark reaching a terminal state
// (status is one of "completed", "failed", "cancelled" — see api.State), alongside the
// existing duration histogram recorded by ObserveBenchmarkDuration.
func RecordBenchmarkCompletion(ctx context.Context, benchmarkName, provider, status, tenant string) {
	promBenchmarkCompletionsTotal.WithLabelValues(benchmarkName, provider, status).Inc()
	if otelEvalBenchmarkCompletion != nil {
		otelEvalBenchmarkCompletion.Add(ctx, 1, metric.WithAttributes(withTenantAttr([]attribute.KeyValue{
			attribute.String("benchmark_name", benchmarkName),
			attribute.String("provider", provider),
			attribute.String("status", status),
		}, tenant)...))
	}
}

// ObserveAPIRequestDuration records API request duration with enriched domain labels.
func ObserveAPIRequestDuration(ctx context.Context, endpoint, method, collection, provider string, durationSeconds float64, tenant string) {
	promAPIRequestDuration.WithLabelValues(endpoint, method, collection, provider).Observe(durationSeconds)
	if otelEvalAPIRequestDuration != nil {
		otelEvalAPIRequestDuration.Record(ctx, durationSeconds, metric.WithAttributes(withTenantAttr([]attribute.KeyValue{
			attribute.String("endpoint", endpoint),
			attribute.String("method", method),
			attribute.String("collection", collection),
			attribute.String("provider", provider),
		}, tenant)...))
	}
}
