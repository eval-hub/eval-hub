package validation

import (
	"testing"

	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/go-playground/validator/v10"
)

func TestNewValidator(t *testing.T) {
	validate, err := NewValidator()
	if err != nil {
		t.Fatalf("NewValidator() error: %v", err)
	}
	if validate == nil {
		t.Fatal("NewValidator() returned nil validator")
	}
}

func TestEvaluationJobConfigBenchmarksMin_WithCollection(t *testing.T) {
	validate, err := NewValidator()
	if err != nil {
		t.Fatalf("NewValidator() error: %v", err)
	}
	// When Collection is set with ID, empty Benchmarks is allowed
	cfg := api.EvaluationJobConfig{
		Model:      api.ModelRef{URL: "http://test.com", Name: "model"},
		Collection: &api.Ref{ID: "coll-1"},
		Benchmarks: []api.BenchmarkConfig{},
	}
	err = validate.Struct(cfg)
	if err != nil {
		t.Errorf("expected no error when Collection is set, got: %v", err)
	}
}

func TestEvaluationJobConfigBenchmarksMin_WithoutCollection_EmptyBenchmarks(t *testing.T) {
	validate, err := NewValidator()
	if err != nil {
		t.Fatalf("NewValidator() error: %v", err)
	}
	// When Collection is not set, Benchmarks must have at least 1 element
	cfg := api.EvaluationJobConfig{
		Model:      api.ModelRef{URL: "http://test.com", Name: "model"},
		Benchmarks: []api.BenchmarkConfig{},
	}
	err = validate.Struct(cfg)
	if err == nil {
		t.Fatal("expected validation error when Benchmarks is empty and Collection not set")
	}
	if valErr, ok := err.(validator.ValidationErrors); ok && len(valErr) > 0 {
		if valErr[0].Tag() != "min=1" && valErr[0].Field() != "Benchmarks" {
			t.Logf("got validation error: %v (acceptable)", err)
		}
	}
}

func TestEvaluationJobConfigBenchmarksMin_WithoutCollection_WithBenchmark(t *testing.T) {
	validate, err := NewValidator()
	if err != nil {
		t.Fatalf("NewValidator() error: %v", err)
	}
	cfg := api.EvaluationJobConfig{
		Model: api.ModelRef{URL: "http://test.com", Name: "model"},
		Benchmarks: []api.BenchmarkConfig{
			{Ref: api.Ref{ID: "b1"}, ProviderID: "provider-1"},
		},
	}
	err = validate.Struct(cfg)
	if err != nil {
		t.Errorf("expected no error when Benchmarks has 1+ elements, got: %v", err)
	}
}
