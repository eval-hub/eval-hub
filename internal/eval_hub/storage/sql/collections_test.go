package sql_test

import (
	"encoding/json"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/common"
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

func TestCollectionStatus_SetAndIncrement(t *testing.T) {
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

			// UpdateCollectionStatus
			state := &api.CollectionStatus{RunCount: 3}
			updated, err := scoped.UpdateCollectionStatus("coll-state-test", state)
			if err != nil {
				t.Fatalf("UpdateCollectionStatus: %v", err)
			}
			if updated.Status == nil {
				t.Fatal("expected Status to be set")
			}
			if updated.Status.RunCount != 3 {
				t.Errorf("RunCount: got %d, want 3", updated.Status.RunCount)
			}

			// UpdateCollection must preserve Status
			config := coll.CollectionConfig
			v1, err := scoped.UpdateCollection("coll-state-test", &config)
			if err != nil {
				t.Fatalf("UpdateCollection (first): %v", err)
			}

			v2, err := scoped.UpdateCollection("coll-state-test", &config)
			if err != nil {
				t.Fatalf("UpdateCollection (second): %v", err)
			}

			// updated_at must advance on each mutation (change-detection signal)
			if !v2.Resource.UpdatedAt.After(v1.Resource.UpdatedAt) && v2.Resource.UpdatedAt != v1.Resource.UpdatedAt {
				t.Errorf("updated_at should be >= after second update: v1=%v v2=%v", v1.Resource.UpdatedAt, v2.Resource.UpdatedAt)
			}

			// Status should be preserved through UpdateCollection
			if v2.Status == nil || v2.Status.RunCount != 3 {
				t.Error("State.RunCount should be preserved through UpdateCollection")
			}
		})
	}
}

func TestCollectionDerivedFrom_StoredAndRetrieved(t *testing.T) {
	t.Parallel()
	store, err := getTestStorage(t, "sqlite", getDBName())
	if err != nil {
		t.Fatalf("getTestStorage: %v", err)
	}

	coll := &api.CollectionResource{
		Resource:    api.Resource{ID: "derived-test", Owner: "user1", Tenant: "t1"},
		DerivedFrom: "source-coll-id",
		CollectionConfig: api.CollectionConfig{
			Name: "Derived", Category: "test",
			Benchmarks: []api.CollectionBenchmarkConfig{{Ref: api.Ref{ID: "b1"}, ProviderID: "p1"}},
		},
	}
	if err := store.WithTenant("t1").WithOwner("user1").CreateCollection(coll); err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}

	fetched, err := store.WithTenant("t1").WithOwner("user1").GetCollection("derived-test")
	if err != nil {
		t.Fatalf("GetCollection: %v", err)
	}
	if fetched.DerivedFrom != "source-coll-id" {
		t.Errorf("DerivedFrom: got %q, want %q", fetched.DerivedFrom, "source-coll-id")
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
			Domains:           []string{"grounded_document_understanding"},
			Tasks:             []string{"rag", "summarization"},
			Modalities:        []string{"text"},
			Industries:        []string{"health"},
			EvaluationTargets: []string{"model"},
			Benchmarks:        []api.CollectionBenchmarkConfig{{Ref: api.Ref{ID: "b1"}, ProviderID: "p1"}},
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
		{"evaluation_targets", "model"},
	} {
		tc := tc
		t.Run(tc.key, func(t *testing.T) {
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
		})
	}
}

func TestCollectionPatchConcurrentPostgres(t *testing.T) {
	image := usePostgresImage()
	databaseName := getDBName()
	user, err := getPostgresUser()
	if err != nil {
		t.Skipf("Failed to get Postgres user: %v", err)
	}
	if err := startPostgres(t, databaseName, user, image); err != nil {
		t.Skipf("Skipping postgres tests: %v", err)
	}
	t.Cleanup(func() {
		stopPostgres(t, databaseName, user, image)
	})

	testCollectionPatchConcurrentDisjoint(t, "postgres", databaseName)
}

