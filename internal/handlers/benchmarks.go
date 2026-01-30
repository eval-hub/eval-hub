package handlers

import (
	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"github.com/eval-hub/eval-hub/internal/http_wrappers"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// HandleListBenchmarks handles GET /api/v1/evaluations/benchmarks
func (h *Handlers) HandleListBenchmarks(ctx *executioncontext.ExecutionContext, w http_wrappers.ResponseWrapper) {

	benchmarks := []api.BenchmarkResource{}
	for _, provider := range ctx.ProviderConfigs {
		for _, benchmark := range provider.Benchmarks {
			benchmark.ProviderId = &provider.ProviderID
			benchmarks = append(benchmarks, benchmark)
		}
	}

	w.WriteJSON(api.BenchmarkResourceList{
		TotalCount: len(benchmarks),
		Items:      benchmarks,
	}, 200)

}
