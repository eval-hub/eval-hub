package handlers

import (
	"maps"

	"strings"

	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"github.com/eval-hub/eval-hub/internal/http_wrappers"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// HandleListProviders handles GET /api/v1/evaluations/providers
func (h *Handlers) HandleListProviders(ctx *executioncontext.ExecutionContext, w http_wrappers.ResponseWrapper) {

	// Remove runtime configuration from the provider configs. This is internal information
	configs := make([]api.ProviderResource, 0, len(ctx.ProviderConfigs))
	for config := range maps.Values(ctx.ProviderConfigs) {
		config.Runtime = nil
		configs = append(configs, config)
	}

	list := api.ProviderResourceList{
		TotalCount: len(ctx.ProviderConfigs),
		Items:      configs,
	}

	w.WriteJSON(list, 200)

}

// HandleGetProvider handles GET /api/v1/evaluations/providers/{provider_id}
func (h *Handlers) HandleGetProvider(ctx *executioncontext.ExecutionContext, r http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {

	id := strings.TrimPrefix(r.Path(), "/api/v1/evaluations/providers/")

	p, found := ctx.ProviderConfigs[id]
	if !found {
		w.WriteJSON(map[string]interface{}{
			"message":             "Provider not found",
			"provider_id":         id,
			"supported_providers": maps.Keys(ctx.ProviderConfigs),
		}, 404)

		return
	}
	p.Runtime = nil
	w.WriteJSON(p, 200)

}
