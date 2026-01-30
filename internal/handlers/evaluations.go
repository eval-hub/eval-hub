package handlers

import (
	"strings"

	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"github.com/eval-hub/eval-hub/internal/http_wrappers"
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

// HandleCreateEvaluation handles POST /api/v1/evaluations/jobs
func (h *Handlers) HandleCreateEvaluation(ctx *executioncontext.ExecutionContext, w http_wrappers.ResponseWrapper) {

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

	w.Error("Not implemented", 501, ctx.RequestID)

}

// HandleGetEvaluation handles GET /api/v1/evaluations/jobs/{id}
func (h *Handlers) HandleGetEvaluation(ctx *executioncontext.ExecutionContext, w http_wrappers.ResponseWrapper) {

	// Extract ID from path
	//pathParts := strings.Split(ctx.Request.URI(), "/")
	//id := pathParts[len(pathParts)-1]

	w.Error("Not implemented", 501, ctx.RequestID)
}

// HandleCancelEvaluation handles DELETE /api/v1/evaluations/jobs/{id}
func (h *Handlers) HandleCancelEvaluation(ctx *executioncontext.ExecutionContext, w http_wrappers.ResponseWrapper) {

	// Extract ID from path
	pathParts := strings.Split(ctx.Request.URI(), "/")
	id := pathParts[len(pathParts)-1]

	err := h.storage.DeleteEvaluationJob(ctx, id, true)
	if err != nil {
		w.Error(err.Error(), 500, ctx.RequestID)
		return
	}
	w.WriteJSON(nil, 204)
}

// HandleGetEvaluationSummary handles GET /api/v1/evaluations/jobs/{id}/summary
func (h *Handlers) HandleGetEvaluationSummary(ctx *executioncontext.ExecutionContext, w http_wrappers.ResponseWrapper) {

	w.Error("Not implemented", 501, ctx.RequestID)
}
