package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/common"
	"github.com/eval-hub/eval-hub/internal/constants"
	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"github.com/eval-hub/eval-hub/internal/http_wrappers"
	"github.com/eval-hub/eval-hub/internal/logging"
	"github.com/eval-hub/eval-hub/internal/messages"
	"github.com/eval-hub/eval-hub/internal/mlflow"
	"github.com/eval-hub/eval-hub/internal/otel"
	"github.com/eval-hub/eval-hub/internal/serialization"
	"github.com/eval-hub/eval-hub/internal/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// BackendSpec represents the backend specification
type BackendSpec struct {
	URL  string `json:"url"`
	Name string `json:"name"`
}

// BenchmarkSpec represents the benchmark specification
type BenchmarkSpec struct {
	BenchmarkID string                 `json:"benchmark_id"`
	ProviderID  string                 `json:"provider_id"`
	Config      map[string]interface{} `json:"config,omitempty"`
}

func getParam[T string | int | bool](r http_wrappers.RequestWrapper, name string, optional bool, defaultValue T) (T, error) {
	values := r.Query(name)
	if (len(values) == 0) || (values[0] == "") {
		if !optional {
			return defaultValue, serviceerrors.NewServiceError(messages.QueryParameterRequired, "ParameterName", name)
		}
		return defaultValue, nil
	}
	switch any(defaultValue).(type) {
	case string:
		return any(values[0]).(T), nil
	case int:
		v, err := strconv.Atoi(values[0])
		if err != nil {
			return defaultValue, serviceerrors.NewServiceError(messages.QueryParameterInvalid, "ParameterName", name, "Type", "integer", "Value", values[0])
		}
		return any(v).(T), nil
	case bool:
		v, err := strconv.ParseBool(values[0])
		if err != nil {
			return defaultValue, serviceerrors.NewServiceError(messages.QueryParameterInvalid, "ParameterName", name, "Type", "boolean", "Value", values[0])
		}
		return any(v).(T), nil
	default:
		// should never get here
		return any(fmt.Sprintf("%v", values[0])).(T), nil
	}
}

// HandleCreateEvaluation handles POST /api/v1/evaluations/jobs
func (h *Handlers) HandleCreateEvaluation(ctx *executioncontext.ExecutionContext, req http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {
	storage := h.storage.WithLogger(ctx.Logger).WithContext(ctx.Ctx).WithTenant(ctx.Tenant)

	logging.LogRequestStarted(ctx)

	now := time.Now()
	id := common.GUID()

	evaluation := &api.EvaluationJobConfig{}

	err := h.withSpan(
		ctx,
		func(runtimeCtx context.Context) error {
			// get the body bytes from the context
			bodyBytes, err := req.BodyAsBytes()
			if err != nil {
				return err
			}
			err = serialization.Unmarshal(h.validate, ctx.WithContext(runtimeCtx), bodyBytes, evaluation)
			if err != nil {
				return err
			}
			return h.validateBenchmarkReferences(evaluation)
		},
		"validation",
		"validate-evaluation-job",
		"job.id", id,
	)

	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}

	mlflowExperimentID := ""
	mlflowExperimentURL := ""
	if h.mlflowClient != nil {
		client := h.mlflowClient.WithContext(ctx.Ctx).WithLogger(ctx.Logger)
		mlflowExperimentID, mlflowExperimentURL, err = mlflow.GetExperimentID(client, evaluation.Experiment)
		if err != nil {
			w.Error(err, ctx.RequestID)
			return
		}
	}

	var job *api.EvaluationJobResource

	err = h.withSpan(
		ctx,
		func(runtimeCtx context.Context) error {
			job = &api.EvaluationJobResource{
				Resource: api.EvaluationResource{
					Resource: api.Resource{
						ID:        id,
						CreatedAt: &now,
						Owner:     ctx.User,
						Tenant:    &ctx.Tenant,
						ReadOnly:  false,
					},
					MLFlowExperimentID: mlflowExperimentID,
				},
				Status: &api.EvaluationJobStatus{
					EvaluationJobState: api.EvaluationJobState{
						State: api.OverallStatePending,
						Message: &api.MessageInfo{
							Message:     "Evaluation job created",
							MessageCode: constants.MESSAGE_CODE_EVALUATION_JOB_CREATED,
						},
					},
				},
				Results: &api.EvaluationJobResults{
					MLFlowExperimentURL: mlflowExperimentURL,
				},
				EvaluationJobConfig: *evaluation,
			}
			return storage.WithContext(runtimeCtx).CreateEvaluationJob(job)
		},
		"storage",
		"store-evaluation-job",
		"job.id", id,
		"job.experiment_id", mlflowExperimentID,
		"job.experiment_url", mlflowExperimentURL,
	)

	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}

	_ = h.withSpan(
		ctx,
		func(runtimeCtx context.Context) (fnErr error) {
			if h.runtime != nil {
				runErr := h.executeEvaluationJob(runtimeCtx, ctx.Logger, h.runtime, job, &storage)
				if runErr != nil {
					ctx.Logger.Error("RunEvaluationJob failed", "error", runErr, "job_id", job.Resource.ID)
					state := api.OverallStateFailed
					message := &api.MessageInfo{
						Message:     runErr.Error(),
						MessageCode: constants.MESSAGE_CODE_EVALUATION_JOB_FAILED,
					}
					if err := storage.UpdateEvaluationJobStatus(job.Resource.ID, state, message); err != nil {
						ctx.Logger.Error("Failed to update evaluation status", "error", err, "job_id", job.Resource.ID)
					}
					// return the first error encountered
					w.Error(runErr, ctx.RequestID)
					return runErr
				}
			}
			w.WriteJSON(job, 202)
			return nil
		},
		"runtime",
		"start-evaluation-job",
		"job.id", id,
		"job.experiment_id", mlflowExperimentID,
		"job.experiment_url", mlflowExperimentURL,
	)
}