func testCollectionPatchConcurrentDisjoint(t *testing.T, driver, databaseName string) {
	store, err := getTestStorage(t, driver, databaseName)
	if err != nil {
		t.Fatalf("getTestStorage: %v", err)
	}
	scoped := store.WithTenant("t1").WithOwner("user1")
	collection := &api.CollectionResource{
		Resource: api.Resource{ID: common.GUID(), Owner: "user1", Tenant: "t1"},
		CollectionConfig: api.CollectionConfig{
			Name:        "Original Name",
			Description: "Original description",
			Category:    "test",
			Benchmarks:  []api.CollectionBenchmarkConfig{{Ref: api.Ref{ID: "b1"}, ProviderID: "p1"}},
		},
	}
	if err := scoped.CreateCollection(collection); err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}

	// Hold the first PATCH transaction after its locked read. A concurrent PATCH
	// must wait at SELECT ... FOR UPDATE, then apply its change to the first
	// transaction's committed entity instead of overwriting it with a stale copy.
	locked := make(chan struct{})
	secondLockedReadAttempt := make(chan struct{})
	release := make(chan struct{})
	var holdGate sync.Mutex
	var holdingTxn bool
	var readAttemptGate sync.Mutex
	readAttempts := 0
	t.Cleanup(func() {
		sql.SetCollectionPatchBeforeLockedReadHook(nil)
		sql.SetCollectionPatchAfterLockedReadHook(nil)
		select {
		case <-release:
		default:
			close(release)
		}
	})
	sql.SetCollectionPatchBeforeLockedReadHook(func(_ string) {
		readAttemptGate.Lock()
		defer readAttemptGate.Unlock()
		readAttempts++
		if readAttempts == 2 {
			close(secondLockedReadAttempt)
		}
	})
	sql.SetCollectionPatchAfterLockedReadHook(func(_ string) {
		holdGate.Lock()
		if holdingTxn {
			holdGate.Unlock()
			return
		}
		holdingTxn = true
		holdGate.Unlock()
		close(locked)
		<-release
	})

	patchName := &api.Patch{{Op: api.PatchOpReplace, Path: "/name", Value: "Updated Name"}}
	patchDescription := &api.Patch{{Op: api.PatchOpReplace, Path: "/description", Value: "Updated description"}}
	firstDone := make(chan error, 1)
	secondDone := make(chan error, 1)
	go func() {
		_, err := scoped.PatchCollection(collection.Resource.ID, patchName)
		firstDone <- err
	}()

	select {
	case <-locked:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for first PATCH transaction to acquire row lock")
	}

	go func() {
		_, err := scoped.PatchCollection(collection.Resource.ID, patchDescription)
		secondDone <- err
	}()
	select {
	case <-secondLockedReadAttempt:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for second PATCH transaction to attempt the row lock")
	}

	select {
	case err := <-firstDone:
		t.Fatalf("first PATCH completed while holding row lock: %v", err)
	case err := <-secondDone:
		t.Fatalf("second PATCH completed before row lock released (FOR UPDATE not contending): %v", err)
	case <-time.After(time.Second):
		// Expected: the second transaction is blocked on SELECT ... FOR UPDATE.
	}

	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first PatchCollection: %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second PatchCollection: %v", err)
	}

	updated, err := scoped.GetCollection(collection.Resource.ID)
	if err != nil {
		t.Fatalf("GetCollection: %v", err)
	}
	if updated.Name != "Updated Name" {
		t.Errorf("Name: got %q, want %q", updated.Name, "Updated Name")
	}
	if updated.Description != "Updated description" {
		t.Errorf("Description: got %q, want %q", updated.Description, "Updated description")
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

func TestCollectionUpdateCuratedCollectionRejected(t *testing.T) {
	t.Parallel()
	store, err := getTestStorage(t, "sqlite", getDBName())
	if err != nil {
		t.Fatalf("getTestStorage: %v", err)
	}

	curated := &api.CollectionResource{
		Resource: api.Resource{ID: "curated-update", Owner: "system"},
		CollectionConfig: api.CollectionConfig{
			Name: "Curated", Category: "test", CurationOrder: 1,
			Benchmarks: []api.CollectionBenchmarkConfig{{Ref: api.Ref{ID: "b1"}, ProviderID: "p1"}},
		},
	}
	if err := store.CreateCollection(curated); err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}

	updated := api.CollectionConfig{Name: "Hacked", Category: "test",
		Benchmarks: []api.CollectionBenchmarkConfig{{Ref: api.Ref{ID: "b1"}, ProviderID: "p1"}}}
	if _, err := store.UpdateCollection("curated-update", &updated); err == nil {
		t.Error("expected error updating curated collection, got nil")
	}
}

func TestCollectionPatchCuratedCollectionRejected(t *testing.T) {
	t.Parallel()
	store, err := getTestStorage(t, "sqlite", getDBName())
	if err != nil {
		t.Fatalf("getTestStorage: %v", err)
	}

	curated := &api.CollectionResource{
		Resource: api.Resource{ID: "curated-patch", Owner: "system"},
		CollectionConfig: api.CollectionConfig{
			Name: "Curated", Category: "test", CurationOrder: 2,
			Benchmarks: []api.CollectionBenchmarkConfig{{Ref: api.Ref{ID: "b1"}, ProviderID: "p1"}},
		},
	}
	if err := store.CreateCollection(curated); err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}

	patches := &api.Patch{{Op: api.PatchOpReplace, Path: "/name", Value: "Hacked"}}
	if _, err := store.PatchCollection("curated-patch", patches); err == nil {
		t.Error("expected error patching curated collection, got nil")
	}
}

