package handlers

import (
	"context"
	"fmt"
	"strconv"

	"github.com/eval-hub/eval-hub/internal/eval_hub/constants"
	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/http_wrappers"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/internal/logging"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// HandleGetEvaluationJobLogs handles GET /api/v1/evaluations/jobs/{id}/logs
func (h *Handlers) HandleGetEvaluationJobLogs(ctx *executioncontext.ExecutionContext, req http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {
	h.handleGetEvaluationLogs(ctx, req, w, nil)
}

// HandleGetEvaluationBenchmarkLogs handles GET /api/v1/evaluations/jobs/{id}/benchmarks/{benchmark_index}/logs
func (h *Handlers) HandleGetEvaluationBenchmarkLogs(ctx *executioncontext.ExecutionContext, req http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {
	rawIndex := req.PathValue(constants.PATH_PARAMETER_BENCHMARK_INDEX)
	if rawIndex == "" {
		w.Error(serviceerrors.NewServiceError(messages.MissingPathParameter, "ParameterName", constants.PATH_PARAMETER_BENCHMARK_INDEX), ctx.RequestID)
		return
	}
	benchmarkIndex, err := strconv.Atoi(rawIndex)
	if err != nil || benchmarkIndex < 0 {
		w.Error(serviceerrors.NewServiceError(messages.QueryParameterInvalid, "ParameterName", constants.PATH_PARAMETER_BENCHMARK_INDEX, "Type", "non-negative integer", "Value", rawIndex), ctx.RequestID)
		return
	}
	h.handleGetEvaluationLogs(ctx, req, w, &benchmarkIndex)
}

func (h *Handlers) handleGetEvaluationLogs(
	ctx *executioncontext.ExecutionContext,
	req http_wrappers.RequestWrapper,
	w http_wrappers.ResponseWrapper,
	benchmarkIndex *int,
) {
	storage := h.getStorage(ctx)
	logging.LogRequestStarted(ctx)

	evaluationJobID := req.PathValue(constants.PATH_PARAMETER_JOB_ID)
	if evaluationJobID == "" {
		w.Error(serviceerrors.NewServiceError(messages.MissingPathParameter, "ParameterName", constants.PATH_PARAMETER_JOB_ID), ctx.RequestID)
		return
	}

	logOpts, err := parseEvaluationLogOptions(req)
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}

	if h.runtime == nil {
		w.Error(serviceerrors.NewServiceError(messages.InternalServerError, "Error", "no runtime configured"), ctx.RequestID)
		return
	}

	_ = h.withSpan(
		ctx,
		func(runtimeCtx context.Context) error {
			job, err := storage.WithContext(runtimeCtx).GetEvaluationJob(evaluationJobID)
			if err != nil {
				w.Error(err, ctx.RequestID)
				return err
			}

			benchmarks, err := h.resolveJobBenchmarks(ctx, storage.WithContext(runtimeCtx), job)
			if err != nil {
				w.Error(err, ctx.RequestID)
				return err
			}

			logs, err := h.runtime.WithLogger(ctx.Logger).WithContext(runtimeCtx).GetEvaluationLogs(job, benchmarks, benchmarkIndex, logOpts)
			if err != nil {
				w.Error(err, ctx.RequestID)
				return err
			}

			writePlainText(w, ctx, 200, logs)
			return nil
		},
		"runtime",
		"get-evaluation-job-logs",
		"job.id", evaluationJobID,
	)
}

func (h *Handlers) resolveJobBenchmarks(_ *executioncontext.ExecutionContext, storage interface {
	GetCollection(id string) (*api.CollectionResource, error)
}, job *api.EvaluationJobResource) ([]api.EvaluationBenchmarkConfig, error) {
	var collection *api.CollectionResource
	if job.Collection != nil && job.Collection.ID != "" {
		var err error
		collection, err = storage.GetCollection(job.Collection.ID)
		if err != nil {
			return nil, err
		}
	}
	return GetJobBenchmarks(job, collection)
}

func parseEvaluationLogOptions(req http_wrappers.RequestWrapper) (api.EvaluationLogOptions, error) {
	tailLines, err := GetParam(req, "tail_lines", true, api.DefaultLogTailLines)
	if err != nil {
		return api.EvaluationLogOptions{}, err
	}
	if tailLines < 1 || tailLines > api.MaxLogTailLines {
		return api.EvaluationLogOptions{}, serviceerrors.NewServiceError(
			messages.QueryParameterInvalid,
			"ParameterName", "tail_lines",
			"Type", fmt.Sprintf("integer between 1 and %d", api.MaxLogTailLines),
			"Value", strconv.Itoa(tailLines),
		)
	}

	timestamps, err := GetParam(req, "timestamps", true, false)
	if err != nil {
		return api.EvaluationLogOptions{}, err
	}

	opts := api.EvaluationLogOptions{
		TailLines:  tailLines,
		Timestamps: timestamps,
	}

	rawSince := req.Query("since_seconds")
	if len(rawSince) > 0 {
		sinceSeconds, err := GetParam(req, "since_seconds", false, 0)
		if err != nil {
			return api.EvaluationLogOptions{}, err
		}
		if sinceSeconds < 1 {
			return api.EvaluationLogOptions{}, serviceerrors.NewServiceError(
				messages.QueryParameterInvalid,
				"ParameterName", "since_seconds",
				"Type", "positive integer",
				"Value", strconv.Itoa(sinceSeconds),
			)
		}
		opts.SinceSeconds = &sinceSeconds
	}

	return opts, nil
}

func writePlainText(w http_wrappers.ResponseWrapper, ctx *executioncontext.ExecutionContext, code int, body string) {
	w.SetHeader("Content-Type", "text/plain; charset=utf-8")
	if ctx.RequestID != "" {
		w.SetHeader("X-Global-Transaction-Id", ctx.RequestID)
	}
	w.SetStatusCode(code)
	if body != "" {
		_, _ = w.Write([]byte(body))
	}
	logging.LogRequestSuccess(ctx, code, nil)
}
