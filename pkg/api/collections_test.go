package api_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/testhelpers"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestCollectionsValidation(t *testing.T) {
	srcs := []string{
		`
		{
        	"name": "test-collection-1",
        	"category": "test",
        	"description": "Collection of benchmarks for FVT",
        	"pass_criteria": {
            	"threshold": 0
        	},
        	"benchmarks": [
            	{
                	"id": "arc_easy",
                	"provider_id": "lm_evaluation_harness",
                	"primary_score": {
						"metric": "acc_norm",
						"lower_is_better": false
					},
					"pass_criteria": {
                    	"threshold": 0.5
                	},
                	"parameters": {
                    	"limit": 10,
                    	"num_fewshot": 0,
                    	"tokenizer": "google/flan-t5-small"
                	}
            	}
        	]
    	}
		`,
	}

	for _, src := range srcs {
		var config api.CollectionConfig
		err := json.Unmarshal([]byte(src), &config)
		if err != nil {
			t.Fatalf("failed to unmarshal collection config: %v", err)
		}
		validator := testhelpers.NewValidator(t)
		err = validator.Struct(config)
		if err != nil {
			t.Fatalf("failed to validate collection config: %v", err)
		}
	}
}

func TestCollectionConfigNewFieldsSerialization(t *testing.T) {
	src := `{
		"name": "rag-eval",
		"category": "document_understanding",
		"curation_order": 1,
		"domains": ["grounded_document_understanding"],
		"tasks": ["rag", "grounding_discipline"],
		"modalities": ["text"],
		"industries": ["health", "financial"],
		"evaluation_targets": ["model"],
		"benchmarks": [{"id": "crag", "provider_id": "ragas"}]
	}`

	var config api.CollectionConfig
	if err := json.Unmarshal([]byte(src), &config); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if config.CurationOrder != 1 {
		t.Errorf("CurationOrder: got %d, want 1", config.CurationOrder)
	}
	if !reflect.DeepEqual(config.Domains, []string{"grounded_document_understanding"}) {
		t.Errorf("Domains: got %v, want [grounded_document_understanding]", config.Domains)
	}
	if !reflect.DeepEqual(config.Tasks, []string{"rag", "grounding_discipline"}) {
		t.Errorf("Tasks: got %v", config.Tasks)
	}
	if !reflect.DeepEqual(config.Modalities, []string{"text"}) {
		t.Errorf("Modalities: got %v", config.Modalities)
	}
	if !reflect.DeepEqual(config.Industries, []string{"health", "financial"}) {
		t.Errorf("Industries: got %v", config.Industries)
	}
	if !reflect.DeepEqual(config.EvaluationTargets, []string{"model"}) {
		t.Errorf("EvaluationTargets: got %v", config.EvaluationTargets)
	}

	// Round-trip
	out, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var rt api.CollectionConfig
	if err := json.Unmarshal(out, &rt); err != nil {
		t.Fatalf("round-trip unmarshal failed: %v", err)
	}
	if rt.CurationOrder != config.CurationOrder {
		t.Errorf("round-trip CurationOrder: got %d, want %d", rt.CurationOrder, config.CurationOrder)
	}
}

func TestCollectionConfigNewFieldsAreOptional(t *testing.T) {
	// Existing minimal payload must still validate — new fields are optional
	src := `{
		"name": "minimal",
		"category": "reasoning",
		"benchmarks": [{"id": "arc_easy", "provider_id": "lm_evaluation_harness"}]
	}`

	var config api.CollectionConfig
	if err := json.Unmarshal([]byte(src), &config); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	validator := testhelpers.NewValidator(t)
	if err := validator.Struct(config); err != nil {
		t.Fatalf("validation failed: %v", err)
	}

	if config.CurationOrder != 0 {
		t.Errorf("CurationOrder should default to 0, got %d", config.CurationOrder)
	}
	if config.Domains != nil {
		t.Errorf("Domains should be nil, got %v", config.Domains)
	}
	if config.Tasks != nil {
		t.Errorf("Tasks should be nil, got %v", config.Tasks)
	}
	if config.Modalities != nil {
		t.Errorf("Modalities should be nil, got %v", config.Modalities)
	}
}

