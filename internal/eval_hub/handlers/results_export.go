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
	ociEligible := job.Status != nil && job.Status.State == api.OverallStateCompleted && job.Exports != nil && job.Exports.OCI != nil
	// Persist OCI progress before updating the in-memory job. A nil card preserves
	// any previously published reference; a supplied card is saved with the status.
	reportOCI := func(state api.OCIProcessingState, code, message string, card *api.OCIArtifactReference) bool {
		status := &api.OCIProcessingStatus{State: state, Message: &api.MessageInfo{Message: message, MessageCode: code, MessageOrigin: api.MessageOriginServer}}
		var err error
		if storage == nil {
			err = errors.New("storage is not configured")
		} else {
			err = storage.UpdateEvaluationJobOCI(job.Resource.ID, status, card, nil)
		}
		if err != nil {
			logger.Error("Failed to persist OCI evaluation-card processing", "job_id", job.Resource.ID, "operation", code, "error", err)
			return false
		}
		job.Results = api.UpdateOCIResults(job.Results, status, card, nil)
		return true
	}
	// Log the cause even if storage is unavailable and saving the failure also fails.
	failOCI := func(code, message string, card *api.OCIArtifactReference) {
		logger.Error("OCI evaluation-card processing failed", "job_id", job.Resource.ID, "operation", code, "cause", message)
		reportOCI(api.OCIProcessingFailed, code, message, card)
	}
	// OCI publishing proceeds only after its running status is saved successfully.
	ociReady := ociEligible
	if ociEligible {
		if !reportOCI(api.OCIProcessingRunning, constants.MessageCodeOCIProcessingRunning, "Evaluation-card OCI processing is running", nil) {
			failOCI(constants.MessageCodeOCIProcessingFailed, "Persist running OCI progress: storage update failed", nil)
			ociReady = false
		}
	}
	// OCI and MLflow exports share the same evaluation card.
	card := cards.NewEvaluationCard(job)
	if card == nil || card.Metadata.EvaluationJobID == "" {
		if ociReady {
			failOCI(constants.MessageCodeOCIProcessingFailed, "Generate evaluation card: card has no evaluation job identity", nil)
		}
		return
	}
	if h.resultsExporter == nil {
		if ociReady {
			failOCI(constants.MessageCodeOCIProcessingFailed, "Initialize OCI publisher: results exporter is unavailable", nil)
		}
		return
	}
	exportJob := job
	if ociEligible && !ociReady {
		// If saving OCI progress fails, skip OCI publishing but allow other exports.
		copyJob := *job
		copyExports := *job.Exports
		copyExports.OCI = nil
		copyJob.Exports = &copyExports
		exportJob = &copyJob
	}
	// Upload to all enabled targets, then record the OCI outcome separately below.
	result, err := h.resultsExporter.Export(ctx, exportJob, card)
	if err != nil {
		logger.Error("Failed to export evaluation results", "job_id", job.Resource.ID, "error", err)
	}
	if !ociReady {
		return
	}
	if result.OCIError != nil {
		var exportErr *evalcards.OCIExportError
		if errors.As(result.OCIError, &exportErr) {
			failOCI(constants.MessageCodeOCIProcessingFailed, exportErr.Error(), nil)
		} else {
			failOCI(constants.MessageCodeOCIProcessingFailed, "Publish evaluation card: OCI publisher failed", nil)
		}
		return
	}
	if result.OCIArtifact == nil || result.OCIArtifact.OCIDigest == "" || result.OCIArtifact.OCIReference == "" {
		failOCI(constants.MessageCodeOCIProcessingFailed, "Publish evaluation card: no published manifest reference is available", nil)
		return
	}
	// Save outputs and the final status together. Future bundle generation and
	// signing phases run before this update, without intermediate progress writes.
	if !reportOCI(api.OCIProcessingCompleted, constants.MessageCodeOCIProcessingCompleted, "OCI processing completed", result.OCIArtifact) {
		failOCI(constants.MessageCodeOCIProcessingFailed, "Persist evaluation-card OCI completion: storage update failed", result.OCIArtifact)
	}
}
