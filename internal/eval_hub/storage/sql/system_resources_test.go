package sql_test

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func testSystemCollection(id, name, description string) api.CollectionResource {
	return api.CollectionResource{
		Resource: api.Resource{ID: id, Owner: "system"},
		CollectionConfig: api.CollectionConfig{
			Name:        name,
			Description: description,
			Category:    "test",
			Benchmarks: []api.CollectionBenchmarkConfig{
				{
					Ref:        api.Ref{ID: "bench-1"},
					ProviderID: "provider-1",
				},
			},
		},
	}
}

func TestLoadSystemResources_ReplacesLargeSystemSetAndPreservesTenantProviders(t *testing.T) {
	store, err := getTestStorage(t, "sqlite", getDBName())
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	const systemCount = 250 // exceeds the previous 200-row pagination page size
	initialSystem := make(map[string]api.ProviderResource, systemCount)
	for i := range systemCount {
		id := fmt.Sprintf("sys-provider-%03d", i)
		initialSystem[id] = api.ProviderResource{
			Resource: api.Resource{ID: id, Owner: "system"},
			ProviderConfig: api.ProviderConfig{
				Name:        id,
				Description: "system provider",
			},
		}
	}
	if err := store.LoadSystemResources(nil, initialSystem); err != nil {
		t.Fatalf("initial LoadSystemResources: %v", err)
	}

	tenant := api.Tenant("tenant-keep")
	tenantStore := store.WithTenant(tenant)
	tenantProvider := &api.ProviderResource{
		Resource: api.Resource{
			ID:        "tenant-provider-1",
			Tenant:    tenant,
			CreatedAt: time.Now(),
		},
		ProviderConfig: api.ProviderConfig{
			Name:        "Tenant Provider",
			Description: "must survive system reload",
		},
	}
	if err := tenantStore.CreateProvider(tenantProvider); err != nil {
		t.Fatalf("CreateProvider tenant: %v", err)
	}

	// Keep only the second half of system providers so orphans must be deleted.
	kept := make(map[string]api.ProviderResource, systemCount/2)
	for i := systemCount / 2; i < systemCount; i++ {
		id := fmt.Sprintf("sys-provider-%03d", i)
		kept[id] = initialSystem[id]
	}
	if err := store.LoadSystemResources(nil, kept); err != nil {
		t.Fatalf("reload LoadSystemResources: %v", err)
	}

	systemListed, err := store.GetProviders(&abstractions.QueryFilter{
		Limit:  1000,
		Params: map[string]any{"scope": abstractions.ScopeSystem},
	})
	if err != nil {
		t.Fatalf("GetProviders system: %v", err)
	}
	if systemListed.TotalCount != len(kept) {
		t.Fatalf("expected %d system providers after reload, got total_count=%d items=%d",
			len(kept), systemListed.TotalCount, len(systemListed.Items))
	}
	for _, p := range systemListed.Items {
		if _, ok := kept[p.Resource.ID]; !ok {
			t.Fatalf("unexpected system provider still present: %s", p.Resource.ID)
		}
	}

	gotTenant, err := tenantStore.GetProvider("tenant-provider-1")
	if err != nil {
		t.Fatalf("tenant provider should remain after system reload: %v", err)
	}
	if gotTenant.Name != "Tenant Provider" {
		t.Fatalf("tenant provider name changed: %s", gotTenant.Name)
	}

	_, err = store.GetProvider("sys-provider-000")
	if err == nil {
		t.Fatal("expected orphaned system provider sys-provider-000 to be deleted")
	}
}

