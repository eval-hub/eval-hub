package handlers

import (
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
	evaluation := postprocessing.ToEvaluationJob(&request)
	if sourceJob != nil {
		evaluation.Exports = copySourceOCIExports(sourceJob)
	}
	job, err := h.createPostProcessingEvaluationJob(ctx, evaluation)
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

// copySourceOCIExports preserves the source registry coordinates and credentials.
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
