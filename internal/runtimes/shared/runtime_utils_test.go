package shared

import (
	"testing"

	"github.com/eval-hub/eval-hub/pkg/api"
)

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
