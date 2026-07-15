package handlers

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/cards"
)

func (h *Handlers) exportEvaluationResults(ctx context.Context, storage abstractions.Storage, job *api.EvaluationJobResource, logger *slog.Logger) {
	if job == nil {
		return
	}
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	card := cards.NewEvaluationCard(job)
	cardJSON, err := json.Marshal(card)
	if err != nil {
		logger.Error("Failed to marshal evaluation card", "job_id", job.Resource.ID, "error", err)
		return
	}
	if err := storage.UpdateEvaluationJobEvalCard(job.Resource.ID, cardJSON); err != nil {
		logger.Error("Failed to persist evaluation card", "job_id", job.Resource.ID, "error", err)
	}

	if h.resultsExporter == nil {
		return
	}

	if _, err := h.resultsExporter.Export(ctx, job, card); err != nil {
		logger.Error("Failed to export evaluation results", "job_id", job.Resource.ID, "error", err)
	}
}
