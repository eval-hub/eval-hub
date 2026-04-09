package validation

import (
	"testing"

	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/go-playground/validator/v10"
)

func TestNewValidator(t *testing.T) {
	validate := NewValidator()
	if validate == nil {
		t.Fatal("NewValidator() returned nil validator")
	}
}

func TestEvaluationJobConfigBenchmarksMin_WithCollection(t *testing.T) {
	validate := NewValidator()
	// When Collection is set with ID, empty Benchmarks is allowed
	cfg := api.EvaluationJobConfig{
		Name:       "test-evaluation-job",
		Model:      api.ModelRef{URL: "http://test.com", Name: "model"},
		Collection: &api.CollectionRef{ID: "coll-1"},
		Benchmarks: []api.EvaluationBenchmarkConfig{},
	}
	err := validate.Struct(cfg)
	if err != nil {
		t.Errorf("expected no error when Collection is set, got: %v", err)
	}
}

func TestEvaluationJobConfigBenchmarksMin_WithoutCollection_EmptyBenchmarks(t *testing.T) {
	validate := NewValidator()
	// When Collection is not set, Benchmarks must have at least 1 element
	cfg := api.EvaluationJobConfig{
		Name:       "test-evaluation-job",
		Model:      api.ModelRef{URL: "http://test.com", Name: "model"},
		Benchmarks: []api.EvaluationBenchmarkConfig{},
	}
	err := validate.Struct(cfg)
	if err == nil {
		t.Fatal("expected validation error when Benchmarks is empty and Collection not set")
	}
	valErr, ok := err.(validator.ValidationErrors)
	if !ok || len(valErr) == 0 {
		t.Fatalf("expected validator.ValidationErrors with at least one error, got %T: %v", err, err)
	}
	if valErr[0].Tag() != "minimum one benchmark" || valErr[0].Param() != "1" || valErr[0].Field() != "benchmarks" {
		t.Errorf("expected first error Tag=\"minimum one benchmark\" Param=1 Field=Benchmarks, got Tag=%q Param=%q Field=%q",
			valErr[0].Tag(), valErr[0].Param(), valErr[0].Field())
	}
}

func TestEvaluationJobConfigBenchmarksMin_WithoutCollection_WithBenchmark(t *testing.T) {
	validate := NewValidator()
	cfg := api.EvaluationJobConfig{
		Name:  "test-evaluation-job",
		Model: api.ModelRef{URL: "http://test.com", Name: "model"},
		Benchmarks: []api.EvaluationBenchmarkConfig{
			{Ref: api.Ref{ID: "b1"}, ProviderID: "provider-1"},
		},
	}
	err := validate.Struct(cfg)
	if err != nil {
		t.Errorf("expected no error when Benchmarks has 1+ elements, got: %v", err)
	}
}

func TestEvaluationJobConfig_ExperimentWithoutNameFails(t *testing.T) {
	validate := NewValidator()
	cfg := api.EvaluationJobConfig{
		Name:  "test-evaluation-job",
		Model: api.ModelRef{URL: "http://test.com", Name: "model"},
		Benchmarks: []api.EvaluationBenchmarkConfig{
			{Ref: api.Ref{ID: "b1"}, ProviderID: "provider-1"},
		},
		Experiment: &api.ExperimentConfig{},
	}
	err := validate.Struct(cfg)
	if err == nil {
		t.Fatal("expected validation error when experiment is set but name is omitted")
	}
	valErr, ok := err.(validator.ValidationErrors)
	if !ok || len(valErr) == 0 {
		t.Fatalf("expected validator.ValidationErrors, got %T: %v", err, err)
	}
	found := false
	for _, e := range valErr {
		if e.Field() == "name" && e.Tag() == "notblank" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected notblank error on experiment name, got: %v", err)
	}
}

func TestEvaluationJobConfig_ExperimentNameEmptyStringFails(t *testing.T) {
	validate := NewValidator()
	cfg := api.EvaluationJobConfig{
		Name:  "test-evaluation-job",
		Model: api.ModelRef{URL: "http://test.com", Name: "model"},
		Benchmarks: []api.EvaluationBenchmarkConfig{
			{Ref: api.Ref{ID: "b1"}, ProviderID: "provider-1"},
		},
		Experiment: &api.ExperimentConfig{Name: ""},
	}
	err := validate.Struct(cfg)
	if err == nil {
		t.Fatal("expected validation error when experiment name is present but empty")
	}
	valErr, ok := err.(validator.ValidationErrors)
	if !ok || len(valErr) == 0 {
		t.Fatalf("expected validator.ValidationErrors, got %T: %v", err, err)
	}
	found := false
	for _, e := range valErr {
		if e.Field() == "name" && e.Tag() == "notblank" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected notblank error on experiment name, got: %v", err)
	}
}

func TestEvaluationJobConfig_ExperimentNameWhitespaceOnlyFails(t *testing.T) {
	validate := NewValidator()
	ws := " \t "
	cfg := api.EvaluationJobConfig{
		Name:  "test-evaluation-job",
		Model: api.ModelRef{URL: "http://test.com", Name: "model"},
		Benchmarks: []api.EvaluationBenchmarkConfig{
			{Ref: api.Ref{ID: "b1"}, ProviderID: "provider-1"},
		},
		Experiment: &api.ExperimentConfig{Name: ws},
	}
	err := validate.Struct(cfg)
	if err == nil {
		t.Fatal("expected validation error when experiment name is only whitespace")
	}
	valErr, ok := err.(validator.ValidationErrors)
	if !ok || len(valErr) == 0 {
		t.Fatalf("expected validator.ValidationErrors, got %T: %v", err, err)
	}
	found := false
	for _, e := range valErr {
		if e.Field() == "name" && e.Tag() == "notblank" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected notblank error on experiment name, got: %v", err)
	}
}

func TestEvaluationJobConfig_ExperimentOmittedOk(t *testing.T) {
	validate := NewValidator()
	cfg := api.EvaluationJobConfig{
		Name:  "test-evaluation-job",
		Model: api.ModelRef{URL: "http://test.com", Name: "model"},
		Benchmarks: []api.EvaluationBenchmarkConfig{
			{Ref: api.Ref{ID: "b1"}, ProviderID: "provider-1"},
		},
		Experiment: nil,
	}
	err := validate.Struct(cfg)
	if err != nil {
		t.Errorf("expected no error when experiment is omitted, got: %v", err)
	}
}
