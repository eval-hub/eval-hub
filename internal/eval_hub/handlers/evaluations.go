package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/common"
	"github.com/eval-hub/eval-hub/internal/eval_hub/constants"
	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/http_wrappers"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/mlflow"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serialization"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/internal/logging"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/go-playground/validator/v10"
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

// runtimeStorage wraps storage for RunEvaluationJob. Instances passed to a runtime use ctx (a detached job context)
// for all GetProvider/UpdateEvaluationJob calls so work is not tied to the HTTP request deadline or cancellation.
type runtimeStorage struct {
	ctx      context.Context
	logger   *slog.Logger
	handlers *Handlers
	tenant   api.Tenant
	owner    api.User
	validate *validator.Validate
}

// scopedStorage matches getStorage scoping (tenant/owner/logger) with the runtime job context.
func (s *runtimeStorage) scopedStorage() abstractions.Storage {
	return s.handlers.storage.WithLogger(s.logger).WithContext(s.ctx).WithTenant(s.tenant).WithOwner(s.owner)
}

func (s *runtimeStorage) GetProvider(id string) (*api.ProviderResource, error) {
	provider, err := s.scopedStorage().GetProvider(id)
	if err != nil {
		s.logger.Info("Failed to get provider from storage", "provider_id", id, "error", err)
		return nil, err
	}
	return provider, nil
}

func (s *runtimeStorage) UpdateEvaluationJob(id string, runStatus *api.StatusEvent) error {
	err := s.validate.Struct(runStatus)
	if err != nil {
		s.logger.Info("Failed to validate evaluation job status from the runtime", "job_id", id, "error", err)
		return err
	}
	err = s.scopedStorage().UpdateEvaluationJob(id, runStatus)
	if err != nil {
		s.logger.Info("Failed to update evaluation job in storage", "job_id", id, "error", err)
		return err
	}
	return nil
}

func (h *Handlers) getStorage(ctx *executioncontext.ExecutionContext) abstractions.Storage {
	return h.storage.WithLogger(ctx.Logger).WithContext(ctx.Ctx).WithTenant(ctx.Tenant).WithOwner(ctx.User)
}

