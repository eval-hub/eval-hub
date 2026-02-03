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
		TotalCount: len(h.providerConfigs),
		Items:      slices.Collect(maps.Values(h.providerConfigs)),
	}

	w.WriteJSON(list, 200)

}

// HandleGetProvider handles GET /api/v1/evaluations/providers/{provider_id}
func (h *Handlers) HandleGetProvider(ctx *executioncontext.ExecutionContext, r http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {

	id := strings.TrimPrefix(r.Path(), "/api/v1/evaluations/providers/")

	p, found := h.providerConfigs[id]
	if !found {
		w.WriteJSON(map[string]interface{}{
			"message":             "Provider not found",
			"provider_id":         id,
			"supported_providers": maps.Keys(h.providerConfigs),
		}, 404)

		return
	}

	w.WriteJSON(p, 200)

}
