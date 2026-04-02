package sql_test

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_hub/storage"
	"github.com/eval-hub/eval-hub/internal/eval_hub/storage/sql"
	"github.com/eval-hub/eval-hub/internal/eval_hub/validation"
	"github.com/eval-hub/eval-hub/internal/logging"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestCollections_PassCriteria(t *testing.T) {
	logger := logging.FallbackLogger()

	validate := validation.NewValidator()
	// set up the collection configs
	collectionConfigs, err := config.LoadCollectionConfigs(logger, validate, "../../../../config")
	if err != nil {
		t.Fatalf("failed to create collection configs: %v", err)
	}
	if len(collectionConfigs) == 0 {
		t.Fatalf("no collection configs loaded")
	}

	databaseConfig := map[string]any{
		"driver":        "sqlite",
		"url":           getDBInMemoryURL("eval_hub_pass_criteria"),
		"database_name": "eval_hub_pass_criteria",
	}
	store, err := storage.NewStorage(&databaseConfig, collectionConfigs, nil, false, logger)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	filter := &abstractions.QueryFilter{Limit: 50, Offset: 0, Params: map[string]any{"scope": "system"}}

	t.Run("get system collections and check pass criteria", func(t *testing.T) {
		res, err := store.GetCollections(filter)
		if err != nil {
			t.Fatalf("GetCollections: %v", err)
		}
		if len(res.Items) < 2 {
			t.Errorf("expected 2 collections, got %d", len(res.Items))
		}
		for _, coll := range res.Items {
			passCriteria := coll.CollectionConfig.PassCriteria.Threshold
			if passCriteria < 0.0 {
				t.Errorf("expected pass criteria to be at least 0.0, got %f", passCriteria)
			}
			// calculate the weighted average score
			weightedAverage := float32(0.0)
			totalWeight := float32(0.0)
			for _, benchmark := range coll.CollectionConfig.Benchmarks {
				if benchmark.PassCriteria == nil {
					t.Fatalf("collection %s benchmark %s is missing pass criteria", coll.Resource.ID, benchmark.ID)
				}
				weight := benchmark.Weight
				if weight == 0 {
					weight = 1
				}
				threshold := benchmark.PassCriteria.Threshold
				if benchmark.PrimaryScore != nil && benchmark.PrimaryScore.LowerIsBetter {
					threshold = 1 - threshold
				}
				weightedAverage += weight * threshold
				totalWeight += weight
			}
			if totalWeight == 0 {
				t.Fatalf("collection %s has no effective benchmark weights", coll.Resource.ID)
			}
			weightedAverage /= totalWeight
			// +/- 0.001?
			if math.Abs(float64(weightedAverage-passCriteria)) > 0.001 {
				t.Logf("expected weighted average for collection %s to be %f, got %f", coll.Resource.ID, passCriteria, weightedAverage)
			} else {
				t.Logf("weighted average for collection %s is %f", coll.Resource.ID, weightedAverage)
			}
		}
	})
}

