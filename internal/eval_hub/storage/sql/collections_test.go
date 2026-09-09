package sql_test

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_hub/storage"
	"github.com/eval-hub/eval-hub/internal/eval_hub/storage/sql"
	"github.com/eval-hub/eval-hub/internal/logging"
	"github.com/eval-hub/eval-hub/internal/testhelpers"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestCollections_PassCriteria(t *testing.T) {
	logger := logging.FallbackLogger()

	validate := testhelpers.NewValidator(t)
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
	store, err := storage.NewStorage(&databaseConfig, collectionConfigs, nil, false, false, logger)
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
			if coll.PassCriteria == nil || coll.PassCriteria.Threshold == nil {
				t.Fatalf("collection %s is missing pass criteria", coll.Resource.ID)
			}
			passCriteria := *coll.PassCriteria.Threshold
			if passCriteria < 0.0 {
				t.Errorf("expected pass criteria to be at least 0.0, got %f", passCriteria)
			}
			// calculate the weighted average score
			weightedAverage := float32(0.0)
			totalWeight := float32(0.0)
			for _, benchmark := range coll.Benchmarks {
				if benchmark.PassCriteria == nil || benchmark.PassCriteria.Threshold == nil {
					t.Fatalf("collection %s benchmark %s is missing pass criteria", coll.Resource.ID, benchmark.ID)
				}
				weight := benchmark.Weight
				if weight == 0 {
					weight = 1
				}
				threshold := *benchmark.PassCriteria.Threshold
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

	validate := testhelpers.NewValidator(t)
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
	store, err := storage.NewStorage(&databaseConfig, collectionConfigs, providerConfigs, false, false, logger)
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
			for _, benchmark := range coll.Benchmarks {
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

func TestCollectionState_SetAndIncrement(t *testing.T) {
	for _, driver := range []string{"sqlite"} {
		driver := driver
		t.Run(driver, func(t *testing.T) {
			t.Parallel()
			store, err := getTestStorage(t, driver, getDBName())
			if err != nil {
				t.Fatalf("getTestStorage: %v", err)
			}

			coll := &api.CollectionResource{
				Resource: api.Resource{ID: "coll-state-test", Owner: "user1", Tenant: "t1"},
				CollectionConfig: api.CollectionConfig{
					Name:     "State Test",
					Category: "test",
					Benchmarks: []api.CollectionBenchmarkConfig{
						{Ref: api.Ref{ID: "b1"}, ProviderID: "p1"},
					},
				},
			}
			if err := store.WithTenant("t1").WithOwner("user1").CreateCollection(coll); err != nil {
				t.Fatalf("CreateCollection: %v", err)
			}

			scoped := store.WithTenant("t1").WithOwner("user1")

			// UpdateCollectionState
			state := &api.CollectionState{DerivedFrom: "original-id", RunCount: 3, PinnedOrder: 2}
			updated, err := scoped.UpdateCollectionState("coll-state-test", state)
			if err != nil {
				t.Fatalf("UpdateCollectionState: %v", err)
			}
			if updated.State == nil {
				t.Fatal("expected State to be set")
			}
			if updated.State.DerivedFrom != "original-id" {
				t.Errorf("DerivedFrom: got %q, want %q", updated.State.DerivedFrom, "original-id")
			}
			if updated.State.RunCount != 3 {
				t.Errorf("RunCount: got %d, want 3", updated.State.RunCount)
			}

			// VersionCounter increments on each UpdateCollection
			config := coll.CollectionConfig
			v1, err := scoped.UpdateCollection("coll-state-test", &config)
			if err != nil {
				t.Fatalf("UpdateCollection (first): %v", err)
			}
			if v1.Resource.VersionCounter != 1 {
				t.Errorf("VersionCounter after first update: got %d, want 1", v1.Resource.VersionCounter)
			}

			v2, err := scoped.UpdateCollection("coll-state-test", &config)
			if err != nil {
				t.Fatalf("UpdateCollection (second): %v", err)
			}
			if v2.Resource.VersionCounter != 2 {
				t.Errorf("VersionCounter after second update: got %d, want 2", v2.Resource.VersionCounter)
			}

			// State should be preserved through UpdateCollection
			if v2.State == nil || v2.State.DerivedFrom != "original-id" {
				t.Error("State should be preserved through UpdateCollection")
			}
		})
	}
}

func TestCollectionState_SystemCollectionVersionCounterZero(t *testing.T) {
	t.Parallel()
	store, err := getTestStorage(t, "sqlite", getDBName())
	if err != nil {
		t.Fatalf("getTestStorage: %v", err)
	}

	sysColl := &api.CollectionResource{
		Resource: api.Resource{ID: "sys-coll-v", Owner: "system", Tenant: ""},
		CollectionConfig: api.CollectionConfig{
			Name: "Sys Coll", Category: "test",
			Benchmarks: []api.CollectionBenchmarkConfig{{Ref: api.Ref{ID: "b1"}, ProviderID: "p1"}},
		},
	}
	if err := store.CreateCollection(sysColl); err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}

	// System collections always have VersionCounter == 0 (version tracking is for custom only)
	fetched, err := store.GetCollection("sys-coll-v")
	if err != nil {
		t.Fatalf("GetCollection: %v", err)
	}
	if fetched.Resource.VersionCounter != 0 {
		t.Errorf("system collection VersionCounter should be 0, got %d", fetched.Resource.VersionCounter)
	}
}

func TestCollectionFilters_ScopeCurated(t *testing.T) {
	t.Parallel()
	store, err := getTestStorage(t, "sqlite", getDBName())
	if err != nil {
		t.Fatalf("getTestStorage: %v", err)
	}

	scoped := store.WithTenant("t1").WithOwner("user1")

	// Create a curated collection (curation_order > 0)
	curatedColl := &api.CollectionResource{
		Resource: api.Resource{ID: "curated-filter", Owner: "user1", Tenant: "t1"},
		CollectionConfig: api.CollectionConfig{
			Name: "Curated", Category: "test", CurationOrder: 1,
			Benchmarks: []api.CollectionBenchmarkConfig{{Ref: api.Ref{ID: "b1"}, ProviderID: "p1"}},
		},
	}
	if err := scoped.CreateCollection(curatedColl); err != nil {
		t.Fatalf("CreateCollection curated: %v", err)
	}

	// Create a non-curated collection (curation_order == 0)
	plainColl := &api.CollectionResource{
		Resource: api.Resource{ID: "plain-filter", Owner: "user1", Tenant: "t1"},
		CollectionConfig: api.CollectionConfig{
			Name: "Plain", Category: "test", CurationOrder: 0,
			Benchmarks: []api.CollectionBenchmarkConfig{{Ref: api.Ref{ID: "b2"}, ProviderID: "p1"}},
		},
	}
	if err := scoped.CreateCollection(plainColl); err != nil {
		t.Fatalf("CreateCollection plain: %v", err)
	}

	// Filter scope_curated should return only curated
	filter := &abstractions.QueryFilter{
		Limit: 50, Offset: 0,
		Params: map[string]any{"scope_curated": "true"},
	}
	results, err := scoped.GetCollections(filter)
	if err != nil {
		t.Fatalf("GetCollections scope_curated: %v", err)
	}
	for _, c := range results.Items {
		if c.CurationOrder <= 0 {
			t.Errorf("scope_curated filter returned non-curated collection %q", c.Resource.ID)
		}
	}
	found := false
	for _, c := range results.Items {
		if c.Resource.ID == "curated-filter" {
			found = true
		}
	}
	if !found {
		t.Error("curated collection not returned by scope_curated filter")
	}
}

func TestCollectionFilters_ArrayFields(t *testing.T) {
	t.Parallel()
	store, err := getTestStorage(t, "sqlite", getDBName())
	if err != nil {
		t.Fatalf("getTestStorage: %v", err)
	}

	scoped := store.WithTenant("t1").WithOwner("user1")

	coll := &api.CollectionResource{
		Resource: api.Resource{ID: "array-filter-test", Owner: "user1", Tenant: "t1"},
		CollectionConfig: api.CollectionConfig{
			Name: "Array Filter", Category: "test",
			Domains:    []string{"grounded_document_understanding"},
			Tasks:      []string{"rag", "summarization"},
			Modalities: []string{"text"},
			Industries: []string{"health"},
			AIEntities: []string{"model"},
			Benchmarks: []api.CollectionBenchmarkConfig{{Ref: api.Ref{ID: "b1"}, ProviderID: "p1"}},
		},
	}
	if err := scoped.CreateCollection(coll); err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}

	for _, tc := range []struct {
		key   string
		value string
	}{
		{"domains", "grounded_document_understanding"},
		{"tasks", "rag"},
		{"modalities", "text"},
		{"industries", "health"},
		{"ai_entities", "model"},
	} {
		tc := tc
		t.Run(tc.key, func(t *testing.T) {
			t.Parallel()
			filter := &abstractions.QueryFilter{
				Limit: 50, Offset: 0,
				Params: map[string]any{tc.key: tc.value},
			}
			results, err := scoped.GetCollections(filter)
			if err != nil {
				t.Fatalf("GetCollections %s=%s: %v", tc.key, tc.value, err)
			}
			found := false
			for _, c := range results.Items {
				if c.Resource.ID == "array-filter-test" {
					found = true
				}
			}
			if !found {
				t.Errorf("collection not found with filter %s=%s", tc.key, tc.value)
			}
		})
	}
}

func TestCollectionDeleteCollection(t *testing.T) {
	for _, driver := range []string{"sqlite"} {
		driver := driver
		t.Run(driver, func(t *testing.T) {
			t.Parallel()
			store, err := getTestStorage(t, driver, getDBName())
			if err != nil {
				t.Fatalf("getTestStorage: %v", err)
			}
			scoped := store.WithTenant("t1").WithOwner("user1")

			coll := &api.CollectionResource{
				Resource: api.Resource{ID: "del-test", Owner: "user1", Tenant: "t1"},
				CollectionConfig: api.CollectionConfig{
					Name: "Delete Me", Category: "test",
					Benchmarks: []api.CollectionBenchmarkConfig{{Ref: api.Ref{ID: "b1"}, ProviderID: "p1"}},
				},
			}
			if err := scoped.CreateCollection(coll); err != nil {
				t.Fatalf("CreateCollection: %v", err)
			}

			// Verify it exists
			if _, err := scoped.GetCollection("del-test"); err != nil {
				t.Fatalf("GetCollection before delete: %v", err)
			}

			// Delete it
			if err := scoped.DeleteCollection("del-test"); err != nil {
				t.Fatalf("DeleteCollection: %v", err)
			}

			// Verify it's gone
			if _, err := scoped.GetCollection("del-test"); err == nil {
				t.Error("expected error after deletion, got nil")
			}
		})
	}
}

func TestCollectionDeleteSystemCollectionRejected(t *testing.T) {
	t.Parallel()
	store, err := getTestStorage(t, "sqlite", getDBName())
	if err != nil {
		t.Fatalf("getTestStorage: %v", err)
	}

	sysColl := &api.CollectionResource{
		Resource: api.Resource{ID: "sys-del", Owner: "system"},
		CollectionConfig: api.CollectionConfig{
			Name: "System", Category: "test",
			Benchmarks: []api.CollectionBenchmarkConfig{{Ref: api.Ref{ID: "b1"}, ProviderID: "p1"}},
		},
	}
	if err := store.CreateCollection(sysColl); err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}

	if err := store.DeleteCollection("sys-del"); err == nil {
		t.Error("expected error deleting system collection, got nil")
	}
}

