package handlers

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_hub/constants"
	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/httpwrappers"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/internal/logging"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// HandleGetEvaluationJobLogs handles GET /api/v1/evaluations/jobs/{id}/logs
func (h *Handlers) HandleGetEvaluationJobLogs(ctx *executioncontext.ExecutionContext, req httpwrappers.RequestWrapper, w httpwrappers.ResponseWrapper) {
	h.handleGetEvaluationLogs(ctx, req, w, nil)
}

// HandleGetEvaluationBenchmarkLogs handles GET /api/v1/evaluations/jobs/{id}/benchmarks/{benchmark_index}/logs
func (h *Handlers) HandleGetEvaluationBenchmarkLogs(ctx *executioncontext.ExecutionContext, req httpwrappers.RequestWrapper, w httpwrappers.ResponseWrapper) {
	rawIndex := req.PathValue(constants.PathParameterBenchmarkIndex)
	if rawIndex == "" {
		w.Error(serviceerrors.NewServiceError(messages.MissingPathParameter, "ParameterName", constants.PathParameterBenchmarkIndex), ctx.RequestID)
		return
	}
	benchmarkIndex, err := strconv.Atoi(rawIndex)
	if err != nil || benchmarkIndex < 0 {
		w.Error(serviceerrors.NewServiceError(messages.QueryParameterInvalid, "ParameterName", constants.PathParameterBenchmarkIndex, "Type", "non-negative integer", "Value", rawIndex), ctx.RequestID)
		return
	}
	h.handleGetEvaluationLogs(ctx, req, w, &benchmarkIndex)
}

func (h *Handlers) handleGetEvaluationLogs(
	ctx *executioncontext.ExecutionContext,
	req httpwrappers.RequestWrapper,
	w httpwrappers.ResponseWrapper,
	benchmarkIndex *int,
) {
	storage := h.getStorage(ctx)
	logging.LogRequestStarted(ctx)

	evaluationJobID := req.PathValue(constants.PathParameterJobID)
	if evaluationJobID == "" {
		w.Error(serviceerrors.NewServiceError(messages.MissingPathParameter, "ParameterName", constants.PathParameterJobID), ctx.RequestID)
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

			benchmarks, err := h.resolveJobBenchmarks(storage.WithContext(runtimeCtx), job)
			if err != nil {
				w.Error(err, ctx.RequestID)
				return err
			}

			w.SetHeader("Content-Type", "text/plain; charset=utf-8")
			if ctx.RequestID != "" {
				w.SetHeader("X-Global-Transaction-Id", ctx.RequestID)
			}
			w.SetHeader("Trailer", "X-Log-Truncated")
			w.SetStatusCode(200)

			limit := config.DefaultMaxLogResponseBytes
			streamTimeout := config.DefaultLogStreamTimeout
			if h.serviceConfig != nil && h.serviceConfig.Service != nil {
				limit = h.serviceConfig.Service.EffectiveMaxLogResponseBytes()
				streamTimeout = h.serviceConfig.Service.EffectiveLogStreamTimeout()
			}
			lw := &LimitedWriter{W: responseWriterAdapter{w}, Limit: limit}

			streamCtx, cancel := context.WithTimeout(runtimeCtx, streamTimeout)
			defer cancel()

			err = h.runtime.WithLogger(ctx.Logger).WithContext(streamCtx).StreamEvaluationLogs(job, benchmarks, benchmarkIndex, logOpts, lw)
			if err != nil {
				if errors.Is(err, ErrLogResponseTruncated) {
					w.SetHeader("X-Log-Truncated", "true")
				} else {
					ctx.Logger.Error("error streaming evaluation logs", "error", err)
					return err
				}
			}

			logging.LogRequestSuccess(ctx, 200, nil)
			return nil
		},
		"runtime",
		"get-evaluation-job-logs",
		"job.id", evaluationJobID,
	)
}

func (h *Handlers) resolveJobBenchmarks(storage interface {
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

// responseWriterAdapter adapts httpwrappers.ResponseWrapper to io.Writer.
type responseWriterAdapter struct {
	w httpwrappers.ResponseWrapper
}

func (a responseWriterAdapter) Write(p []byte) (int, error) {
	return a.w.Write(p)
}

func parseEvaluationLogOptions(req httpwrappers.RequestWrapper) (api.EvaluationLogOptions, error) {
	tailLines, err := GetParam(req, "tail_lines", true, api.DefaultLogTailLines)
	if err != nil {
		return api.EvaluationLogOptions{}, err
	}
	if tailLines != api.AllLogLines && (tailLines < 1 || tailLines > api.MaxLogTailLines) {
		return api.EvaluationLogOptions{}, serviceerrors.NewServiceError(
			messages.QueryParameterInvalid,
			"ParameterName", "tail_lines",
			"Type", fmt.Sprintf("integer between 1 and %d, or -1 for all lines", api.MaxLogTailLines),
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