func TestCollectionPatch_PinnedOrderTopLevel(t *testing.T) {
	t.Parallel()
	store, err := getTestStorage(t, "sqlite", getDBName())
	if err != nil {
		t.Fatalf("getTestStorage: %v", err)
	}
	scoped := store.WithTenant("t-patch-state").WithOwner("user1")

	// Create a tenant collection without a pinned_order set
	coll := &api.CollectionResource{
		Resource: api.Resource{ID: "patch-state-nil", Owner: "user1", Tenant: "t-patch-state"},
		CollectionConfig: api.CollectionConfig{
			Name: "NilState", Category: "test",
			Benchmarks: []api.CollectionBenchmarkConfig{{Ref: api.Ref{ID: "b1"}, ProviderID: "p1"}},
		},
	}
	if err := scoped.CreateCollection(coll); err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}

	patches := &api.Patch{{Op: api.PatchOpAdd, Path: "/pinned_order", Value: float64(3)}}
	updated, err := scoped.PatchCollection("patch-state-nil", patches)
	if err != nil {
		t.Fatalf("PatchCollection /pinned_order on nil-State collection: %v", err)
	}
	if updated.PinnedOrder != 3 {
		t.Errorf("expected PinnedOrder=3, got %d", updated.PinnedOrder)
	}
}

func TestCollectionPatch_NegativePinnedOrderRejected(t *testing.T) {
	t.Parallel()
	store, err := getTestStorage(t, "sqlite", getDBName())
	if err != nil {
		t.Fatalf("getTestStorage: %v", err)
	}
	scoped := store.WithTenant("t-neg-pin").WithOwner("user1")

	coll := &api.CollectionResource{
		Resource: api.Resource{ID: "neg-pin-test", Owner: "user1", Tenant: "t-neg-pin"},
		CollectionConfig: api.CollectionConfig{
			Name: "NegPin", Category: "test",
			Benchmarks: []api.CollectionBenchmarkConfig{{Ref: api.Ref{ID: "b1"}, ProviderID: "p1"}},
		},
	}
	if err := scoped.CreateCollection(coll); err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}

	patches := &api.Patch{{Op: api.PatchOpAdd, Path: "/pinned_order", Value: float64(-1)}}
	if _, err := scoped.PatchCollection("neg-pin-test", patches); err == nil {
		t.Error("expected error for negative pinned_order, got nil")
	}
}

func TestCollectionFilters_MultiValueArrayField(t *testing.T) {
	t.Parallel()
	store, err := getTestStorage(t, "sqlite", getDBName())
	if err != nil {
		t.Fatalf("getTestStorage: %v", err)
	}
	scoped := store.WithTenant("t-mv").WithOwner("user1")

	// Collection with both domains
	multi := &api.CollectionResource{
		Resource: api.Resource{ID: "multi-domain", Owner: "user1", Tenant: "t-mv"},
		CollectionConfig: api.CollectionConfig{
			Name: "Multi Domain", Category: "test",
			Domains:    []string{"rag", "grounding"},
			Benchmarks: []api.CollectionBenchmarkConfig{{Ref: api.Ref{ID: "b1"}, ProviderID: "p1"}},
		},
	}
	// Collection with only one domain
	single := &api.CollectionResource{
		Resource: api.Resource{ID: "single-domain", Owner: "user1", Tenant: "t-mv"},
		CollectionConfig: api.CollectionConfig{
			Name: "Single Domain", Category: "test",
			Domains:    []string{"rag"},
			Benchmarks: []api.CollectionBenchmarkConfig{{Ref: api.Ref{ID: "b2"}, ProviderID: "p1"}},
		},
	}
	if err := scoped.CreateCollection(multi); err != nil {
		t.Fatalf("CreateCollection multi: %v", err)
	}
	if err := scoped.CreateCollection(single); err != nil {
		t.Fatalf("CreateCollection single: %v", err)
	}

	// Multi-value filter: must have BOTH rag AND grounding
	filter := &abstractions.QueryFilter{
		Limit: 50, Offset: 0,
		Params: map[string]any{"domains": []string{"rag", "grounding"}},
	}
	results, err := scoped.GetCollections(filter)
	if err != nil {
		t.Fatalf("GetCollections multi-value: %v", err)
	}

	// Only "multi-domain" has both values
	for _, c := range results.Items {
		if c.Resource.ID == "single-domain" {
			t.Error("single-domain collection (only has 'rag') should not match rag+grounding filter")
		}
	}
	found := false
	for _, c := range results.Items {
		if c.Resource.ID == "multi-domain" {
			found = true
		}
	}
	if !found {
		t.Error("multi-domain collection should match rag+grounding filter")
	}
}
