package handlers

import (
	"context"
	"errors"
	"log/slog"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/constants"
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
		job.Results = api.MergeOCIResults(job.Results, status, card)
		return true
	}
	fail := func(code, message string, card *api.OCIArtifactReference) {
		logger.Error("OCI evaluation-card processing failed", "job_id", job.Resource.ID, "operation", code, "cause", message)
		report(api.OCIProcessingFailed, code, message, card)
	}
	ready := eligible
	if eligible {
		if !report(api.OCIProcessingRunning, constants.MessageCodeOCIProcessingRunning, "Evaluation-card OCI processing is running", nil) {
			fail(constants.MessageCodeOCIProcessingFailed, "Persist running OCI progress: storage update failed", nil)
			ready = false
		}
	}
	card := cards.NewEvaluationCard(job)
	if card == nil || card.Metadata.EvaluationJobID == "" {
		if ready {
			fail(constants.MessageCodeOCIProcessingFailed, "Generate evaluation card: card has no evaluation job identity", nil)
		}
		return
	}
	if h.resultsExporter == nil {
		if ready {
			fail(constants.MessageCodeOCIProcessingFailed, "Initialize OCI publisher: results exporter is unavailable", nil)
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
			fail(constants.MessageCodeOCIProcessingFailed, exportErr.Error(), nil)
		} else {
			fail(constants.MessageCodeOCIProcessingFailed, "Publish evaluation card: OCI publisher failed", nil)
		}
		return
	}
	if result.OCIArtifact == nil || result.OCIArtifact.OCIDigest == "" || result.OCIArtifact.OCIReference == "" {
		fail(constants.MessageCodeOCIProcessingFailed, "Publish evaluation card: no published manifest reference is available", nil)
		return
	}
	// Save outputs and the final status together. Future bundle generation and
	// signing phases run before this update, without intermediate progress writes.
	if !report(api.OCIProcessingCompleted, constants.MessageCodeOCIProcessingCompleted, "OCI processing completed", result.OCIArtifact) {
		fail(constants.MessageCodeOCIProcessingFailed, "Persist evaluation-card OCI completion: storage update failed", result.OCIArtifact)
	}
}
