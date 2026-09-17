package handlers

import (
	"context"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/metrics"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func recordEvaluationJobTerminalStateAfterUpdate(
	ctx context.Context,
	getJob func() (*api.EvaluationJobResource, error),
	previousState api.OverallState,
) {
	job, err := getJob()
	if err != nil || job == nil || job.Status == nil {
		return
	}
	tenant := job.Resource.Tenant.String()
	newState := job.Status.State
	metrics.RecordEvaluationJobTerminalState(ctx, previousState, newState, tenant)

	if previousState == newState {
		return
	}

	collectionID := jobCollectionID(&job.EvaluationJobConfig)
	providerIDs := jobProviderIDs(nil, &job.EvaluationJobConfig)
	if len(providerIDs) == 0 && job.Status != nil {
		seen := make(map[string]struct{})
		for _, bs := range job.Status.Benchmarks {
			if _, ok := seen[bs.ProviderID]; !ok {
				seen[bs.ProviderID] = struct{}{}
				providerIDs = append(providerIDs, bs.ProviderID)
			}
		}
	}

	for _, pid := range providerIDs {
		metrics.RecordEvaluationJobStateTransition(ctx, pid, collectionID, string(newState), tenant)
	}

	if previousState == api.OverallStatePending && newState == api.OverallStateRunning {
		metrics.DecQueueDepth(ctx, tenant)
	}

	if newState.IsTerminalState() && !previousState.IsTerminalState() {
		metrics.DecActiveJobs(ctx, tenant)
		if previousState == api.OverallStatePending {
			metrics.DecQueueDepth(ctx, tenant)
		}

		durationSeconds := time.Since(job.Resource.CreatedAt).Seconds()
		for _, pid := range providerIDs {
			metrics.ObserveEvaluationJobDuration(ctx, pid, collectionID, durationSeconds, tenant)
		}

		recordBenchmarkDurations(ctx, job, tenant)

		if newState == api.OverallStateFailed || newState == api.OverallStatePartiallyFailed {
			for _, pid := range providerIDs {
				metrics.RecordEvaluationError(ctx, "job_failed", pid, tenant)
			}
		}
	}
}

// recordEvaluationJobTerminalTransition records the per-provider state transition and job
// duration histogram for a job moving directly into a terminal state (used by the
// runtime-start-failure and cancellation paths, where the terminal state is known up front
// rather than discovered by re-fetching the job — see recordEvaluationJobTerminalStateAfterUpdate
// for the update-triggered case), then decrements the active-jobs (and, if the job was still
// queued, queue-depth) gauges.
func recordEvaluationJobTerminalTransition(
	ctx context.Context,
	previousState, newState api.OverallState,
	providerIDs []string,
	collectionID string,
	createdAt time.Time,
	tenant string,
) {
	metrics.RecordEvaluationJobTerminalState(ctx, previousState, newState, tenant)

	durationSeconds := time.Since(createdAt).Seconds()
	for _, pid := range providerIDs {
		metrics.RecordEvaluationJobStateTransition(ctx, pid, collectionID, string(newState), tenant)
		metrics.ObserveEvaluationJobDuration(ctx, pid, collectionID, durationSeconds, tenant)
	}

	metrics.DecActiveJobs(ctx, tenant)
	if previousState == api.OverallStatePending {
		metrics.DecQueueDepth(ctx, tenant)
	}
}

func recordBenchmarkDurations(ctx context.Context, job *api.EvaluationJobResource, tenant string) {
	if job.Status == nil {
		return
	}
	for _, bs := range job.Status.Benchmarks {
		if api.IsBenchmarkTerminalState(bs.Status) {
			metrics.RecordBenchmarkCompletion(ctx, bs.ID, bs.ProviderID, string(bs.Status), tenant)
		}

		if bs.StartedAt == "" || bs.CompletedAt == "" {
			continue
		}
		started, err := api.DateTimeFromString(bs.StartedAt)
		if err != nil {
			continue
		}
		completed, err := api.DateTimeFromString(bs.CompletedAt)
		if err != nil {
			continue
		}
		duration := completed.Sub(started).Seconds()
		if duration > 0 {
			metrics.ObserveBenchmarkDuration(ctx, bs.ID, bs.ProviderID, duration, tenant)
		}
	}
}