func TestLoadSystemResources_ReplacesLargeSystemCollections(t *testing.T) {
	store, err := getTestStorage(t, "sqlite", getDBName())
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	const systemCount = 250
	initial := make(map[string]api.CollectionResource, systemCount)
	for i := range systemCount {
		id := fmt.Sprintf("sys-collection-%03d", i)
		initial[id] = testSystemCollection(id, id, "system collection")
	}
	if err := store.LoadSystemResources(initial, nil); err != nil {
		t.Fatalf("initial LoadSystemResources: %v", err)
	}

	tenant := api.Tenant("tenant-keep")
	tenantStore := store.WithTenant(tenant)
	tenantCollection := &api.CollectionResource{
		Resource: api.Resource{
			ID:        "tenant-collection-1",
			Tenant:    tenant,
			CreatedAt: time.Now(),
		},
		CollectionConfig: api.CollectionConfig{
			Name:        "Tenant Collection",
			Description: "must survive system reload",
			Category:    "test",
			Benchmarks: []api.CollectionBenchmarkConfig{
				{Ref: api.Ref{ID: "bench-1"}, ProviderID: "provider-1"},
			},
		},
	}
	if err := tenantStore.CreateCollection(tenantCollection); err != nil {
		t.Fatalf("CreateCollection tenant: %v", err)
	}

	kept := make(map[string]api.CollectionResource, systemCount/2)
	for i := systemCount / 2; i < systemCount; i++ {
		id := fmt.Sprintf("sys-collection-%03d", i)
		kept[id] = initial[id]
	}
	if err := store.LoadSystemResources(kept, nil); err != nil {
		t.Fatalf("reload LoadSystemResources: %v", err)
	}

	systemListed, err := store.GetCollections(&abstractions.QueryFilter{
		Limit:  1000,
		Params: map[string]any{"scope": abstractions.ScopeSystem},
	})
	if err != nil {
		t.Fatalf("GetCollections system: %v", err)
	}
	if systemListed.TotalCount != len(kept) {
		t.Fatalf("expected %d system collections after reload, got total_count=%d items=%d",
			len(kept), systemListed.TotalCount, len(systemListed.Items))
	}

	gotTenant, err := tenantStore.GetCollection("tenant-collection-1")
	if err != nil {
		t.Fatalf("tenant collection should remain after system reload: %v", err)
	}
	if gotTenant.Name != "Tenant Collection" {
		t.Fatalf("tenant collection name changed: %s", gotTenant.Name)
	}

	_, err = store.GetCollection("sys-collection-000")
	if err == nil {
		t.Fatal("expected orphaned system collection sys-collection-000 to be deleted")
	}
}

func TestLoadSystemResources_TimestampsOnContentChangeAndUnchangedReload(t *testing.T) {
	store, err := getTestStorage(t, "sqlite", getDBName())
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	initialProviders := map[string]api.ProviderResource{
		"sys-a": {
			Resource:       api.Resource{ID: "sys-a", Owner: "system"},
			ProviderConfig: api.ProviderConfig{Name: "A", Description: "first"},
		},
	}
	initialCollections := map[string]api.CollectionResource{
		"sys-c": testSystemCollection("sys-c", "C", "first"),
	}
	if err := store.LoadSystemResources(initialCollections, initialProviders); err != nil {
		t.Fatalf("initial load: %v", err)
	}

	beforeProvider, err := store.GetProvider("sys-a")
	if err != nil {
		t.Fatalf("GetProvider before reload: %v", err)
	}
	beforeCollection, err := store.GetCollection("sys-c")
	if err != nil {
		t.Fatalf("GetCollection before reload: %v", err)
	}

	time.Sleep(5 * time.Millisecond)

	// Unchanged content should preserve both timestamps.
	if err := store.LoadSystemResources(initialCollections, initialProviders); err != nil {
		t.Fatalf("unchanged reload: %v", err)
	}
	unchangedProvider, err := store.GetProvider("sys-a")
	if err != nil {
		t.Fatalf("GetProvider after unchanged reload: %v", err)
	}
	if !unchangedProvider.Resource.CreatedAt.Equal(beforeProvider.Resource.CreatedAt) {
		t.Fatalf("provider CreatedAt changed on identical reload")
	}
	if !unchangedProvider.Resource.UpdatedAt.Equal(beforeProvider.Resource.UpdatedAt) {
		t.Fatalf("provider UpdatedAt changed on identical reload: got %v want %v",
			unchangedProvider.Resource.UpdatedAt, beforeProvider.Resource.UpdatedAt)
	}
	unchangedCollection, err := store.GetCollection("sys-c")
	if err != nil {
		t.Fatalf("GetCollection after unchanged reload: %v", err)
	}
	if !unchangedCollection.Resource.UpdatedAt.Equal(beforeCollection.Resource.UpdatedAt) {
		t.Fatalf("collection UpdatedAt changed on identical reload")
	}

	time.Sleep(5 * time.Millisecond)

	changedProviders := map[string]api.ProviderResource{
		"sys-a": {
			Resource:       api.Resource{ID: "sys-a", Owner: "system"},
			ProviderConfig: api.ProviderConfig{Name: "A-updated", Description: "second"},
		},
	}
	changedCollections := map[string]api.CollectionResource{
		"sys-c": testSystemCollection("sys-c", "C-updated", "second"),
	}
	if err := store.LoadSystemResources(changedCollections, changedProviders); err != nil {
		t.Fatalf("content-change reload: %v", err)
	}

	gotProvider, err := store.GetProvider("sys-a")
	if err != nil {
		t.Fatalf("GetProvider after content change: %v", err)
	}
	if gotProvider.Name != "A-updated" {
		t.Fatalf("expected updated provider name, got %s", gotProvider.Name)
	}
	if !gotProvider.Resource.CreatedAt.Equal(beforeProvider.Resource.CreatedAt) {
		t.Fatalf("provider CreatedAt not preserved: got %v want %v",
			gotProvider.Resource.CreatedAt, beforeProvider.Resource.CreatedAt)
	}
	if !gotProvider.Resource.UpdatedAt.After(beforeProvider.Resource.UpdatedAt) {
		t.Fatalf("provider UpdatedAt should advance on content change: got %v want after %v",
			gotProvider.Resource.UpdatedAt, beforeProvider.Resource.UpdatedAt)
	}

	gotCollection, err := store.GetCollection("sys-c")
	if err != nil {
		t.Fatalf("GetCollection after content change: %v", err)
	}
	if gotCollection.Name != "C-updated" {
		t.Fatalf("expected updated collection name, got %s", gotCollection.Name)
	}
	if !gotCollection.Resource.CreatedAt.Equal(beforeCollection.Resource.CreatedAt) {
		t.Fatalf("collection CreatedAt not preserved")
	}
	if !gotCollection.Resource.UpdatedAt.After(beforeCollection.Resource.UpdatedAt) {
		t.Fatalf("collection UpdatedAt should advance on content change: got %v want after %v",
			gotCollection.Resource.UpdatedAt, beforeCollection.Resource.UpdatedAt)
	}
}