func TestCollectionStatusFieldsSerialization(t *testing.T) {
	resource := api.CollectionResource{
		Resource:    api.Resource{ID: "abc123"},
		DerivedFrom: "source-collection-id",
		CollectionConfig: api.CollectionConfig{
			Name:     "my-collection",
			Category: "software",
			Benchmarks: []api.CollectionBenchmarkConfig{
				{Ref: api.Ref{ID: "lcb"}, ProviderID: "lighteval"},
			},
		},
		PinnedOrder: 2,
		Status: &api.CollectionStatus{
			RunCount: 5,
		},
	}

	out, err := json.Marshal(resource)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var rt api.CollectionResource
	if err := json.Unmarshal(out, &rt); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if rt.DerivedFrom != "source-collection-id" {
		t.Errorf("DerivedFrom: got %q, want %q", rt.DerivedFrom, "source-collection-id")
	}
	if rt.Status == nil {
		t.Fatal("State should not be nil after round-trip")
	}
	if rt.Status.RunCount != 5 {
		t.Errorf("RunCount: got %d, want 5", rt.Status.RunCount)
	}
	if rt.PinnedOrder != 2 {
		t.Errorf("PinnedOrder: got %d, want 2", rt.PinnedOrder)
	}
}

func TestCollectionStatusAbsentForSystemCollections(t *testing.T) {
	resource := api.CollectionResource{
		Resource: api.Resource{ID: "sys-col"},
		CollectionConfig: api.CollectionConfig{
			Name:     "rag-eval-v1",
			Category: "document_understanding",
			Benchmarks: []api.CollectionBenchmarkConfig{
				{Ref: api.Ref{ID: "crag"}, ProviderID: "ragas"},
			},
		},
	}

	out, err := json.Marshal(resource)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	if strings.Contains(string(out), `"status"`) {
		t.Error("state must be absent from system collection JSON when nil")
	}
}

func TestCollectionDerivedFromOmittedWhenEmpty(t *testing.T) {
	resource := api.CollectionResource{
		Resource: api.Resource{ID: "r1"},
		CollectionConfig: api.CollectionConfig{
			Name: "n", Category: "c",
			Benchmarks: []api.CollectionBenchmarkConfig{{Ref: api.Ref{ID: "b"}, ProviderID: "p"}},
		},
	}
	out, err := json.Marshal(resource)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if strings.Contains(string(out), `"derived_from"`) {
		t.Error("derived_from must be omitted when empty")
	}
}

