package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/eval-hub/eval-hub/internal/executioncontext"
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
func (h *Handlers) HandleCreateEvaluation(ctx *executioncontext.ExecutionContext, w http.ResponseWriter) {
	if !h.checkMethod(ctx, http.MethodPost, w) {
		return
	}
	// get the body bytes from the context
	bodyBytes, err := ctx.Request.BodyAsBytes()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	evaluation := &api.EvaluationJobConfig{}
	err = serialization.Unmarshal(h.validate, ctx, bodyBytes, evaluation)
	if err != nil {
		h.serializationError(ctx, w, err, http.StatusBadRequest)
		return
	}

	response, err := h.storage.CreateEvaluationJob(ctx, evaluation)
	if err != nil {
		h.errorResponse(ctx, w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.successResponse(ctx, w, response, http.StatusAccepted)
}

// HandleListEvaluations handles GET /api/v1/evaluations/jobs
func (h *Handlers) HandleListEvaluations(ctx *executioncontext.ExecutionContext, w http.ResponseWriter) {
	if !h.checkMethod(ctx, http.MethodGet, w) {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"items":       []interface{}{},
		"total_count": 0,
		"limit":       50,
		"first":       map[string]string{"href": ""},
		"next":        nil,
	})
}

// HandleGetEvaluation handles GET /api/v1/evaluations/jobs/{id}
func (h *Handlers) HandleGetEvaluation(ctx *executioncontext.ExecutionContext, w http.ResponseWriter) {
	if !h.checkMethod(ctx, http.MethodGet, w) {
		return
	}

	// Extract ID from path
	pathParts := strings.Split(ctx.Request.URI(), "/")
	id := pathParts[len(pathParts)-1]

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Evaluation retrieval not yet implemented",
		"id":      id,
	})
}

// HandleCancelEvaluation handles DELETE /api/v1/evaluations/jobs/{id}
func (h *Handlers) HandleCancelEvaluation(ctx *executioncontext.ExecutionContext, w http.ResponseWriter) {
	if !h.checkMethod(ctx, http.MethodDelete, w) {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Evaluation cancellation not yet implemented",
	})
}

// HandleGetEvaluationSummary handles GET /api/v1/evaluations/jobs/{id}/summary
func (h *Handlers) HandleGetEvaluationSummary(ctx *executioncontext.ExecutionContext, w http.ResponseWriter) {
	if !h.checkMethod(ctx, http.MethodGet, w) {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Evaluation summary not yet implemented",
	})
}