func TestCollectionPatchCollection(t *testing.T) {
	for _, driver := range []string{"sqlite"} {
		driver := driver
		t.Run(driver, func(t *testing.T) {
			t.Parallel()
			store, err := getTestStorage(t, driver, getDBName())
			if err != nil {
				t.Fatalf("getTestStorage: %v", err)
			}
			scoped := store.WithTenant("t1").WithOwner("user1")

			coll := &api.CollectionResource{
				Resource: api.Resource{ID: "patch-test", Owner: "user1", Tenant: "t1"},
				CollectionConfig: api.CollectionConfig{
					Name: "Original Name", Category: "test",
					Benchmarks: []api.CollectionBenchmarkConfig{{Ref: api.Ref{ID: "b1"}, ProviderID: "p1"}},
				},
			}
			if err := scoped.CreateCollection(coll); err != nil {
				t.Fatalf("CreateCollection: %v", err)
			}

			patchOp := api.PatchOpReplace
			patches := &api.Patch{
				{Op: patchOp, Path: "/name", Value: "Patched Name"},
			}
			updated, err := scoped.PatchCollection("patch-test", patches)
			if err != nil {
				t.Fatalf("PatchCollection: %v", err)
			}
			if updated.Name != "Patched Name" {
				t.Errorf("expected name 'Patched Name', got %q", updated.Name)
			}
			// VersionCounter should have been incremented
			if updated.Resource.VersionCounter != 1 {
				t.Errorf("expected VersionCounter=1 after patch, got %d", updated.Resource.VersionCounter)
			}
		})
	}
}

func TestCollectionPatchSystemCollectionRejected(t *testing.T) {
	t.Parallel()
	store, err := getTestStorage(t, "sqlite", getDBName())
	if err != nil {
		t.Fatalf("getTestStorage: %v", err)
	}

	sysColl := &api.CollectionResource{
		Resource: api.Resource{ID: "sys-patch", Owner: "system"},
		CollectionConfig: api.CollectionConfig{
			Name: "System", Category: "test",
			Benchmarks: []api.CollectionBenchmarkConfig{{Ref: api.Ref{ID: "b1"}, ProviderID: "p1"}},
		},
	}
	if err := store.CreateCollection(sysColl); err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}

	patches := &api.Patch{{Op: api.PatchOpReplace, Path: "/name", Value: "New Name"}}
	if _, err := store.PatchCollection("sys-patch", patches); err == nil {
		t.Error("expected error patching system collection, got nil")
	}
}
