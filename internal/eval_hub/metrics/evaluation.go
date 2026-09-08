package metrics

import "context"

// RecordEvaluationJobStateTransition increments evalhub_evaluation_jobs_total
// for a state transition (pending, running, completed, failed, cancelled, partially_failed).
func RecordEvaluationJobStateTransition(_ context.Context, provider, collection, status string) {
	promEvalJobsTotal.WithLabelValues(provider, collection, status).Inc()
}

// ObserveEvaluationJobDuration records the wall-clock duration from job creation to terminal state.
func ObserveEvaluationJobDuration(_ context.Context, provider, collection string, durationSeconds float64) {
	promEvalJobDuration.WithLabelValues(provider, collection).Observe(durationSeconds)
}

// IncActiveJobs increments the active-jobs gauge (call on job creation).
func IncActiveJobs(_ context.Context) {
	promEvalJobsActive.Inc()
}

// DecActiveJobs decrements the active-jobs gauge (call on terminal state or cancellation).
func DecActiveJobs(_ context.Context) {
	promEvalJobsActive.Dec()
}

// IncQueueDepth increments the queue-depth gauge (call when a job enters pending).
func IncQueueDepth(_ context.Context) {
	promEvalQueueDepth.Inc()
}

// DecQueueDepth decrements the queue-depth gauge (call when a pending job starts running or reaches terminal state).
func DecQueueDepth(_ context.Context) {
	promEvalQueueDepth.Dec()
}

// RecordEvaluationError increments evalhub_evaluation_errors_total with the given error_type and provider.
func RecordEvaluationError(_ context.Context, errorType, provider string) {
	promEvalErrorsTotal.WithLabelValues(errorType, provider).Inc()
}

// ObserveBenchmarkDuration records per-benchmark execution duration.
func ObserveBenchmarkDuration(_ context.Context, benchmarkName, provider string, durationSeconds float64) {
	promBenchmarkDuration.WithLabelValues(benchmarkName, provider).Observe(durationSeconds)
}

// ObserveAPIRequestDuration records API request duration with enriched domain labels.
func ObserveAPIRequestDuration(_ context.Context, endpoint, method, collection, provider string, durationSeconds float64) {
	promAPIRequestDuration.WithLabelValues(endpoint, method, collection, provider).Observe(durationSeconds)
}