func (h *Handlers) executeEvaluationJob(ctx context.Context, logger *slog.Logger, runtime abstractions.Runtime, job *api.EvaluationJobResource, storage *abstractions.Storage) error {
	var err error
	defer func() {
		if recovered := recover(); recovered != nil {
			logger.Error("panic in RunEvaluationJob", "panic", recovered, "stack", string(debug.Stack()), "job_id", job.Resource.ID)
			runtimeErr := serviceerrors.NewServiceError(messages.InternalServerError, "Error", fmt.Sprint(recovered))
			// return the runtime error if not already set
			if err == nil {
				err = runtimeErr
			}
		}
	}()
	err = runtime.WithLogger(logger).WithContext(ctx).RunEvaluationJob(job, storage)
	return err
}

func (h *Handlers) validateBenchmarkReferences(evaluation *api.EvaluationJobConfig) error {
	for _, benchmark := range evaluation.Benchmarks {
		provider, ok := h.providerConfigs[benchmark.ProviderID]
		if !ok {
			return serviceerrors.NewServiceError(
				messages.RequestFieldInvalid,
				"ParameterName", "provider_id",
				"Value", benchmark.ProviderID,
			)
		}
		if !benchmarkExists(provider.Benchmarks, benchmark.ID) {
			return serviceerrors.NewServiceError(
				messages.RequestFieldInvalid,
				"ParameterName", "id",
				"Value", benchmark.ID,
			)
		}
	}
	return nil
}

func benchmarkExists(benchmarks []api.BenchmarkResource, id string) bool {
	for _, benchmark := range benchmarks {
		if benchmark.ID == id {
			return true
		}
	}
	return false
}

// HandleListEvaluations handles GET /api/v1/evaluations/jobs
func (h *Handlers) HandleListEvaluations(ctx *executioncontext.ExecutionContext, r http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {
	storage := h.storage.WithLogger(ctx.Logger).WithContext(ctx.Ctx).WithTenant(ctx.Tenant)

	logging.LogRequestStarted(ctx)

	limit, err := getParam(r, "limit", true, 50)
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}
	offset, err := getParam(r, "offset", true, 0)
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}
	statusFilter, err := getParam(r, "status_filter", true, "")
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}

	res, err := storage.GetEvaluationJobs(limit, offset, abstractions.QueryFilter{
		Params: map[string]string{
			"status_filter": statusFilter,
		},
	})
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}
	page, err := CreatePage(res.TotalStored, offset, limit, ctx, r)
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}
	w.WriteJSON(api.EvaluationJobResourceList{
		Page:   *page,
		Items:  res.Items,
		Errors: res.Errors,
	}, 200)
}

