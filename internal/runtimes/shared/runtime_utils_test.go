package shared

import (
	"testing"

	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestResolveProvider_FromMap(t *testing.T) {
	providers := map[string]api.ProviderResource{
		"p1": {Resource: api.Resource{ID: "p1"}},
	}
	got, err := ResolveProvider("p1", providers, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got == nil || got.Resource.ID != "p1" {
		t.Fatalf("expected provider p1, got %v", got)
	}
}

func TestResolveProvider_NotFound(t *testing.T) {
	providers := map[string]api.ProviderResource{}
	got, err := ResolveProvider("missing", providers, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got != nil {
		t.Fatalf("expected nil provider, got %v", got)
	}
	if err.Error() != `provider "missing" not found` {
		t.Fatalf("expected 'provider \"missing\" not found', got %q", err.Error())
	}
}

func TestResolveBenchmarks_FromJobBenchmarks(t *testing.T) {
	eval := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-1"}},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Benchmarks: []api.BenchmarkConfig{
				{Ref: api.Ref{ID: "b1"}, ProviderID: "p1"},
			},
		},
	}
	got, err := ResolveBenchmarks(eval, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(got) != 1 || got[0].ID != "b1" {
		t.Fatalf("expected one benchmark b1, got %v", got)
	}
}

func TestResolveBenchmarks_CollectionSetStorageNil_ReturnsError(t *testing.T) {
	eval := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-1"}},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Collection: &api.Ref{ID: "coll-1"},
		},
	}
	_, err := ResolveBenchmarks(eval, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "collection is set but storage is not available for job job-1" {
		t.Fatalf("expected collection/storage error, got %q", err.Error())
	}
}