func TestLoadSystemResources_SerializesConcurrentCalls(t *testing.T) {
	store, err := getTestStorage(t, "sqlite", getDBName())
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	const goroutines = 8
	var wg sync.WaitGroup
	var failures atomic.Int32
	wg.Add(goroutines)
	for i := range goroutines {
		go func(i int) {
			defer wg.Done()
			providers := map[string]api.ProviderResource{
				"shared-sys": {
					Resource: api.Resource{ID: "shared-sys", Owner: "system"},
					ProviderConfig: api.ProviderConfig{
						Name:        fmt.Sprintf("name-%d", i),
						Description: "concurrent reload",
					},
				},
				fmt.Sprintf("only-%d", i): {
					Resource: api.Resource{ID: fmt.Sprintf("only-%d", i), Owner: "system"},
					ProviderConfig: api.ProviderConfig{
						Name:        fmt.Sprintf("only-%d", i),
						Description: "per-goroutine",
					},
				},
			}
			if err := store.LoadSystemResources(nil, providers); err != nil {
				failures.Add(1)
				t.Errorf("LoadSystemResources goroutine %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	if failures.Load() != 0 {
		t.Fatalf("%d concurrent LoadSystemResources calls failed", failures.Load())
	}

	systemListed, err := store.GetProviders(&abstractions.QueryFilter{
		Limit:  100,
		Params: map[string]any{"scope": abstractions.ScopeSystem},
	})
	if err != nil {
		t.Fatalf("GetProviders: %v", err)
	}
	// Last writer wins: exactly one full desired set should remain (2 providers).
	if systemListed.TotalCount != 2 {
		t.Fatalf("expected 2 system providers after serialized concurrent reloads, got %d", systemListed.TotalCount)
	}
	if _, err := store.GetProvider("shared-sys"); err != nil {
		t.Fatalf("shared-sys missing after concurrent reloads: %v", err)
	}
}

func TestCreateProviderAndCollection_DefaultTimestamps(t *testing.T) {
	store, err := getTestStorage(t, "sqlite", getDBName())
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	tenant := api.Tenant("tenant-ts")
	store = store.WithTenant(tenant)

	provider := &api.ProviderResource{
		Resource: api.Resource{ID: "p-ts", Tenant: tenant, Owner: "user"},
		ProviderConfig: api.ProviderConfig{
			Name:        "P",
			Description: "timestamps defaulted",
		},
	}
	if err := store.CreateProvider(provider); err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}
	gotProvider, err := store.GetProvider("p-ts")
	if err != nil {
		t.Fatalf("GetProvider: %v", err)
	}
	if gotProvider.Resource.CreatedAt.IsZero() || gotProvider.Resource.UpdatedAt.IsZero() {
		t.Fatal("expected CreateProvider to populate timestamps")
	}

	collection := &api.CollectionResource{
		Resource: api.Resource{ID: "c-ts", Tenant: tenant, Owner: "user"},
		CollectionConfig: api.CollectionConfig{
			Name:        "C",
			Description: "timestamps defaulted",
			Category:    "test",
			Benchmarks: []api.CollectionBenchmarkConfig{
				{Ref: api.Ref{ID: "bench-1"}, ProviderID: "provider-1"},
			},
		},
	}
	if err := store.CreateCollection(collection); err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	gotCollection, err := store.GetCollection("c-ts")
	if err != nil {
		t.Fatalf("GetCollection: %v", err)
	}
	if gotCollection.Resource.CreatedAt.IsZero() || gotCollection.Resource.UpdatedAt.IsZero() {
		t.Fatal("expected CreateCollection to populate timestamps")
	}
}
