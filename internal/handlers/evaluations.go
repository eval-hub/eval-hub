package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"
	"time"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/common"
	"github.com/eval-hub/eval-hub/internal/constants"
	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"github.com/eval-hub/eval-hub/internal/http_wrappers"
	"github.com/eval-hub/eval-hub/internal/logging"
	"github.com/eval-hub/eval-hub/internal/messages"
	"github.com/eval-hub/eval-hub/internal/mlflow"
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

// HandleCreateEvaluation handles POST /api/v1/evaluations/jobs
func (h *Handlers) HandleCreateEvaluation(ctx *executioncontext.ExecutionContext, req http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {
	storage := h.storage.WithLogger(ctx.Logger).WithContext(ctx.Ctx).WithTenant(ctx.Tenant).WithOwner(ctx.User)

	logging.LogRequestStarted(ctx)

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
			resolveProvider := func(providerID string) (*api.ProviderResource, error) {
				return common.ResolveProvider(providerID, h.providerConfigs, storage)
			}
			return h.validateBenchmarkReferences(evaluation, storage.GetCollection, resolveProvider)
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
		// Experiments must be scoped to the tenant namespace so job pods running
		// in that namespace can reach them with their own X-MLFLOW-WORKSPACE header.
		if !ctx.Tenant.IsEmpty() {
			client = client.WithWorkspace(ctx.Tenant.String())
		}

		mlflowExperimentID, mlflowExperimentURL, err = mlflow.GetExperimentID(client, evaluation, id)
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
						CreatedAt: time.Now(),
						Owner:     ctx.User,
						Tenant:    ctx.Tenant,
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
				runErr := h.executeEvaluationJob(runtimeCtx, ctx.Logger, h.runtime, job, storage)
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

func (h *Handlers) executeEvaluationJob(ctx context.Context, logger *slog.Logger, runtime abstractions.Runtime, job *api.EvaluationJobResource, storage abstractions.Storage) error {
	// Detach storage from the HTTP request context so that background
	// goroutines inside the runtime can update job status after the
	// request completes. This is the single transition point from
	// request-scoped work to background runtime work, covering all
	// runtime implementations (local, k8s, etc.).
	if storage != nil {
		detached := storage.WithContext(context.Background())
		storage = detached
	}

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

// ResolveProviderFunc resolves a provider by ID. Used by validateBenchmarkReferences so it does not depend on storage or context.
type ResolveProviderFunc func(providerID string) (*api.ProviderResource, error)

func (h *Handlers) validateBenchmarkReferences(evaluation *api.EvaluationJobConfig, getCollection common.GetCollectionFunc, resolveProvider ResolveProviderFunc) error {
	jobForResolve := &api.EvaluationJobResource{EvaluationJobConfig: *evaluation}
	benchmarks, err := common.GetJobBenchmarks(jobForResolve, getCollection)
	if err != nil {
		return err
	}
	for _, benchmark := range benchmarks {
		provider, err := resolveProvider(benchmark.ProviderID)
		if err != nil || provider == nil {
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
func (h *Handlers) HandleListEvaluations(ctx *executioncontext.ExecutionContext, req http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {
	storage := h.storage.WithLogger(ctx.Logger).WithContext(ctx.Ctx).WithTenant(ctx.Tenant).WithOwner(ctx.User)

	logging.LogRequestStarted(ctx)

	allowedParams := []string{"limit", "offset", "status", "name", "tags", "owner", "experiment_id"}
	badParams := getAllParams(req, allowedParams...)
	if len(badParams) > 0 {
		// just report the first bad parameter
		w.Error(serviceerrors.NewServiceError(messages.QueryBadParameter, "ParameterName", badParams[0], "AllowedParameters", strings.Join(allowedParams, ", ")), ctx.RequestID)
		return
	}

	filter, err := CommonListFilters(req)
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}

	status, err := GetParam(req, "status", true, "")
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}
	if status != "" {
		filter.Params["status"] = status
	}
	experimentID, err := GetParam(req, "experiment_id", true, "")
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}
	if experimentID != "" {
		filter.Params["experiment_id"] = experimentID
	}

	res, err := storage.GetEvaluationJobs(filter)
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}
	page, err := CreatePage(ctx, res.TotalCount, filter.Offset, filter.Limit, req)
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
	storage := h.storage.WithLogger(ctx.Logger).WithContext(ctx.Ctx).WithTenant(ctx.Tenant).WithOwner(ctx.User)

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
	storage := h.storage.WithLogger(ctx.Logger).WithContext(ctx.Ctx).WithTenant(ctx.Tenant).WithOwner(ctx.User)

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

	ctx.Logger.Info("Updating evaluation job", "id", evaluationJobID, "state", status.BenchmarkStatusEvent.Status, "status", status)

	err = storage.UpdateEvaluationJob(evaluationJobID, status)
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}

	w.WriteJSON(nil, 204)
}

// HandleCancelEvaluation handles DELETE /api/v1/evaluations/jobs/{id}
func (h *Handlers) HandleCancelEvaluation(ctx *executioncontext.ExecutionContext, r http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {
	storage := h.storage.WithLogger(ctx.Logger).WithContext(ctx.Ctx).WithTenant(ctx.Tenant).WithOwner(ctx.User)

	logging.LogRequestStarted(ctx)

	// Extract ID from path
	evaluationJobID := r.PathValue(constants.PATH_PARAMETER_JOB_ID)
	if evaluationJobID == "" {
		w.Error(serviceerrors.NewServiceError(messages.MissingPathParameter, "ParameterName", constants.PATH_PARAMETER_JOB_ID), ctx.RequestID)
		return
	}

	if h.runtime != nil {
		job, err := storage.GetEvaluationJob(evaluationJobID)
		if err != nil {
			w.Error(err, ctx.RequestID)
			return
		}
		if (job != nil) && (job.Status != nil) && (job.Status.State != api.OverallStateCancelled) {
			if err := h.runtime.WithLogger(ctx.Logger).WithContext(ctx.Ctx).DeleteEvaluationJobResources(job); err != nil {
				// Cleanup failures shouldn't block deleting the storage record.
				ctx.Logger.Error("Failed to delete evaluation runtime resources", "error", err, "id", evaluationJobID)
			}
		} else {
			if (job != nil) && (job.Status != nil) {
				ctx.Logger.Info(fmt.Sprintf("Evaluation job has has status %s so not deleting runtime resources", job.Status.State), "id", evaluationJobID)
			} else {
				ctx.Logger.Info("Evaluation job status not found so not deleting runtime resources", "id", evaluationJobID)
			}
		}
	}

	hardDelete, err := GetParam(r, "hard_delete", true, false)
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}

	if hardDelete {
		err = storage.DeleteEvaluationJob(evaluationJobID)
		if err != nil {
			ctx.Logger.Info("Failed to delete evaluation job", "error", err.Error(), "id", evaluationJobID)
			w.Error(err, ctx.RequestID)
			return
		}
	} else {
		err = storage.UpdateEvaluationJobStatus(evaluationJobID, api.OverallStateCancelled, &api.MessageInfo{
			Message:     "Evaluation job cancelled",
			MessageCode: constants.MESSAGE_CODE_EVALUATION_JOB_CANCELLED,
		})
		if err != nil {
			ctx.Logger.Info("Failed to cancel evaluation job", "error", err.Error(), "id", evaluationJobID)
			w.Error(err, ctx.RequestID)
			return
		}
	}
	w.WriteJSON(nil, 204)
}
