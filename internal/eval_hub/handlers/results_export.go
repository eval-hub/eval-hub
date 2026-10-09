package handlers

import (
	"context"
	"errors"
	"log/slog"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/evalcards"
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
	eligible := job.Status != nil && job.Status.State == api.OverallStateCompleted && job.Exports != nil && job.Exports.OCI != nil
	report := func(state api.OCIProcessingState, code, message string, card *api.OCIArtifactReference) bool {
		status := &api.OCIProcessingStatus{State: state, Message: &api.MessageInfo{Message: message, MessageCode: code, MessageOrigin: api.MessageOriginServer}}
		var err error
		if storage == nil {
			err = errors.New("storage is not configured")
		} else {
			err = storage.UpdateEvaluationJobOCI(job.Resource.ID, status, card)
		}
		if err != nil {
			logger.Error("Failed to persist OCI evaluation-card processing", "job_id", job.Resource.ID, "operation", code, "error", err)
			return false
		}
		if job.Results == nil {
			job.Results = &api.EvaluationJobResults{}
		}
		if job.Results.OCI == nil {
			job.Results.OCI = &api.EvaluationOCIResults{}
		}
		job.Results.OCI.Status = status
		if card != nil {
			job.Results.OCI.EvaluationCard = card
		}
		return true
	}
	fail := func(code, message string) {
		logger.Error("OCI evaluation-card processing failed", "job_id", job.Resource.ID, "operation", code, "cause", message)
		report(api.OCIProcessingFailed, code, message, nil)
	}
	ready := eligible
	if eligible {
		if !report(api.OCIProcessingPending, "OCI_PROCESSING_PENDING", "Evaluation-card OCI processing is pending", nil) {
			fail("OCI_CARD_REPORTING_FAILED", "Persist pending OCI progress: storage update failed")
			ready = false
		} else if !report(api.OCIProcessingGeneratingEvaluationCard, "OCI_CARD_GENERATING", "Generating the evaluation card and publishing it to OCI", nil) {
			fail("OCI_CARD_REPORTING_FAILED", "Persist evaluation-card OCI progress: storage update failed")
			ready = false
		}
	}
	card := cards.NewEvaluationCard(job)
	if card == nil || card.Metadata.EvaluationJobID == "" {
		if ready {
			fail("OCI_CARD_GENERATION_FAILED", "Generate evaluation card: card has no evaluation job identity")
		}
		return
	}
	if h.resultsExporter == nil {
		if ready {
			fail("OCI_CARD_ACCESS_FAILED", "Initialize OCI publisher: results exporter is unavailable")
		}
		return
	}
	exportJob := job
	if eligible && !ready {
		// A reporting failure stops OCI processing while independent exports still run.
		copyJob := *job
		copyExports := *job.Exports
		copyExports.OCI = nil
		copyJob.Exports = &copyExports
		exportJob = &copyJob
	}
	result, err := h.resultsExporter.Export(ctx, exportJob, card)
	if err != nil {
		logger.Error("Failed to export evaluation results", "job_id", job.Resource.ID, "error", err)
	}
	if !ready {
		return
	}
	if result.OCIError != nil {
		var exportErr *evalcards.OCIExportError
		if errors.As(result.OCIError, &exportErr) {
			fail(exportErr.Code, exportErr.Error())
		} else {
			fail("OCI_CARD_PUBLICATION_FAILED", "Publish evaluation card: OCI publisher failed")
		}
		return
	}
	if result.OCIArtifact == nil || result.OCIArtifact.OCIDigest == "" || result.OCIArtifact.OCIReference == "" {
		fail("OCI_CARD_PUBLICATION_FAILED", "Publish evaluation card: no published manifest reference is available")
		return
	}
	if !report(api.OCIProcessingGeneratingEvaluationCard, "OCI_CARD_PUBLISHED", "Evaluation card published to OCI; recording its manifest reference", result.OCIArtifact) {
		fail("OCI_CARD_REPORTING_FAILED", "Persist published evaluation-card manifest reference: storage update failed")
		return
	}
	// The future bundle stage takes over here. Until then, completion covers the card only.
	if !report(api.OCIProcessingCompleted, "OCI_CARD_COMPLETED", "Evaluation-card generation and OCI publication completed", nil) {
		fail("OCI_CARD_REPORTING_FAILED", "Persist evaluation-card OCI completion: storage update failed")
	}
}