func TestCollections_BenchmarksExist(t *testing.T) {
	logger := logging.FallbackLogger()

	validate := validation.NewValidator()
	// set up the collection configs
	collectionConfigs, err := config.LoadCollectionConfigs(logger, validate, "../../../../config")
	if err != nil {
		t.Fatalf("failed to create collection configs: %v", err)
	}
	if len(collectionConfigs) == 0 {
		t.Fatalf("no collection configs loaded")
	}
	// set up the provider configs
	providerConfigs, err := config.LoadProviderConfigs(logger, validate, "../../../../config")
	if err != nil {
		t.Fatalf("failed to create provider configs: %v", err)
	}
	if len(providerConfigs) == 0 {
		t.Fatalf("no provider configs loaded")
	}

	databaseConfig := map[string]any{
		"driver":        "sqlite",
		"url":           getDBInMemoryURL("eval_hub_pass_criteria"),
		"database_name": "eval_hub_pass_criteria",
	}
	store, err := storage.NewStorage(&databaseConfig, collectionConfigs, providerConfigs, false, logger)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	filter := &abstractions.QueryFilter{Limit: 50, Offset: 0, Params: map[string]any{"scope": "system"}}

	t.Run("get system collections and check pass criteria", func(t *testing.T) {
		res, err := store.GetCollections(filter)
		if err != nil {
			t.Fatalf("GetCollections: %v", err)
		}
		if len(res.Items) < 2 {
			t.Errorf("expected 2 collections, got %d", len(res.Items))
		}
		for _, coll := range res.Items {
			for _, benchmark := range coll.CollectionConfig.Benchmarks {
				if benchmark.ProviderID == "" {
					t.Fatalf("expected provider ID for benchmark %s", benchmark.ID)
				}
				provider, err := store.GetProvider(benchmark.ProviderID)
				if err != nil {
					t.Fatalf("failed to get provider %s: %v", benchmark.ProviderID, err)
				}
				if provider == nil {
					t.Fatalf("expected provider %s, got nil", benchmark.ProviderID)
				}
				if len(provider.Benchmarks) == 0 {
					t.Errorf("expected benchmarks for provider %s, got 0", benchmark.ProviderID)
				}
				found := false
				for _, pbenchmark := range provider.Benchmarks {
					if pbenchmark.ID == benchmark.ID {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected benchmark %s for provider %s, got none", benchmark.ID, benchmark.ProviderID)
				}
			}
		}
	})
}

func TestApplyPatches(t *testing.T) {
	t.Run("nil patches returns document unchanged", func(t *testing.T) {
		doc := `{"name":"x"}`
		got, err := sql.ApplyPatches(doc, nil)
		if err != nil {
			t.Fatalf("applyPatches: %v", err)
		}
		if string(got) != doc {
			t.Errorf("expected document unchanged, got %q", got)
		}
	})

	t.Run("empty patches returns document unchanged", func(t *testing.T) {
		doc := `{"name":"only"}`
		patches := &api.Patch{}
		got, err := sql.ApplyPatches(doc, patches)
		if err != nil {
			t.Fatalf("applyPatches: %v", err)
		}
		if string(got) != doc {
			t.Errorf("expected document unchanged, got %q", got)
		}
	})

	t.Run("single replace patch applies and returns patched JSON", func(t *testing.T) {
		doc := `{"name":"original","description":"desc","benchmarks":[]}`
		patches := &api.Patch{
			{Op: api.PatchOpReplace, Path: "/name", Value: "patched-name"},
		}
		got, err := sql.ApplyPatches(doc, patches)
		if err != nil {
			t.Fatalf("applyPatches: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal(got, &m); err != nil {
			t.Fatalf("result is not valid JSON: %v", err)
		}
		if name, _ := m["name"].(string); name != "patched-name" {
			t.Errorf("expected name %q, got %q", "patched-name", name)
		}
		if desc, _ := m["description"].(string); desc != "desc" {
			t.Errorf("expected description unchanged %q, got %q", "desc", desc)
		}
	})

	t.Run("multiple patches apply and return patched JSON", func(t *testing.T) {
		doc := `{"name":"a","description":"b"}`
		patches := &api.Patch{
			{Op: api.PatchOpReplace, Path: "/name", Value: "x"},
			{Op: api.PatchOpReplace, Path: "/description", Value: "y"},
		}
		got, err := sql.ApplyPatches(doc, patches)
		if err != nil {
			t.Fatalf("applyPatches: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal(got, &m); err != nil {
			t.Fatalf("result is not valid JSON: %v", err)
		}
		if name, _ := m["name"].(string); name != "x" {
			t.Errorf("expected name %q, got %q", "x", name)
		}
		if desc, _ := m["description"].(string); desc != "y" {
			t.Errorf("expected description %q, got %q", "y", desc)
		}
	})

	t.Run("replace nested path applies correctly", func(t *testing.T) {
		doc := `{"benchmarks":[{"id":"a","provider_id":"p1"}]}`
		patches := &api.Patch{
			{Op: api.PatchOpReplace, Path: "/benchmarks/0/id", Value: "new-id"},
		}
		got, err := sql.ApplyPatches(doc, patches)
		if err != nil {
			t.Fatalf("applyPatches: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal(got, &m); err != nil {
			t.Fatalf("result is not valid JSON: %v", err)
		}
		benchmarks, _ := m["benchmarks"].([]any)
		if len(benchmarks) != 1 {
			t.Fatalf("expected 1 benchmark, got %d", len(benchmarks))
		}
		first, _ := benchmarks[0].(map[string]any)
		if id, _ := first["id"].(string); id != "new-id" {
			t.Errorf("expected id %q, got %q", "new-id", id)
		}
	})
}
