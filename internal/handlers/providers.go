package handlers

import (
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// HandleListProviders handles GET /api/v1/evaluations/providers
func (h *Handlers) HandleListProviders(ctx *executioncontext.ExecutionContext, w http.ResponseWriter) {
	if !h.checkMethod(ctx, http.MethodGet, w) {
		return
	}

	list := api.ProviderResourceList{
		TotalCount: len(ctx.ProviderConfigs),
		Items:      slices.Collect(maps.Values(ctx.ProviderConfigs)),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

// HandleGetProvider handles GET /api/v1/evaluations/providers/{provider_id}
func (h *Handlers) HandleGetProvider(ctx *executioncontext.ExecutionContext, w http.ResponseWriter) {
	if !h.checkMethod(ctx, http.MethodGet, w) {
		return
	}

	id := strings.TrimPrefix(ctx.Request.Path(), "/api/v1/evaluations/providers/")

	p, found := ctx.ProviderConfigs[id]
	if !found {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message":     "Provider not found",
			"provider_id": id,
		})

		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(p)
}