// HandleGetEvaluation handles GET /api/v1/evaluations/jobs/{id}
func (h *Handlers) HandleGetEvaluation(ctx *executioncontext.ExecutionContext, r http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {
	storage := h.storage.WithLogger(ctx.Logger).WithContext(ctx.Ctx).WithTenant(ctx.Tenant)
	logging.LogRequestStarted(ctx)

	// Extract ID from path
	evaluationJobID := r.PathValue(constants.PATH_PARAMETER_JOB_ID)
	if evaluationJobID == "" {
		w.Error(serviceerrors.NewServiceError(messages.MissingPathParameter, "ParameterName", constants.PATH_PARAMETER_JOB_ID), ctx.RequestID)
		return
	}

	response, err := storage.GetEvaluationJob(evaluationJobID)
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}

	w.WriteJSON(response, 200)
}

func (h *Handlers) HandleUpdateEvaluation(ctx *executioncontext.ExecutionContext, r http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {
	storage := h.storage.WithLogger(ctx.Logger).WithContext(ctx.Ctx).WithTenant(ctx.Tenant)
	logging.LogRequestStarted(ctx)

	// Extract ID from path
	evaluationJobID := r.PathValue(constants.PATH_PARAMETER_JOB_ID)
	if evaluationJobID == "" {
		w.Error(serviceerrors.NewServiceError(messages.MissingPathParameter, "ParameterName", constants.PATH_PARAMETER_JOB_ID), ctx.RequestID)
		return
	}

	// get the body bytes from the context
	bodyBytes, err := r.BodyAsBytes()
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}
	status := &api.StatusEvent{}
	err = serialization.Unmarshal(h.validate, ctx, bodyBytes, status)
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}

	err = storage.UpdateEvaluationJob(evaluationJobID, status)
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}

	w.WriteJSON(nil, 204)
}

// HandleCancelEvaluation handles DELETE /api/v1/evaluations/jobs/{id}
func (h *Handlers) HandleCancelEvaluation(ctx *executioncontext.ExecutionContext, r http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {
	storage := h.storage.WithLogger(ctx.Logger).WithContext(ctx.Ctx).WithTenant(ctx.Tenant)
	logging.LogRequestStarted(ctx)

	// Extract ID from path
	evaluationJobID := r.PathValue(constants.PATH_PARAMETER_JOB_ID)
	if evaluationJobID == "" {
		w.Error(serviceerrors.NewServiceError(messages.MissingPathParameter, "ParameterName", constants.PATH_PARAMETER_JOB_ID), ctx.RequestID)
		return
	}

	hardDelete, err := getParam(r, "hard_delete", true, false)
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}

	if hardDelete && h.runtime != nil {
		job, err := storage.GetEvaluationJob(evaluationJobID)
		if err != nil {
			w.Error(err, ctx.RequestID)
			return
		}
		if job != nil {
			if err := h.runtime.WithLogger(ctx.Logger).WithContext(ctx.Ctx).DeleteEvaluationJobResources(job); err != nil {
				// Cleanup failures shouldn't block deleting the storage record.
				ctx.Logger.Error("Failed to delete evaluation runtime resources", "error", err, "id", evaluationJobID)
			}
		}
	}

	if !hardDelete {
		err = storage.UpdateEvaluationJobStatus(evaluationJobID, api.OverallStateCancelled, &api.MessageInfo{
			Message:     "Evaluation job cancelled",
			MessageCode: constants.MESSAGE_CODE_EVALUATION_JOB_CANCELLED,
		})
		if err != nil {
			ctx.Logger.Info("Failed to cancel evaluation job", "error", err.Error(), "id", evaluationJobID)
			w.Error(err, ctx.RequestID)
			return
		}
	} else {
		err = storage.DeleteEvaluationJob(evaluationJobID)
		if err != nil {
			ctx.Logger.Info("Failed to delete evaluation job", "error", err.Error(), "id", evaluationJobID)
			w.Error(err, ctx.RequestID)
			return
		}
	}
	w.WriteJSON(nil, 204)
}

func (h *Handlers) withSpan(ctx *executioncontext.ExecutionContext, fn otel.SpanFunction, component string, operation string, atts ...string) error {
	attributes := make(map[string]string)
	for i := 0; i < len(atts); i += 2 {
		if i+1 >= len(atts) {
			attributes[atts[i]] = ""
		} else {
			attributes[atts[i]] = atts[i+1]
		}
	}
	return otel.WithSpan(
		ctx.Ctx,
		h.serviceConfig,
		ctx.Logger,
		component,
		operation,
		attributes,
		fn,
	)
}
