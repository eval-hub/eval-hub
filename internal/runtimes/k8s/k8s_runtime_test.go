package k8s

import (
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestResolveProviderFromEvaluation(t *testing.T) {
	providers := map[string]api.ProviderResource{
		"provider-1": {
			ProviderID: "provider-1",
			Benchmarks: []api.BenchmarkResource{
				{BenchmarkId: "bench-1"},
			},
		},
	}
	evaluation := &api.EvaluationJobResource{
		EvaluationJobConfig: api.EvaluationJobConfig{
			Benchmarks: []api.BenchmarkConfig{
				{Ref: api.Ref{ID: "bench-1"}},
			},
		},
	}

	provider, benchmarkID, err := resolveProviderFromEvaluation(providers, evaluation)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider.ProviderID != "provider-1" {
		t.Fatalf("expected provider provider-1, got %s", provider.ProviderID)
	}
	if benchmarkID != "bench-1" {
		t.Fatalf("expected benchmark bench-1, got %s", benchmarkID)
	}
}

func TestResolveProviderFromEvaluationMissingBenchmarks(t *testing.T) {
	providers := map[string]api.ProviderResource{
		"provider-1": {
			ProviderID: "provider-1",
		},
	}
	evaluation := &api.EvaluationJobResource{}

	_, _, err := resolveProviderFromEvaluation(providers, evaluation)
	if err == nil {
		t.Fatalf("expected error for missing benchmarks")
	}
}

func TestResolveProviderFromEvaluationMultipleBenchmarks(t *testing.T) {
	providers := map[string]api.ProviderResource{
		"provider-1": {
			ProviderID: "provider-1",
			Benchmarks: []api.BenchmarkResource{
				{BenchmarkId: "bench-1"},
			},
		},
	}
	evaluation := &api.EvaluationJobResource{
		EvaluationJobConfig: api.EvaluationJobConfig{
			Benchmarks: []api.BenchmarkConfig{
				{Ref: api.Ref{ID: "bench-1"}},
				{Ref: api.Ref{ID: "bench-2"}},
			},
		},
	}

	_, _, err := resolveProviderFromEvaluation(providers, evaluation)
	if err == nil {
		t.Fatalf("expected error for multiple benchmarks")
	}
	if !strings.Contains(err.Error(), "multi-benchmark evaluations are not supported") {
		t.Fatalf("unexpected error: %v", err)
	}
}
