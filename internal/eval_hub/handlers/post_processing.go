package handlers

import (
	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/httpwrappers"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/postprocessing"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serialization"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/internal/logging"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// HandleCreatePostProcessing handles POST /api/v1/evaluations/post-processing.
func (h *Handlers) HandleCreatePostProcessing(ctx *executioncontext.ExecutionContext, req httpwrappers.RequestWrapper, w httpwrappers.ResponseWrapper) {
	logging.LogRequestStarted(ctx)
	body, err := req.BodyAsBytes()
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}
	var request api.StandalonePostProcessingRequest
	if err := serialization.Unmarshal(h.validate, ctx, body, &request); err != nil {
		w.Error(err, ctx.RequestID)
		return
	}
	if operation := request.Operations.ConfidenceInterval; operation != nil {
		if err := h.validatePostProcessingResultsSource(ctx, operation.ResultsDataRef); err != nil {
			w.Error(err, ctx.RequestID)
			return
		}
	}
	job, err := h.createPostProcessingEvaluationJob(ctx, postprocessing.ToEvaluationJob(&request))
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}
	response, err := postprocessing.ResourceFromJob(job)
	if err != nil {
		w.Error(serviceerrors.NewServiceError(messages.InternalServerError, "Error", err.Error()), ctx.RequestID)
		return
	}
	w.WriteJSON(response, 202)
}

// validatePostProcessingResultsSource checks the stored state of an eval_job
// reference after Unmarshal has validated the schema for every source type.
// External data access and content validation are performed by the adapter.
// Job-scoped requests can use the same source check before mapping.
func (h *Handlers) validatePostProcessingResultsSource(ctx *executioncontext.ExecutionContext, ref *api.PostProcessingResultsDataRef) error {
	if ref.EvalJob == nil {
		return nil
	}
	source, err := h.getStorage(ctx).GetEvaluationJob(ref.EvalJob.ID)
	if err != nil {
		return err
	}
	if source == nil {
		return serviceerrors.NewServiceError(messages.ResourceNotFound, "Type", "evaluation job", "ResourceId", ref.EvalJob.ID)
	}
	if source.Status == nil || source.Status.State != api.OverallStateCompleted {
		return serviceerrors.NewServiceError(messages.PostProcessingSourceNotCompleted, "Id", source.Resource.ID)
	}
	return nil
}
