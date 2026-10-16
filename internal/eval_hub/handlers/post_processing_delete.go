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
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/internal/eval_hub/workloads"
	"github.com/eval-hub/eval-hub/internal/logging"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func (h *Handlers) HandleDeletePostProcessing(ctx *executioncontext.ExecutionContext, req httpwrappers.RequestWrapper, w httpwrappers.ResponseWrapper) {
	logging.LogRequestStarted(ctx)
	id := req.PathValue(constants.PathParameterPostProcessingID)
	if id == "" {
		w.Error(serviceerrors.NewServiceError(messages.MissingPathParameter, "ParameterName", constants.PathParameterPostProcessingID), ctx.RequestID)
		return
	}

	storage := h.getStorage(ctx)
	_ = h.withSpan(ctx, func(runtimeCtx context.Context) error {
		scopedContext := storage.WithContext(runtimeCtx).WithWorkloadType(workloads.PostProcessing)
		job, err := h.getStandalonePostProcessingJob(scopedContext, id)
		if err != nil {
			w.Error(err, ctx.RequestID)
			return err
		}

		if h.runtime != nil && (job.Status == nil || job.Status.State != api.OverallStateCancelled) {
			if err := h.runtime.WithLogger(ctx.Logger).WithContext(runtimeCtx).DeleteEvaluationJobResources(job); err != nil {
				ctx.Logger.Error("Failed to delete post-processing runtime resources", "error", err, "id", id)
			}
		}

		if err := scopedContext.DeleteEvaluationJob(id); err != nil {
			w.Error(err, ctx.RequestID)
			return err
		}
		if job.Status == nil || !job.Status.State.IsTerminalState() {
			previousState := api.OverallStatePending
			if job.Status != nil {
				previousState = job.Status.State
			}
			recordEvaluationJobTerminalTransition(
				ctx.Ctx,
				previousState,
				api.OverallStateCancelled,
				jobProviderIDs(nil, &job.EvaluationJobConfig),
				jobCollectionID(&job.EvaluationJobConfig),
				job.Resource.CreatedAt,
				ctx.Tenant.String(),
			)
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
