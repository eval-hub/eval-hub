package handlers

import (
	"context"
	"errors"
	"fmt"

	"log/slog"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/constants"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/oci"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/cards"
)

// exportEvaluationResults exports the shared card, then saves OCI outputs and progress
// without changing the evaluation outcome. Other destinations can succeed independently.
func (h *Handlers) exportEvaluationResults(ctx context.Context, storage abstractions.Storage, job *api.EvaluationJobResource, logger *slog.Logger) {
	if !api.IsResultsExportEligible(job) {
		return
	}
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	ociEligible := api.IsOCIExportEnabled(job)
	// Persist before updating the in-memory job. Nil outputs preserve stored references.
	reportOCI := func(state api.OCIProcessingState, code, message string, card, bundle *api.OCIArtifactReference) bool {
		status := &api.OCIProcessingStatus{State: state, Message: &api.MessageInfo{Message: message, MessageCode: code, MessageOrigin: api.MessageOriginServer}}
		err := h.persistOCIResults(storage, job, status, card, bundle)
		if err != nil {
			logger.Error("Failed to persist OCI processing", "job_id", job.Resource.ID, "operation", code, "error", err)
			return false
		}
		return true
	}
	// Log the cause even if storage is unavailable and saving the failure also fails.
	failOCI := func(code, message string, card, bundle *api.OCIArtifactReference) {
		logger.Error("OCI processing failed", "job_id", job.Resource.ID, "operation", code, "cause", message)
		reportOCI(api.OCIProcessingFailed, code, message, card, bundle)
	}
	// OCI publishing proceeds only after its running status is saved successfully.
	ociReady := ociEligible
	if ociEligible {
		if !reportOCI(api.OCIProcessingRunning, constants.MessageCodeOCIProcessingRunning, "OCI processing is running", nil, nil) {
			failOCI(constants.MessageCodeOCIProcessingFailed, "Persist running OCI progress: storage update failed", nil, nil)
			ociReady = false
		}
	}
	// OCI and MLflow exports share the same evaluation card.
	card := cards.NewEvaluationCard(job)
	if card == nil || card.Metadata.EvaluationJobID == "" {
		if ociReady {
			failOCI(constants.MessageCodeOCIProcessingFailed, "Generate evaluation card: card has no evaluation job identity", nil, nil)
		}
		return
	}
	if h.resultsExporter == nil {
		if ociReady {
			failOCI(constants.MessageCodeOCIProcessingFailed, "Initialize OCI publisher: results exporter is unavailable", nil, nil)
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
		var exportErr *oci.OperationError
		if errors.As(result.OCIError, &exportErr) {
			failOCI(constants.MessageCodeOCIProcessingFailed, exportErr.Error(), nil, nil)
		} else {
			failOCI(constants.MessageCodeOCIProcessingFailed, "Publish evaluation card: OCI publisher failed", nil, nil)
		}
		return
	}
	if !oci.HasPublishedReference(result.OCIArtifact) {
		failOCI(constants.MessageCodeOCIProcessingFailed, "Publish evaluation card: no published manifest reference is available", nil, nil)
		return
	}
	if err := h.validate.Struct(result.OCIArtifact); err != nil {
		failOCI(constants.MessageCodeOCIProcessingFailed, "Invalid evaluation card: "+err.Error(), nil, nil)
		return
	}
	bundle, err := h.publishEvaluationBundle(ctx, job, result.OCIArtifact)
	if err != nil {
		failOCI(constants.MessageCodeOCIProcessingFailed, err.Error(), result.OCIArtifact, nil)
		return
	}
	// Save both outputs and completion together, without an intermediate card write.
	if !reportOCI(api.OCIProcessingCompleted, constants.MessageCodeOCIProcessingCompleted, "OCI processing completed", result.OCIArtifact, bundle) {
		failOCI(constants.MessageCodeOCIProcessingFailed, "Persist OCI completion: storage update failed", result.OCIArtifact, bundle)
	}
}

// publishEvaluationBundle publishes and validates the bundle without updating job progress.
func (h *Handlers) publishEvaluationBundle(ctx context.Context, job *api.EvaluationJobResource, card *api.OCIArtifactReference) (*api.OCIArtifactReference, error) {
	if h.bundlePublisherFactory == nil {
		return nil, &oci.OperationError{Operation: "Initialize evaluation bundle publisher", Reason: "factory is unavailable"}
	}
	publisher, err := h.bundlePublisherFactory.NewPublisher(ctx, job)
	if err != nil {
		return nil, &oci.OperationError{Operation: "Initialize evaluation bundle publisher", Err: err}
	}
	if publisher == nil {
		return nil, &oci.OperationError{Operation: "Initialize evaluation bundle publisher", Reason: "publisher is unavailable"}
	}
	bundle, err := publisher.Publish(ctx, job, card)
	if err != nil {
		return nil, &oci.OperationError{Operation: "Evaluation bundle OCI publication failed", Err: err}
	}
	if !oci.HasPublishedReference(bundle) {
		return nil, &oci.OperationError{Operation: "Publish evaluation bundle", Reason: "no published manifest reference is available"}
	}
	if err := h.validate.Struct(bundle); err != nil {
		return nil, fmt.Errorf("Invalid evaluation bundle: %w", err)
	}
	return bundle, nil
}

// persistOCIResults validates workflow output before touching storage or the job.
func (h *Handlers) persistOCIResults(storage abstractions.Storage, job *api.EvaluationJobResource, status *api.OCIProcessingStatus, card, bundle *api.OCIArtifactReference) error {
	if err := h.validate.Struct(status); err != nil {
		return serviceerrors.NewServiceError(messages.InternalServerError, "Error", "invalid OCI processing status: "+err.Error())
	}
	for _, card := range []*api.OCIArtifactReference{card, bundle} {
		if card == nil {
			continue
		}
		if err := h.validate.Struct(card); err != nil {
			return serviceerrors.NewServiceError(messages.InternalServerError, "Error", "invalid OCI artifact reference: "+err.Error())
		}
	}
	if storage == nil {
		return serviceerrors.NewServiceError(messages.InternalServerError, "Error", "storage is not configured")
	}
	if err := storage.UpdateEvaluationJobOCI(job.Resource.ID, status, card, bundle); err != nil {
		return err
	}
	job.Results = api.UpdateOCIResults(job.Results, status, card, bundle)
	return nil
}
