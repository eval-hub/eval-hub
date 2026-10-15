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
	"github.com/eval-hub/eval-hub/internal/eval_hub/workloads"
	"github.com/eval-hub/eval-hub/internal/logging"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// HandleGetPostProcessing handles GET /api/v1/evaluations/post-processing/{post_processing_job_id}.
func (h *Handlers) HandleGetPostProcessing(ctx *executioncontext.ExecutionContext, req httpwrappers.RequestWrapper, w httpwrappers.ResponseWrapper) {
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

// HandleDeletePostProcessing handles DELETE /api/v1/evaluations/post-processing/{post_processing_job_id}.
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
		w.WriteJSON(nil, http.StatusNoContent)
		return nil
	}, "storage", "delete-post-processing", "post_processing.id", id)
}

// HandleGetJobPostProcessing handles GET /api/v1/evaluations/jobs/{job_id}/post-processing/{post_processing_id}.
func (h *Handlers) HandleGetJobPostProcessing(ctx *executioncontext.ExecutionContext, req httpwrappers.RequestWrapper, w httpwrappers.ResponseWrapper) {
	logging.LogRequestStarted(ctx)
	jobID := req.PathValue(constants.PathParameterJobID)
	if jobID == "" {
		w.Error(serviceerrors.NewServiceError(messages.MissingPathParameter, "ParameterName", constants.PathParameterJobID), ctx.RequestID)
		return
	}
	id := req.PathValue(constants.PathParameterJobPostProcessingID)
	if id == "" {
		w.Error(serviceerrors.NewServiceError(messages.MissingPathParameter, "ParameterName", constants.PathParameterJobPostProcessingID), ctx.RequestID)
		return
	}
	storage := h.getStorage(ctx)
	_ = h.withSpan(ctx, func(runtimeCtx context.Context) error {
		job, err := h.getJobPostProcessingJob(storage.WithContext(runtimeCtx), jobID, id)
		if err != nil {
			w.Error(err, ctx.RequestID)
			return err
		}
		response, err := postprocessing.JobResourceFromJob(job)
		if err != nil {
			err = serviceerrors.NewServiceError(messages.InternalServerError, "Error", err.Error())
			w.Error(err, ctx.RequestID)
			return err
		}
		w.WriteJSON(response, http.StatusOK)
		return nil
	}, "storage", "get-job-post-processing", "job.id", jobID, "post_processing.id", id)
}

// HandleDeleteJobPostProcessing handles DELETE /api/v1/evaluations/jobs/{job_id}/post-processing/{post_processing_id}.
func (h *Handlers) HandleDeleteJobPostProcessing(ctx *executioncontext.ExecutionContext, req httpwrappers.RequestWrapper, w httpwrappers.ResponseWrapper) {
	logging.LogRequestStarted(ctx)
	jobID := req.PathValue(constants.PathParameterJobID)
	if jobID == "" {
		w.Error(serviceerrors.NewServiceError(messages.MissingPathParameter, "ParameterName", constants.PathParameterJobID), ctx.RequestID)
		return
	}
	id := req.PathValue(constants.PathParameterJobPostProcessingID)
	if id == "" {
		w.Error(serviceerrors.NewServiceError(messages.MissingPathParameter, "ParameterName", constants.PathParameterJobPostProcessingID), ctx.RequestID)
		return
	}
	storage := h.getStorage(ctx)
	_ = h.withSpan(ctx, func(runtimeCtx context.Context) error {
		contextualStorage := storage.WithContext(runtimeCtx)
		job, err := h.getJobPostProcessingJob(contextualStorage, jobID, id)
		if err != nil {
			w.Error(err, ctx.RequestID)
			return err
		}
		if h.runtime != nil && (job.Status == nil || job.Status.State != api.OverallStateCancelled) {
			if err := h.runtime.WithLogger(ctx.Logger).WithContext(runtimeCtx).DeleteEvaluationJobResources(job); err != nil {
				ctx.Logger.Error("Failed to delete post-processing runtime resources", "error", err, "id", id)
			}
		}
		if err := contextualStorage.WithWorkloadType(workloads.PostProcessing).DeleteEvaluationJob(id); err != nil {
			w.Error(err, ctx.RequestID)
			return err
		}
		w.WriteJSON(nil, http.StatusOK)
		return nil
	}, "storage", "delete-job-post-processing", "job.id", jobID, "post_processing.id", id)
}

func (h *Handlers) getStandalonePostProcessingJob(storage abstractions.Storage, id string) (*api.EvaluationJobResource, error) {
	job, err := h.getPostProcessingJob(storage, id)
	if err != nil {
		return nil, err
	}
	operations, err := postprocessing.OperationsFromJob(&job.EvaluationJobConfig)
	if err != nil {
		return nil, err
	}
	if operations.ConfidenceInterval == nil || operations.ConfidenceInterval.ResultsDataRef == nil || operations.ConfidenceInterval.ResultsDataRef.EvalJob != nil {
		return nil, postProcessingNotFound(id)
	}
	return job, nil
}

