package handlers

import (
	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"github.com/eval-hub/eval-hub/internal/http_wrappers"
	"github.com/eval-hub/eval-hub/internal/messages"
	"github.com/eval-hub/eval-hub/internal/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// HandleListProviders handles GET /api/v1/evaluations/providers
func (h *Handlers) HandleListProviders(ctx *executioncontext.ExecutionContext, r http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {

	providerIdParam := r.Query("id")
	benchmarksParam := r.Query("benchmarks")
	providerId := ""
	benchmarks := true

	if len(providerIdParam) > 0 {
		providerId = providerIdParam[0]
	}
	if len(benchmarksParam) > 0 {
		benchmarks = benchmarksParam[0] != "false"
	}

	providers := []api.ProviderResource{}
	foundProvider := false

	for _, p := range h.providerConfigs {
		if providerId != "" && p.ID != providerId {
			continue
		}
		if providerId != "" && p.ID == providerId {
			foundProvider = true
		}
		if !benchmarks {
			p.Benchmarks = []api.BenchmarkResource{}
		}
		providers = append(providers, p)
	}

	if providerId != "" && !foundProvider {
		w.Error(serviceerrors.NewServiceError(
			messages.QueryParameterInvalid,
			"ParameterName", "id",
			"Type", "provider id",
			"Value", providerId,
		), ctx.RequestID)
		return
	}

	w.WriteJSON(api.ProviderResourceList{
		TotalCount: len(providers),
		Items:      providers,
	}, 200)

}
