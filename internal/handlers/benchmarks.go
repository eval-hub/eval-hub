package handlers

import (
	"slices"

	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"github.com/eval-hub/eval-hub/internal/http_wrappers"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// HandleListBenchmarks handles GET /api/v1/evaluations/benchmarks
func (h *Handlers) HandleListBenchmarks(ctx *executioncontext.ExecutionContext, r http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {

	providerIdParam := r.Query("provider_id")
	benchmarkIdParam := r.Query("benchmark_id")
	categoryParam := r.Query("category")
	tags := r.Query("tags")

	providerId := ""
	benchmarkId := ""
	category := ""

	if len(providerIdParam) > 0 {
		providerId = providerIdParam[0]
	}
	if len(benchmarkIdParam) > 0 {
		benchmarkId = benchmarkIdParam[0]
	}
	if len(categoryParam) > 0 {
		category = categoryParam[0]
	}

	benchmarks := []api.BenchmarkResource{}
	for _, provider := range h.providerConfigs {
		for _, benchmark := range provider.Benchmarks {
			if providerId != "" && provider.ProviderID != providerId {
				continue
			}
			if benchmarkId != "" && benchmark.BenchmarkId != benchmarkId {
				continue
			}
			if category != "" && benchmark.Category != category {
				continue
			}

			contains := slices.ContainsFunc(tags, func(t string) bool {
				return slices.Contains(benchmark.Tags, t)
			})

			if len(tags) > 0 && !contains {
				continue
			}
			benchmark.ProviderId = &provider.ProviderID
			benchmarks = append(benchmarks, benchmark)
		}
	}

	w.WriteJSON(api.BenchmarkResourceList{
		TotalCount: len(benchmarks),
		Items:      benchmarks,
	}, 200)

}