func TestApplyOverrides(t *testing.T) {
	t.Parallel()
	base := api.CollectionConfig{
		Name: "Base", Category: "base", CurationOrder: 5,
		Benchmarks: []api.CollectionBenchmarkConfig{{Ref: api.Ref{ID: "b-base"}, ProviderID: "p1"}},
	}

	t.Run("nil overrides returns base unchanged", func(t *testing.T) {
		t.Parallel()
		result := base.ApplyOverrides(nil)
		if result.Name != "Base" {
			t.Errorf("expected Base, got %q", result.Name)
		}
		if result.CurationOrder != 5 {
			t.Errorf("expected CurationOrder=5 (nil override preserves), got %d", result.CurationOrder)
		}
	})

	t.Run("empty overrides reset CurationOrder to 0", func(t *testing.T) {
		t.Parallel()
		result := base.ApplyOverrides(&api.CollectionConfig{})
		if result.CurationOrder != 0 {
			t.Errorf("expected CurationOrder=0, got %d", result.CurationOrder)
		}
	})

	t.Run("name override applied", func(t *testing.T) {
		t.Parallel()
		result := base.ApplyOverrides(&api.CollectionConfig{Name: "Override"})
		if result.Name != "Override" {
			t.Errorf("expected Override, got %q", result.Name)
		}
	})

	t.Run("benchmarks override applied", func(t *testing.T) {
		t.Parallel()
		newBench := []api.CollectionBenchmarkConfig{{Ref: api.Ref{ID: "new-b"}, ProviderID: "p2"}}
		result := base.ApplyOverrides(&api.CollectionConfig{Benchmarks: newBench})
		if len(result.Benchmarks) != 1 || result.Benchmarks[0].ID != "new-b" {
			t.Errorf("expected override benchmarks, got %v", result.Benchmarks)
		}
	})

	t.Run("tags override applied", func(t *testing.T) {
		t.Parallel()
		result := base.ApplyOverrides(&api.CollectionConfig{Tags: []string{"t1", "t2"}})
		if len(result.Tags) != 2 {
			t.Errorf("expected 2 tags, got %v", result.Tags)
		}
	})

	t.Run("custom override applied", func(t *testing.T) {
		t.Parallel()
		data := map[string]any{"k": "v"}
		result := base.ApplyOverrides(&api.CollectionConfig{Custom: &data})
		if result.Custom == nil {
			t.Error("expected custom to be set")
		}
	})

	t.Run("description override applied", func(t *testing.T) {
		t.Parallel()
		result := base.ApplyOverrides(&api.CollectionConfig{Description: "new desc"})
		if result.Description != "new desc" {
			t.Errorf("expected 'new desc', got %q", result.Description)
		}
	})

	t.Run("category override applied", func(t *testing.T) {
		t.Parallel()
		result := base.ApplyOverrides(&api.CollectionConfig{Category: "new-cat"})
		if result.Category != "new-cat" {
			t.Errorf("expected 'new-cat', got %q", result.Category)
		}
	})

	t.Run("pass criteria override applied", func(t *testing.T) {
		t.Parallel()
		threshold := float32(0.9)
		pc := &api.PassCriteria{Threshold: &threshold}
		result := base.ApplyOverrides(&api.CollectionConfig{PassCriteria: pc})
		if result.PassCriteria == nil || *result.PassCriteria.Threshold != 0.9 {
			t.Errorf("expected PassCriteria.Threshold=0.9, got %+v", result.PassCriteria)
		}
	})

	t.Run("agent override applied", func(t *testing.T) {
		t.Parallel()
		agent := &api.CollectionAgentMetadata{Summary: "my-agent"}
		result := base.ApplyOverrides(&api.CollectionConfig{Agent: agent})
		if result.Agent == nil || result.Agent.Summary != "my-agent" {
			t.Errorf("expected Agent.Summary='my-agent', got %+v", result.Agent)
		}
	})

	t.Run("domains/tasks/modalities/industries/evaluation_targets override applied", func(t *testing.T) {
		t.Parallel()
		result := base.ApplyOverrides(&api.CollectionConfig{
			Domains:           []string{"d1"},
			Tasks:             []string{"t1"},
			Modalities:        []string{"text"},
			Industries:        []string{"health"},
			EvaluationTargets: []string{"model"},
		})
		if len(result.Domains) == 0 || result.Domains[0] != "d1" {
			t.Errorf("Domains not applied: %v", result.Domains)
		}
		if len(result.Tasks) == 0 || result.Tasks[0] != "t1" {
			t.Errorf("Tasks not applied: %v", result.Tasks)
		}
	})
}

func TestToEvaluationBenchmark(t *testing.T) {
	t.Parallel()
	b := api.CollectionBenchmarkConfig{
		Ref:          api.Ref{ID: "b1"},
		ProviderID:   "p1",
		URL:          "https://example.com/b1",
		Weight:       0.8,
		PrimaryScore: &api.PrimaryScore{Metric: "acc"},
		PassCriteria: &api.PassCriteria{},
		Parameters:   map[string]any{"limit": 10},
	}
	eb := b.ToEvaluationBenchmark()
	if eb.ID != "b1" {
		t.Errorf("ID: got %q, want %q", eb.ID, "b1")
	}
	// URL is stripped by ToEvaluationBenchmark (not in EvaluationBenchmarkConfig)
	_ = eb // verify compiles
	if false {
		t.Error("unreachable")
	}
	if eb.ProviderID != "p1" {
		t.Errorf("ProviderID: got %q", eb.ProviderID)
	}
	if eb.Weight != 0.8 {
		t.Errorf("Weight: got %f", eb.Weight)
	}
}