func (h *Handlers) getPostProcessingJob(storage abstractions.Storage, id string) (*api.EvaluationJobResource, error) {
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

func (h *Handlers) getJobPostProcessingJob(storage abstractions.Storage, jobID, postProcessingID string) (*api.EvaluationJobResource, error) {
	sourceStorage := storage.WithWorkloadType(workloads.Evaluation)
	if _, err := sourceStorage.GetEvaluationJob(jobID); err != nil {
		return nil, err
	}
	postProcessingStorage := storage.WithWorkloadType(workloads.PostProcessing)
	job, err := h.getPostProcessingJob(postProcessingStorage, postProcessingID)
	if err != nil {
		return nil, err
	}
	operations, err := postprocessing.OperationsFromJob(&job.EvaluationJobConfig)
	if err != nil {
		return nil, err
	}
	operation := operations.ConfidenceInterval
	if operation == nil || operation.ResultsDataRef == nil || operation.ResultsDataRef.EvalJob == nil || operation.ResultsDataRef.EvalJob.ID != jobID {
		return nil, postProcessingNotFound(postProcessingID)
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
	var sourceJob *api.EvaluationJobResource
	if operation := request.Operations.ConfidenceInterval; operation != nil {
		var err error
		sourceJob, err = h.validatePostProcessingResultsSource(ctx, operation.ResultsDataRef)
		if err != nil {
			w.Error(err, ctx.RequestID)
			return
		}
	}
	job, err := h.createPostProcessingEvaluationJobFromRequest(ctx, &request, sourceJob)
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

// HandleCreateJobPostProcessing handles POST /api/v1/evaluations/jobs/{job_id}/post-processing.
func (h *Handlers) HandleCreateJobPostProcessing(ctx *executioncontext.ExecutionContext, req httpwrappers.RequestWrapper, w httpwrappers.ResponseWrapper) {
	logging.LogRequestStarted(ctx)
	jobID := req.PathValue(constants.PathParameterJobID)
	if jobID == "" {
		w.Error(serviceerrors.NewServiceError(messages.MissingPathParameter, "ParameterName", constants.PathParameterJobID), ctx.RequestID)
		return
	}
	body, err := req.BodyAsBytes()
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}
	var request api.ConfidenceIntervalPostProcessingRequest
	if err := serialization.Unmarshal(h.validate, ctx, body, &request); err != nil {
		w.Error(err, ctx.RequestID)
		return
	}
	operation := request.Operations.ConfidenceInterval
	sourceRef := &api.PostProcessingResultsDataRef{EvalJob: &api.EvaluationJobDataRef{
		ID:                 jobID,
		NumParallelThreads: operation.NumParallelThreads,
	}}
	sourceJob, err := h.validatePostProcessingResultsSource(ctx, sourceRef)
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}

	standaloneRequest := &api.StandalonePostProcessingRequest{
		PostProcessingCommon: request.PostProcessingCommon,
		Operations: api.StandalonePostProcessingOperations{
			ConfidenceInterval: &api.StandaloneConfidenceIntervalConfig{
				ConfidenceIntervalConfigCommon: operation.ConfidenceIntervalConfigCommon,
				ResultsDataRef: &api.PostProcessingResultsDataRef{
					EvalJob:    sourceRef.EvalJob,
					DataConfig: operation.ResultsDataConfig,
				},
			},
		},
	}
	job, err := h.createPostProcessingEvaluationJobFromRequest(ctx, standaloneRequest, sourceJob)
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}
	response, err := postprocessing.JobResourceFromJob(job)
	if err != nil {
		w.Error(serviceerrors.NewServiceError(messages.InternalServerError, "Error", err.Error()), ctx.RequestID)
		return
	}
	w.WriteJSON(response, http.StatusAccepted)
}

func (h *Handlers) createPostProcessingEvaluationJobFromRequest(
	ctx *executioncontext.ExecutionContext,
	request *api.StandalonePostProcessingRequest,
	sourceJob *api.EvaluationJobResource,
) (*api.EvaluationJobResource, error) {
	evaluation := postprocessing.ToEvaluationJob(request)
	if sourceJob != nil {
		evaluation.Exports = copySourceOCIExports(sourceJob)
	}
	return h.createPostProcessingEvaluationJob(ctx, evaluation)
}

// validatePostProcessingResultsSource checks the stored state of an eval_job
// reference after Unmarshal has validated the schema for every source type.
// External data access and content validation are performed by the adapter.
func (h *Handlers) validatePostProcessingResultsSource(ctx *executioncontext.ExecutionContext, ref *api.PostProcessingResultsDataRef) (*api.EvaluationJobResource, error) {
	if ref.EvalJob == nil {
		return nil, nil
	}
	sourceStorage := h.getStorage(ctx).WithContext(ctx.Ctx).WithWorkloadType(workloads.Evaluation)
	source, err := sourceStorage.GetEvaluationJob(ref.EvalJob.ID)
	if err != nil {
		return nil, err
	}
	if source == nil || workloads.TypeForJob(&source.EvaluationJobConfig) != workloads.Evaluation {
		return nil, serviceerrors.NewServiceError(messages.ResourceNotFound, "Type", "evaluation job", "ResourceId", ref.EvalJob.ID)
	}
	if source.Status == nil || source.Status.State != api.OverallStateCompleted {
		return nil, serviceerrors.NewServiceError(messages.PostProcessingSourceNotCompleted, "Id", source.Resource.ID)
	}
	return source, nil
}

// copySourceOCIExports carries the source job's OCI coordinates and runtime
// credentials reference into the generated job so its sidecar can download
// per-sample results from the same registry.
func copySourceOCIExports(source *api.EvaluationJobResource) *api.EvaluationExports {
	if source == nil || source.Exports == nil || source.Exports.OCI == nil {
		return nil
	}

	oci := *source.Exports.OCI
	if annotations := source.Exports.OCI.Coordinates.Annotations; annotations != nil {
		oci.Coordinates.Annotations = make(map[string]string, len(annotations))
		for key, value := range annotations {
			oci.Coordinates.Annotations[key] = value
		}
	}
	if connection := source.Exports.OCI.K8s; connection != nil {
		copiedConnection := *connection
		oci.K8s = &copiedConnection
	}
	return &api.EvaluationExports{OCI: &oci}
}
