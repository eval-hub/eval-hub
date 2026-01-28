package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/eval-hub/eval-hub/internal/executioncontext"
)

// HandleListBenchmarks handles GET /api/v1/evaluations/benchmarks
func (h *Handlers) HandleListBenchmarks(ctx *executioncontext.ExecutionContext, w http.ResponseWriter) {
	if !h.checkMethod(ctx, http.MethodGet, w) {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"benchmarks":         []interface{}{},
		"total_count":        0,
		"providers_included": []string{},
	})
}
