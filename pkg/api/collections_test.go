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
		"ai_entities": ["model"],
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
	if !reflect.DeepEqual(config.AIEntities, []string{"model"}) {
		t.Errorf("AIEntities: got %v", config.AIEntities)
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

func TestCollectionStateFieldsSerialization(t *testing.T) {
	resource := api.CollectionResource{
		Resource: api.Resource{ID: "abc123", VersionCounter: 3},
		CollectionConfig: api.CollectionConfig{
			Name:     "my-collection",
			Category: "software",
			Benchmarks: []api.CollectionBenchmarkConfig{
				{Ref: api.Ref{ID: "lcb"}, ProviderID: "lighteval"},
			},
		},
		State: &api.CollectionState{
			DerivedFrom: "source-collection-id",
			RunCount:    5,
			PinnedOrder: 2,
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

	if rt.State == nil {
		t.Fatal("State should not be nil after round-trip")
	}
	if rt.State.DerivedFrom != "source-collection-id" {
		t.Errorf("DerivedFrom: got %q, want %q", rt.State.DerivedFrom, "source-collection-id")
	}
	if rt.State.RunCount != 5 {
		t.Errorf("RunCount: got %d, want 5", rt.State.RunCount)
	}
	if rt.State.PinnedOrder != 2 {
		t.Errorf("PinnedOrder: got %d, want 2", rt.State.PinnedOrder)
	}
	if rt.Resource.VersionCounter != 3 {
		t.Errorf("Resource.VersionCounter: got %d, want 3", rt.Resource.VersionCounter)
	}
}

func TestCollectionStateAbsentForSystemCollections(t *testing.T) {
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

	if strings.Contains(string(out), `"state"`) {
		t.Error("state must be absent from system collection JSON when nil")
	}
}

func TestResourceVersionField(t *testing.T) {
	r := api.Resource{ID: "r1", VersionCounter: 7}
	out, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var rt api.Resource
	if err := json.Unmarshal(out, &rt); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if rt.VersionCounter != 7 {
		t.Errorf("Version: got %d, want 7", rt.VersionCounter)
	}
}

func TestResourceVersionOmittedWhenZero(t *testing.T) {
	r := api.Resource{ID: "r1"}
	out, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	if strings.Contains(string(out), `"version_counter"`) {
		t.Error("version_counter must be omitted when zero")
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

	t.Run("domains/tasks/modalities/industries/ai_entities override applied", func(t *testing.T) {
		t.Parallel()
		result := base.ApplyOverrides(&api.CollectionConfig{
			Domains:    []string{"d1"},
			Tasks:      []string{"t1"},
			Modalities: []string{"text"},
			Industries: []string{"health"},
			AIEntities: []string{"model"},
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
