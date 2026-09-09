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
	newState := job.Status.State
	metrics.RecordEvaluationJobTerminalState(ctx, previousState, newState)

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
		metrics.RecordEvaluationJobStateTransition(ctx, pid, collectionID, string(newState))
	}

	if previousState == api.OverallStatePending && newState == api.OverallStateRunning {
		metrics.DecQueueDepth(ctx)
	}

	if newState.IsTerminalState() && !previousState.IsTerminalState() {
		metrics.DecActiveJobs(ctx)
		if previousState == api.OverallStatePending {
			metrics.DecQueueDepth(ctx)
		}

		durationSeconds := time.Since(job.Resource.CreatedAt).Seconds()
		for _, pid := range providerIDs {
			metrics.ObserveEvaluationJobDuration(ctx, pid, collectionID, durationSeconds)
		}

		recordBenchmarkDurations(ctx, job)

		if newState == api.OverallStateFailed || newState == api.OverallStatePartiallyFailed {
			for _, pid := range providerIDs {
				metrics.RecordEvaluationError(ctx, "job_failed", pid)
			}
		}
	}
}

func recordBenchmarkDurations(ctx context.Context, job *api.EvaluationJobResource) {
	if job.Status == nil {
		return
	}
	for _, bs := range job.Status.Benchmarks {
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
			metrics.ObserveBenchmarkDuration(ctx, bs.ID, bs.ProviderID, duration)
		}
	}
}
