package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/constants"
	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/httpwrappers"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/postprocessing"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serialization"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/internal/logging"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// HandleGetPostProcessing handles GET /api/v1/evaluations/post-processing/{id}.
func (h *Handlers) HandleGetPostProcessing(ctx *executioncontext.ExecutionContext, req httpwrappers.RequestWrapper, w httpwrappers.ResponseWrapper) {
	logging.LogRequestStarted(ctx)
	id := req.PathValue(constants.PathParameterPostProcessingID)
	if id == "" {
		w.Error(serviceerrors.NewServiceError(messages.MissingPathParameter, "ParameterName", constants.PathParameterPostProcessingID), ctx.RequestID)
		return
	}

	storage := h.getStorage(ctx)
	_ = h.withSpan(ctx, func(runtimeCtx context.Context) error {
		job, err := h.getStandalonePostProcessingJob(storage.WithContext(runtimeCtx), id)
		if err != nil {
			w.Error(err, ctx.RequestID)
			return err
		}
		response, err := postprocessing.ResourceFromJob(job)
		if err != nil {
			err = serviceerrors.NewServiceError(messages.InternalServerError, "Error", err.Error())
			w.Error(err, ctx.RequestID)
			return err
		}
		w.WriteJSON(response, http.StatusOK)
		return nil
	}, "storage", "get-post-processing", "post_processing.id", id)
}

// HandleDeletePostProcessing handles DELETE /api/v1/evaluations/post-processing/{id}.
func (h *Handlers) HandleDeletePostProcessing(ctx *executioncontext.ExecutionContext, req httpwrappers.RequestWrapper, w httpwrappers.ResponseWrapper) {
	logging.LogRequestStarted(ctx)
	id := req.PathValue(constants.PathParameterPostProcessingID)
	if id == "" {
		w.Error(serviceerrors.NewServiceError(messages.MissingPathParameter, "ParameterName", constants.PathParameterPostProcessingID), ctx.RequestID)
		return
	}

	storage := h.getStorage(ctx)
	_ = h.withSpan(ctx, func(runtimeCtx context.Context) error {
		scoped := storage.WithContext(runtimeCtx)
		job, err := h.getStandalonePostProcessingJob(scoped, id)
		if err != nil {
			w.Error(err, ctx.RequestID)
			return err
		}

		if h.runtime != nil && (job.Status == nil || job.Status.State != api.OverallStateCancelled) {
			if err := h.runtime.WithLogger(ctx.Logger).WithContext(runtimeCtx).DeleteEvaluationJobResources(job); err != nil {
				ctx.Logger.Error("Failed to delete post-processing runtime resources", "error", err, "id", id)
			}
		}

		if err := scoped.DeleteEvaluationJob(id); err != nil {
			w.Error(err, ctx.RequestID)
			return err
		}
		w.WriteJSON(nil, http.StatusNoContent)
		return nil
	}, "storage", "delete-post-processing", "post_processing.id", id)
}

func (h *Handlers) getStandalonePostProcessingJob(storage abstractions.Storage, id string) (*api.EvaluationJobResource, error) {
	job, err := storage.GetEvaluationJob(id)
	if err != nil {
		var serviceErr *serviceerrors.ServiceError
		if errors.As(err, &serviceErr) && serviceErr.MessageCode() == messages.ResourceNotFound {
			return nil, postProcessingNotFound(id)
		}
		return nil, err
	}
	if job == nil || !postprocessing.IsPostProcessingJob(&job.EvaluationJobConfig) {
		return nil, postProcessingNotFound(id)
	}
	return job, nil
}

func postProcessingNotFound(id string) error {
	return serviceerrors.NewServiceError(messages.ResourceNotFound, "Type", "post-processing computation", "ResourceId", id)
}

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
	job, err := h.createEvaluationJob(ctx, postprocessing.ToEvaluationJob(&request))
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
