package handlers

import (
	"context"
	"log/slog"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/pkg/cards"
)

func (h *Handlers) exportEvaluationResults(ctx context.Context, storage abstractions.Storage, jobID string, logger *slog.Logger) {
	if h.resultsExporter == nil {
		return
	}
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	job, err := storage.GetEvaluationJob(jobID)
	if err != nil {
		logger.Error("Failed to load evaluation job for results export", "job_id", jobID, "error", err)
		return
	}
	if job == nil {
		return
	}

	card := cards.NewEvaluationCard(job)
	cardURL, err := h.resultsExporter.Export(ctx, job, card)
	if err != nil {
		logger.Error("Failed to export evaluation results", "job_id", jobID, "error", err)
	}
	if cardURL == "" {
		return
	}

	if err := storage.UpdateEvaluationJobCardURL(jobID, cardURL); err != nil {
		logger.Error("Failed to persist evaluation card URL", "job_id", jobID, "card_url", cardURL, "error", err)
		return
	}
	logger.Info("Persisted evaluation card URL", "job_id", jobID, "card_url", cardURL)
}
