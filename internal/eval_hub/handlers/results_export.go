package handlers

import (
	"context"
	"log/slog"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/cards"
)

func (h *Handlers) exportEvaluationResults(ctx context.Context, storage abstractions.Storage, job *api.EvaluationJobResource, logger *slog.Logger) {
	if h.resultsExporter == nil || job == nil {
		return
	}
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	card := cards.NewEvaluationCard(job)
	if _, err := h.resultsExporter.Export(ctx, job, card); err != nil {
		logger.Error("Failed to export evaluation results", "job_id", job.Resource.ID, "error", err)
	}

	if storage != nil && job.Results != nil {
		if err := storage.UpdateEvaluationJobResults(job.Resource.ID, job.Results); err != nil {
			logger.Error("Failed to persist evaluation job results after export", "job_id", job.Resource.ID, "error", err)
		}
	}
}
