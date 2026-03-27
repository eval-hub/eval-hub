package handlers

import (
	"errors"
	"reflect"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestMergeBenchmarkParameters(t *testing.T) {
	t.Parallel()

	t.Run("no job overrides copies collection parameters", func(t *testing.T) {
		t.Parallel()
		benchmark := api.CollectionBenchmarkConfig{
			Ref:        api.Ref{ID: "bench-1"},
			ProviderID: "prov-a",
			Weight:     0.5,
			Parameters: map[string]any{"k1": "v1", "k2": float64(2)},
		}
		got := mergeBenchmarkParameters(benchmark, nil)
		if !reflect.DeepEqual(got.Parameters, benchmark.Parameters) {
			t.Fatalf("Parameters = %#v, want %#v", got.Parameters, benchmark.Parameters)
		}
		if got.Ref != benchmark.Ref || got.ProviderID != benchmark.ProviderID || got.Weight != benchmark.Weight {
			t.Fatalf("metadata changed: got %+v, want %+v", got, benchmark)
		}
	})

	t.Run("job matching provider adds parameters not in collection", func(t *testing.T) {
		t.Parallel()
		benchmark := api.CollectionBenchmarkConfig{
			Ref:        api.Ref{ID: "bench-1"},
			ProviderID: "prov-a",
			Parameters: map[string]any{"from_collection": "x"},
		}
		job := []api.EvaluationBenchmarkConfig{{
			ProviderID: "prov-a",
			Parameters: map[string]any{"from_job": "y"},
		}}
		got := mergeBenchmarkParameters(benchmark, job)
		want := map[string]any{"from_collection": "x", "from_job": "y"}
		if !reflect.DeepEqual(got.Parameters, want) {
			t.Fatalf("Parameters = %#v, want %#v", got.Parameters, want)
		}
	})

	t.Run("non-empty collection value overrides job for same key", func(t *testing.T) {
		t.Parallel()
		benchmark := api.CollectionBenchmarkConfig{
			ProviderID: "prov-a",
			Parameters: map[string]any{"k": "from_collection"},
		}
		job := []api.EvaluationBenchmarkConfig{{
			ProviderID: "prov-a",
			Parameters: map[string]any{"k": "from_job"},
		}}
		got := mergeBenchmarkParameters(benchmark, job)
		if got.Parameters["k"] != "from_collection" {
			t.Fatalf("k = %v, want from_collection", got.Parameters["k"])
		}
	})

	t.Run("empty string in collection removes key including job-only keys", func(t *testing.T) {
		t.Parallel()
		benchmark := api.CollectionBenchmarkConfig{
			ProviderID: "prov-a",
			Parameters: map[string]any{"k": ""},
		}
		job := []api.EvaluationBenchmarkConfig{{
			ProviderID: "prov-a",
			Parameters: map[string]any{"k": "job_val", "other": "keep"},
		}}
		got := mergeBenchmarkParameters(benchmark, job)
		want := map[string]any{"other": "keep"}
		if !reflect.DeepEqual(got.Parameters, want) {
			t.Fatalf("Parameters = %#v, want %#v", got.Parameters, want)
		}
	})

	t.Run("nil parameter value in collection removes key", func(t *testing.T) {
		t.Parallel()
		benchmark := api.CollectionBenchmarkConfig{
			ProviderID: "prov-a",
			Parameters: map[string]any{"k": nil},
		}
		job := []api.EvaluationBenchmarkConfig{{
			ProviderID: "prov-a",
			Parameters: map[string]any{"k": "job_val"},
		}}
		got := mergeBenchmarkParameters(benchmark, job)
		if _, ok := got.Parameters["k"]; ok {
			t.Fatalf("expected k removed, got %#v", got.Parameters)
		}
	})

	t.Run("job entries with different provider are ignored", func(t *testing.T) {
		t.Parallel()
		benchmark := api.CollectionBenchmarkConfig{
			ProviderID: "prov-other",
			Parameters: map[string]any{"a": 1},
		}
		job := []api.EvaluationBenchmarkConfig{{
			ProviderID: "prov-a",
			Parameters: map[string]any{"noise": true},
		}}
		got := mergeBenchmarkParameters(benchmark, job)
		if !reflect.DeepEqual(got.Parameters, benchmark.Parameters) {
			t.Fatalf("Parameters = %#v, want %#v", got.Parameters, benchmark.Parameters)
		}
	})

	t.Run("multiple job blocks same provider accumulate then collection overlays", func(t *testing.T) {
		t.Parallel()
		benchmark := api.CollectionBenchmarkConfig{
			ProviderID: "prov-a",
			Parameters: map[string]any{"third": "from_collection", "dup": "collection_wins"},
		}
		job := []api.EvaluationBenchmarkConfig{
			{ProviderID: "prov-a", Parameters: map[string]any{"first": 1, "dup": "first"}},
			{ProviderID: "prov-a", Parameters: map[string]any{"second": 2, "dup": "second"}},
		}
		got := mergeBenchmarkParameters(benchmark, job)
		want := map[string]any{
			"first":  1,
			"second": 2,
			"third":  "from_collection",
			"dup":    "collection_wins",
		}
		if !reflect.DeepEqual(got.Parameters, want) {
			t.Fatalf("Parameters = %#v, want %#v", got.Parameters, want)
		}
	})
}

func TestGetJobBenchmarks(t *testing.T) {
	t.Parallel()

	jobID := "job-1"
	makeJob := func() *api.EvaluationJobResource {
		return &api.EvaluationJobResource{
			Resource: api.EvaluationResource{Resource: api.Resource{ID: jobID}},
		}
	}

	t.Run("collection set but storage nil returns internal error", func(t *testing.T) {
		t.Parallel()
		job := makeJob()
		job.Collection = &api.CollectionRef{ID: "col-1"}
		job.Benchmarks = []api.EvaluationBenchmarkConfig{{ProviderID: "p", Ref: api.Ref{ID: "b"}}}
		_, err := GetJobBenchmarks(job, nil)
		var se *serviceerrors.ServiceError
		if !errors.As(err, &se) || se.MessageCode() != messages.InternalServerError {
			t.Fatalf("err = %v, want InternalServerError service error", err)
		}
	})

	t.Run("collection with no benchmarks returns collection_empty", func(t *testing.T) {
		t.Parallel()
		job := makeJob()
		job.Collection = &api.CollectionRef{ID: "col-1"}
		collection := &api.CollectionResource{
			Resource: api.Resource{ID: "col-1"},
			CollectionConfig: api.CollectionConfig{
				Benchmarks: []api.CollectionBenchmarkConfig{},
			},
		}
		_, err := GetJobBenchmarks(job, collection)
		var se *serviceerrors.ServiceError
		if !errors.As(err, &se) || se.MessageCode() != messages.CollectionEmpty {
			t.Fatalf("err = %v, want CollectionEmpty", err)
		}
	})

	t.Run("no collection and no job benchmarks returns evaluation_job_empty", func(t *testing.T) {
		t.Parallel()
		job := makeJob()
		job.Benchmarks = nil
		_, err := GetJobBenchmarks(job, nil)
		var se *serviceerrors.ServiceError
		if !errors.As(err, &se) || se.MessageCode() != messages.EvaluationJobEmpty {
			t.Fatalf("err = %v, want EvaluationJobEmpty", err)
		}
	})

	t.Run("no collection returns job benchmarks unchanged", func(t *testing.T) {
		t.Parallel()
		job := makeJob()
		want := []api.EvaluationBenchmarkConfig{{ProviderID: "p", Ref: api.Ref{ID: "b1"}}}
		job.Benchmarks = want
		got, err := GetJobBenchmarks(job, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	})

	t.Run("collection id set but job collection ref id empty uses job benchmarks", func(t *testing.T) {
		t.Parallel()
		job := makeJob()
		job.Collection = &api.CollectionRef{ID: ""}
		want := []api.EvaluationBenchmarkConfig{{ProviderID: "p", Ref: api.Ref{ID: "only-job"}}}
		job.Benchmarks = want
		got, err := GetJobBenchmarks(job, &api.CollectionResource{})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	})

	t.Run("collection resolves and merges job parameters per benchmark", func(t *testing.T) {
		t.Parallel()
		job := makeJob()
		job.Collection = &api.CollectionRef{ID: "col-1"}
		job.Benchmarks = []api.EvaluationBenchmarkConfig{
			{
				ProviderID: "prov-a-ref",
				Parameters: map[string]any{"shared": "from_job_a", "only_a": 1},
			},
			{
				ProviderID: "prov-b-ref",
				Parameters: map[string]any{"shared": "from_job_b"},
			},
		}
		collection := &api.CollectionResource{
			Resource: api.Resource{ID: "col-1"},
			CollectionConfig: api.CollectionConfig{
				Benchmarks: []api.CollectionBenchmarkConfig{
					{
						Ref:        api.Ref{ID: "a-ref"},
						ProviderID: "prov-a-ref",
						Weight:     1,
						Parameters: map[string]any{"shared": "from_collection_a", "base": "x"},
					},
					{
						Ref:        api.Ref{ID: "b-ref"},
						ProviderID: "prov-b-ref",
						Parameters: map[string]any{"base_b": "y"},
					},
				},
			},
		}
		got, err := GetJobBenchmarks(job, collection)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 {
			t.Fatalf("len = %d, want 2", len(got))
		}
		want0 := map[string]any{"shared": "from_collection_a", "only_a": 1, "base": "x"}
		if !reflect.DeepEqual(got[0].Parameters, want0) {
			t.Fatalf("first benchmark parameters = %#v, want %#v", got[0].Parameters, want0)
		}
		want1 := map[string]any{"shared": "from_job_b", "base_b": "y"}
		if !reflect.DeepEqual(got[1].Parameters, want1) {
			t.Fatalf("second benchmark parameters = %#v, want %#v", got[1].Parameters, want1)
		}
		if got[0].Ref.ID != "a-ref" || got[1].Ref.ID != "b-ref" {
			t.Fatalf("Refs = %+v, %+v", got[0].Ref, got[1].Ref)
		}
	})
}
