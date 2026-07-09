package handlers

import (
	"context"
	"log/slog"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/otel"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func (h *Handlers) onEvaluationJobUpdated(
	ctx context.Context,
	storage abstractions.Storage,
	getJob func() (*api.EvaluationJobResource, error),
	previousState api.OverallState,
	logger *slog.Logger,
) {
	recordEvaluationJobTerminalStateAfterUpdate(ctx, getJob, previousState)

	job, err := getJob()
	if err != nil || job == nil || job.Status == nil {
		return
	}
	if !job.Status.State.IsTerminalState() || previousState == job.Status.State {
		return
	}

	h.exportEvaluationResults(ctx, storage, job.Resource.ID, logger)

	if h.serviceConfig == nil || !h.serviceConfig.IsOTELJobContainerLogsEnabled() || h.runtime == nil {
		return
	}

	benchmarks, err := h.resolveJobBenchmarksForStorage(storage, job)
	if err != nil {
		if logger != nil {
			logger.WarnContext(ctx, "failed to resolve benchmarks for OTEL container log export",
				"job_id", job.Resource.ID,
				"error", err,
			)
		}
		return
	}

	otel.ExportJobContainerLogsAsync(ctx, h.runtime, job, benchmarks, logger)
}

func (h *Handlers) resolveJobBenchmarksForStorage(storage abstractions.Storage, job *api.EvaluationJobResource) ([]api.EvaluationBenchmarkConfig, error) {
	var collection *api.CollectionResource
	if job.Collection != nil && job.Collection.ID != "" {
		var err error
		collection, err = storage.GetCollection(job.Collection.ID)
		if err != nil {
			return nil, err
		}
	}
	return GetJobBenchmarks(job, collection)
}
