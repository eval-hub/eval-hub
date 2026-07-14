package handlers

import (
	"context"
	"log/slog"

	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/cards"
)

func (h *Handlers) exportEvaluationResults(ctx context.Context, job *api.EvaluationJobResource, logger *slog.Logger) {
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
}
