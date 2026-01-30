package handlers

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"github.com/eval-hub/eval-hub/internal/http_wrappers"
	"github.com/eval-hub/eval-hub/internal/logging"
	"github.com/eval-hub/eval-hub/internal/serialization"
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

func getEvaluationJobID(ctx *executioncontext.ExecutionContext) string {
	pathParts := strings.Split(ctx.Request.URI(), "/")
	id := pathParts[len(pathParts)-1]
	return id
}

func getParam[T string | int | bool](ctx *executioncontext.ExecutionContext, name string, optional bool, defaultValue T) (T, error) {
	values := ctx.Request.Query(name)
	if (len(values) == 0) || (values[0] == "") {
		if !optional {
			return defaultValue, fmt.Errorf("parameter '%s' is required", name)
		}
		return defaultValue, nil
	}
	switch any(defaultValue).(type) {
	case string:
		return any(values[0]).(T), nil
	case int:
		v, err := strconv.Atoi(values[0])
		if err != nil {
			return defaultValue, err
		}
		return any(v).(T), nil
	case bool:
		v, err := strconv.ParseBool(values[0])
		if err != nil {
			return defaultValue, err
		}
		return any(v).(T), nil
	default:
		// we should never get here
		return defaultValue, fmt.Errorf("unsupported type %T", defaultValue)
	}
}

// HandleCreateEvaluation handles POST /api/v1/evaluations/jobs
func (h *Handlers) HandleCreateEvaluation(ctx *executioncontext.ExecutionContext, w http_wrappers.ResponseWrapper) {
	logging.LogRequestStarted(ctx)

	// get the body bytes from the context
	bodyBytes, err := ctx.Request.BodyAsBytes()
	if err != nil {
		w.Error(err.Error(), 500, ctx.RequestID)
		return
	}
	evaluation := &api.EvaluationJobConfig{}
	err = serialization.Unmarshal(h.validate, ctx, bodyBytes, evaluation)
	if err != nil {
		w.Error(err.Error(), 400, ctx.RequestID)
		return
	}

	response, err := h.storage.CreateEvaluationJob(ctx, evaluation)
	if err != nil {
		w.Error(err.Error(), 500, ctx.RequestID)
		return
	}

	w.WriteJSON(response, 202)
}

// HandleListEvaluations handles GET /api/v1/evaluations/jobs
func (h *Handlers) HandleListEvaluations(ctx *executioncontext.ExecutionContext, w http_wrappers.ResponseWrapper) {
	logging.LogRequestStarted(ctx)

	limit, err := getParam(ctx, "limit", true, 50)
	if err != nil {
		w.Error(fmt.Errorf("failed to get limit query parameter as an integer: %w", err).Error(), 400, ctx.RequestID)
		return
	}
	offset, err := getParam(ctx, "offset", true, 0)
	if err != nil {
		w.Error(fmt.Errorf("failed to get offset query parameter as an integer: %w", err).Error(), 400, ctx.RequestID)
		return
	}
	statusFilter, err := getParam(ctx, "status_filter", true, "")
	if err != nil {
		w.Error(fmt.Errorf("failed to get status_filter query parameter: %w", err).Error(), 400, ctx.RequestID)
		return
	}
	response, err := h.storage.GetEvaluationJobs(ctx, limit, offset, statusFilter)
	if err != nil {
		w.Error(err.Error(), 500, ctx.RequestID)
		return
	}

	// set the first href to the current request URL
	response.Page.First = &api.HRef{Href: ctx.Request.URI()} // ctx.Request.URI() is the full request URL which should include the query parameters

	w.WriteJSON(response, 200)
}

// HandleGetEvaluation handles GET /api/v1/evaluations/jobs/{id}
func (h *Handlers) HandleGetEvaluation(ctx *executioncontext.ExecutionContext, w http_wrappers.ResponseWrapper) {
	logging.LogRequestStarted(ctx)

	// Extract ID from path
	evaluationJobID := getEvaluationJobID(ctx)

	response, err := h.storage.GetEvaluationJob(ctx, evaluationJobID)
	if err != nil {
		w.Error(err.Error(), 500, ctx.RequestID)
		return
	}

	w.WriteJSON(response, 200)
}

func (h *Handlers) HandleUpdateEvaluation(ctx *executioncontext.ExecutionContext, w http_wrappers.ResponseWrapper) {
	logging.LogRequestStarted(ctx)

	// Extract ID from path
	evaluationJobID := getEvaluationJobID(ctx)

	// get the body bytes from the context
	bodyBytes, err := ctx.Request.BodyAsBytes()
	if err != nil {
		w.Error(err.Error(), 500, ctx.RequestID)
		return
	}
	status := &api.EvaluationJobStatus{}
	err = serialization.Unmarshal(h.validate, ctx, bodyBytes, status)
	if err != nil {
		w.Error(err.Error(), 400, ctx.RequestID)
		return
	}

	err = h.storage.UpdateEvaluationJobStatus(ctx, evaluationJobID, status)
	if err != nil {
		w.Error(err.Error(), 500, ctx.RequestID)
		return
	}

	w.WriteJSON(nil, 204)
}

// HandleCancelEvaluation handles DELETE /api/v1/evaluations/jobs/{id}
func (h *Handlers) HandleCancelEvaluation(ctx *executioncontext.ExecutionContext, w http_wrappers.ResponseWrapper) {
	logging.LogRequestStarted(ctx)

	// Extract ID from path
	evaluationJobID := getEvaluationJobID(ctx)

	hardDelete, err := getParam(ctx, "hard_delete", false, false)
	if err != nil {
		w.Error(fmt.Errorf("failed to get hard_delete query parameter: %w", err).Error(), 400, ctx.RequestID)
		return
	}

	err = h.storage.DeleteEvaluationJob(ctx, evaluationJobID, hardDelete)
	if err != nil {
		w.Error(err.Error(), 500, ctx.RequestID)
		return
	}
	w.WriteJSON(nil, 204)
}
