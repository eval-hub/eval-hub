package handlers

import (
	"context"
	"errors"
	"fmt"

	"log/slog"
	"runtime/debug"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/constants"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/oci"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/internal/otel"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/cards"
)

const exportStorageTimeout = 30 * time.Second

func (h *Handlers) exportEvaluationResultsAsync(ctx context.Context, storage abstractions.Storage, job *api.EvaluationJobResource, logger *slog.Logger) {
	if !api.IsResultsExportEligible(job) {
		return
	}
	// The handler continues reading the original job for threshold notifications
	// and OTEL log export while this worker updates OCI results. Even one writer
	// can race with those readers, so give the worker its own job, Results, and OCI
	// structs. Existing status/artifact pointers are replaced, not mutated in place;
	// other nested data remains shared and read-only in these paths. Copy it too if
	// future export stages mutate it. This snapshot does not prevent duplicate workers.
	snapshot := *job
	if job.Results != nil {
		results := *job.Results
		snapshot.Results = &results
		if results.OCI != nil {
			oci := *results.OCI
			results.OCI = &oci
		}
	}
	// Background exports outlive the request; carry its span as a link source.
	go h.exportEvaluationResults(otel.DetachedContext(ctx), storage, &snapshot, logger)
}

func (h *Handlers) exportEvaluationResults(ctx context.Context, storage abstractions.Storage, job *api.EvaluationJobResource, logger *slog.Logger) {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	ociEligible := api.IsOCIExportEnabled(job)
	ctx = context.WithoutCancel(ctx)
	ctx, span := otel.StartLinkedSpan(ctx, "eval-hub/handlers", "evaluation-results-export")
	defer span.End()
	// Persist OCI progress before updating the in-memory job. A nil card preserves
	// any previously published reference; a supplied card is saved with the status.
	reportOCI := func(state api.OCIProcessingState, code, message string, card, bundle *api.OCIArtifactReference) bool {
		status := &api.OCIProcessingStatus{State: state, Message: &api.MessageInfo{Message: message, MessageCode: code, MessageOrigin: api.MessageOriginServer}}
		writeCtx, cancel := context.WithTimeout(ctx, exportStorageTimeout)
		defer cancel()
		writeStorage := storage
		if storage != nil {
			writeStorage = storage.WithContext(writeCtx)
		}
		err := h.persistOCIResults(writeStorage, job, status, card, bundle)
		if err != nil {
			logger.Error("Failed to persist OCI processing", "job_id", job.Resource.ID, "operation", code, "error", err)
			return false
		}
		return true
	}
	// Log the cause even if storage is unavailable and saving the failure also fails.
	failOCI := func(code, message string, card, bundle *api.OCIArtifactReference) {
		// A malformed reference must not cost us the failure status itself.
		if card != nil && h.validate.Struct(card) != nil {
			card = nil
		}
		logger.Error("OCI evaluation-card processing failed", "job_id", job.Resource.ID, "operation", code, "cause", message)
		reportOCI(api.OCIProcessingFailed, code, message, card, bundle)
	}
	// OCI publishing proceeds only after its running status is saved successfully.
	ociReady := false
	var published *api.OCIArtifactReference
	defer func() {
		if recovered := recover(); recovered != nil {
			// Recovery itself invokes storage and logging; guard against another panic.
			defer func() {
				if nested := recover(); nested != nil {
					logger.Error("Panic while recovering evaluation export", "job_id", job.Resource.ID, "panic", nested, "stack", string(debug.Stack()))
				}
			}()
			logger.Error("Panic during evaluation export", "job_id", job.Resource.ID, "panic", recovered, "stack", string(debug.Stack()))
			if ociReady {
				failOCI(constants.MessageCodeOCIProcessingFailed, "Evaluation-card OCI processing panicked", published, nil)
			}
		}
	}()
	if ociEligible {
		ociReady = reportOCI(api.OCIProcessingRunning, constants.MessageCodeOCIProcessingRunning, "Evaluation-card OCI processing is running", nil, nil)
		if !ociReady {
			failOCI(constants.MessageCodeOCIProcessingFailed, "Persist running OCI progress: storage update failed", nil, nil)
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
	published = result.OCIArtifact
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
	if result.OCIArtifact == nil {
		failOCI(constants.MessageCodeOCIProcessingFailed, "Publish evaluation card: no published manifest reference is available", nil, nil)
		return
	}
	if err := h.validate.Struct(result.OCIArtifact); err != nil {
		logger.Error("Invalid published OCI evaluation-card reference", "job_id", job.Resource.ID, "error", err)
		failOCI(constants.MessageCodeOCIProcessingFailed, "Publish evaluation card: publisher returned an invalid manifest reference", nil, nil)
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
