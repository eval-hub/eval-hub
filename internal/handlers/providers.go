package handlers

import (
	"maps"

	"slices"
	"strings"

	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"github.com/eval-hub/eval-hub/internal/http_wrappers"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// HandleListProviders handles GET /api/v1/evaluations/providers
func (h *Handlers) HandleListProviders(ctx *executioncontext.ExecutionContext, w http_wrappers.ResponseWrapper) {

	list := api.ProviderResourceList{
		TotalCount: len(ctx.ProviderConfigs),
		Items:      slices.Collect(maps.Values(ctx.ProviderConfigs)),
	}

	w.WriteJSON(list, 200)

}

// HandleGetProvider handles GET /api/v1/evaluations/providers/{provider_id}
func (h *Handlers) HandleGetProvider(ctx *executioncontext.ExecutionContext, w http_wrappers.ResponseWrapper) {

	id := strings.TrimPrefix(ctx.Request.Path(), "/api/v1/evaluations/providers/")

	p, found := ctx.ProviderConfigs[id]
	if !found {
		w.WriteJSON(map[string]interface{}{
			"message":             "Provider not found",
			"provider_id":         id,
			"supported_providers": maps.Keys(ctx.ProviderConfigs),
		}, 404)

		return
	}

	w.WriteJSON(p, 200)

}