// HandleCreateEvaluation handles POST /api/v1/evaluations/jobs
func (h *Handlers) HandleCreateEvaluation(ctx *executioncontext.ExecutionContext, req http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {
	storage := h.getStorage(ctx)

	logging.LogRequestStarted(ctx)

	id := common.GUID()

	evaluation := &api.EvaluationJobConfig{}
	var collection *api.CollectionResource

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
			if evaluation.Collection != nil && evaluation.Collection.ID != "" {
				collection, err = storage.WithContext(runtimeCtx).GetCollection(evaluation.Collection.ID)
				if err != nil {
					return err
				}
			}
			jobForResolve := &api.EvaluationJobResource{EvaluationJobConfig: *evaluation}
			benchmarks, err := GetJobBenchmarks(jobForResolve, collection)
			if err != nil {
				return err
			}
			return h.validateBenchmarkReferences(ctx, benchmarks)
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
		err = h.withSpan(
			ctx,
			func(runtimeCtx context.Context) error {
				client := h.mlflowClient.WithContext(runtimeCtx).WithLogger(ctx.Logger)
				// Experiments must be scoped to the tenant namespace so job pods running
				// in that namespace can reach them with their own X-MLFLOW-WORKSPACE header.
				if !ctx.Tenant.IsEmpty() {
					client = client.WithWorkspace(ctx.Tenant.String())
				}
				mlflowExperimentID, mlflowExperimentURL, err = mlflow.GetOrCreateExperimentID(client, evaluation, id)
				return err
			},
			"mlflow",
			"get-or-create-experiment",
			"job.id", id,
		)
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
		func(runtimeCtx context.Context) error {
			if h.runtime != nil {
				runErr := h.executeEvaluationJob(ctx.WithContext(runtimeCtx), job, collection)
				if runErr != nil {
					state := api.OverallStateFailed
					message := &api.MessageInfo{
						Message:     runErr.Error(),
						MessageCode: constants.MESSAGE_CODE_EVALUATION_JOB_FAILED,
					}
					if err := storage.WithContext(runtimeCtx).UpdateEvaluationJobStatus(job.Resource.ID, state, message); err != nil {
						ctx.Logger.Error("Failed to update evaluation status", "error", err, "job_id", job.Resource.ID)
					}
					// return the first error encountered
					w.Error(runErr, ctx.RequestID)
					return runErr
				}
			} else {
				message := &api.MessageInfo{
					Message:     "Evaluation job created but no runtime configured",
					MessageCode: constants.MESSAGE_CODE_EVALUATION_JOB_UPDATED,
				}
				if err := storage.WithContext(runtimeCtx).UpdateEvaluationJobStatus(job.Resource.ID, job.Status.State, message); err != nil {
					ctx.Logger.Error("Failed to update evaluation status", "error", err, "job_id", job.Resource.ID)
				}
				job.Status.Message = message
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

func (h *Handlers) createRuntimeStorage(ctx *executioncontext.ExecutionContext, jobContext context.Context) *runtimeStorage {
	return &runtimeStorage{
		ctx:      jobContext,
		logger:   ctx.Logger,
		handlers: h,
		tenant:   ctx.Tenant,
		owner:    ctx.User,
		validate: h.validate,
	}
}

func (h *Handlers) executeEvaluationJob(ctx *executioncontext.ExecutionContext, job *api.EvaluationJobResource, collection *api.CollectionResource) error {
	var err error

	benchmarks, err := GetJobBenchmarks(job, collection)
	if err != nil {
		return err
	}

	// Detach storage from the HTTP request context so that background
	// goroutines inside the runtime can update job status after the
	// request completes. This is the single transition point from
	// request-scoped work to background runtime work, covering all
	// runtime implementations (local, k8s, etc.).
	jobContext := context.Background()

	return h.runtime.WithLogger(ctx.Logger).WithContext(jobContext).RunEvaluationJob(job, benchmarks, h.createRuntimeStorage(ctx, jobContext))
}

func (h *Handlers) validateBenchmarkReferences(ctx *executioncontext.ExecutionContext, benchmarks []api.EvaluationBenchmarkConfig) error {
	storage := h.getStorage(ctx)

	for _, benchmark := range benchmarks {
		provider, err := storage.GetProvider(benchmark.ProviderID)
		if err != nil {
			ctx.Logger.Error("Failed to get provider whilst validating benchmark", "benchmark_id", benchmark.ID, "provider_id", benchmark.ProviderID, "error", err)
			return err
		}
		if provider == nil {
			ctx.Logger.Debug("Provider not found whilst validating benchmark", "benchmark_id", benchmark.ID, "provider_id", benchmark.ProviderID)
			return serviceerrors.NewServiceError(
				messages.ResourceDoesNotExist,
				"Type", "provider",
				"ResourceID", benchmark.ProviderID,
			)
		}
		if !slices.ContainsFunc(provider.Benchmarks, func(b api.BenchmarkResource) bool { return b.ID == benchmark.ID }) {
			ctx.Logger.Debug("Benchmark does not exist in provider", "benchmark_id", benchmark.ID, "provider_id", benchmark.ProviderID)
			return serviceerrors.NewServiceError(
				messages.ResourceDoesNotExist,
				"Type", "benchmark",
				"ResourceID", benchmark.ID,
			)
		}
	}
	return nil
}

// HandleListEvaluations handles GET /api/v1/evaluations/jobs
func (h *Handlers) HandleListEvaluations(ctx *executioncontext.ExecutionContext, req http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {
	storage := h.getStorage(ctx)

	var ofilter *abstractions.QueryFilter

	err := h.withSpan(
		ctx,
		func(runtimeCtx context.Context) error {
			filter, err := CommonListFilters(req)
			if err != nil {
				return err
			}

			logging.LogRequestStarted(ctx, "filter", filter)

			allowedParams := []string{"limit", "offset", "status", "name", "tags", "owner", "experiment_id"}
			badParams := getAllParams(req, allowedParams...)
			if len(badParams) > 0 {
				// just report the first bad parameter
				return serviceerrors.NewServiceError(messages.QueryBadParameter, "ParameterName", badParams[0], "AllowedParameters", strings.Join(allowedParams, ", "))
			}

			status, err := GetParam(req, "status", true, "")
			if err != nil {
				return err
			}
			if status != "" {
				filter.Params["status"] = status
			}
			experimentID, err := GetParam(req, "experiment_id", true, "")
			if err != nil {
				return err
			}
			if experimentID != "" {
				filter.Params["experiment_id"] = experimentID
			}

			ofilter = filter
			return nil
		},
		"validation",
		"validate-evaluation-jobs-filter",
	)
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}

	_ = h.withSpan(
		ctx,
		func(runtimeCtx context.Context) error {
			res, err := storage.WithContext(runtimeCtx).GetEvaluationJobs(ofilter)
			if err != nil {
				w.Error(err, ctx.RequestID)
				return err
			}
			page, err := CreatePage(ctx, res.TotalCount, ofilter.Offset, ofilter.Limit, req)
			if err != nil {
				w.Error(err, ctx.RequestID)
				return err
			}
			result := api.EvaluationJobResourceList{
				Page:   *page,
				Items:  res.Items,
				Errors: res.Errors,
			}
			w.WriteJSON(result, 200)
			return nil
		},
		"storage",
		"list-evaluation-jobs",
	)
}

// HandleGetEvaluation handles GET /api/v1/evaluations/jobs/{id}
func (h *Handlers) HandleGetEvaluation(ctx *executioncontext.ExecutionContext, r http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {
	storage := h.getStorage(ctx)

	logging.LogRequestStarted(ctx)

	// Extract ID from path
	evaluationJobID := r.PathValue(constants.PATH_PARAMETER_JOB_ID)
	if evaluationJobID == "" {
		w.Error(serviceerrors.NewServiceError(messages.MissingPathParameter, "ParameterName", constants.PATH_PARAMETER_JOB_ID), ctx.RequestID)
		return
	}

	_ = h.withSpan(
		ctx,
		func(runtimeCtx context.Context) error {
			response, err := storage.WithContext(runtimeCtx).GetEvaluationJob(evaluationJobID)
			if err != nil {
				w.Error(err, ctx.RequestID)
				return err
			}
			w.WriteJSON(response, 200)
			return nil
		},
		"storage",
		"get-evaluation-job",
		"job.id", evaluationJobID,
	)
}

func (h *Handlers) HandleUpdateEvaluation(ctx *executioncontext.ExecutionContext, r http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {
	storage := h.getStorage(ctx)

	logging.LogRequestStarted(ctx)

	// Extract ID from path
	evaluationJobID := r.PathValue(constants.PATH_PARAMETER_JOB_ID)
	if evaluationJobID == "" {
		w.Error(serviceerrors.NewServiceError(messages.MissingPathParameter, "ParameterName", constants.PATH_PARAMETER_JOB_ID), ctx.RequestID)
		return
	}

	var status = &api.StatusEvent{}

	err := h.withSpan(
		ctx,
		func(runtimeCtx context.Context) error {
			// get the body bytes from the context
			bodyBytes, err := r.BodyAsBytes()
			if err != nil {
				return err
			}
			return serialization.Unmarshal(h.validate, ctx.WithContext(runtimeCtx), bodyBytes, status)
		},
		"validation",
		"validate-evaluation-job",
		"job.id", evaluationJobID,
	)
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}

	ctx.Logger.Debug("Updating evaluation job", "id", evaluationJobID, "state", status.BenchmarkStatusEvent.Status, "status", status)

	_ = h.withSpan(
		ctx,
		func(runtimeCtx context.Context) error {
			err = storage.WithContext(runtimeCtx).UpdateEvaluationJob(evaluationJobID, status)
			if err != nil {
				w.Error(err, ctx.RequestID)
				return err
			}
			w.WriteJSON(nil, 204)
			return nil
		},
		"storage",
		"update-evaluation-job",
		"job.id", evaluationJobID,
	)
}

// HandleCancelEvaluation handles DELETE /api/v1/evaluations/jobs/{id}
func (h *Handlers) HandleCancelEvaluation(ctx *executioncontext.ExecutionContext, r http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {
	storage := h.getStorage(ctx)

	logging.LogRequestStarted(ctx)

	// Extract ID from path
	evaluationJobID := r.PathValue(constants.PATH_PARAMETER_JOB_ID)
	if evaluationJobID == "" {
		w.Error(serviceerrors.NewServiceError(messages.MissingPathParameter, "ParameterName", constants.PATH_PARAMETER_JOB_ID), ctx.RequestID)
		return
	}

	err := h.withSpan(
		ctx,
		func(runtimeCtx context.Context) error {
			if h.runtime != nil {
				job, err := storage.WithContext(runtimeCtx).GetEvaluationJob(evaluationJobID)
				if err != nil {
					return err
				}
				if (job != nil) && (job.Status != nil) && (job.Status.State != api.OverallStateCancelled) {
					if err := h.runtime.WithLogger(ctx.Logger).WithContext(runtimeCtx).DeleteEvaluationJobResources(job); err != nil {
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
			return nil
		},
		"runtime",
		"delete-evaluation-job-resources",
		"job.id", evaluationJobID,
	)
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}

	operation := "cancel-evaluation-job"
	hardDelete, err := GetParam(r, "hard_delete", true, false)
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}
	if hardDelete {
		operation = "delete-evaluation-job"
	}

	_ = h.withSpan(
		ctx,
		func(runtimeCtx context.Context) error {
			if hardDelete {
				err = storage.WithContext(runtimeCtx).DeleteEvaluationJob(evaluationJobID)
				if err != nil {
					ctx.Logger.Info("Failed to delete evaluation job", "error", err.Error(), "id", evaluationJobID)
					w.Error(err, ctx.RequestID)
					return err
				}
			} else {
				err = storage.WithContext(runtimeCtx).UpdateEvaluationJobStatus(evaluationJobID, api.OverallStateCancelled, &api.MessageInfo{
					Message:     "Evaluation job cancelled",
					MessageCode: constants.MESSAGE_CODE_EVALUATION_JOB_CANCELLED,
				})
				if err != nil {
					ctx.Logger.Info("Failed to cancel evaluation job", "error", err.Error(), "id", evaluationJobID)
					w.Error(err, ctx.RequestID)
					return err
				}
			}
			w.WriteJSON(nil, 204)
			return nil
		},
		"storage",
		operation,
		"job.id", evaluationJobID,
	)
}
