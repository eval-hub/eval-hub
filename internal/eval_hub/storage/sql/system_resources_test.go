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

func TestLoadSystemResources_PreservesTimestampsOnUpdate(t *testing.T) {
	store, err := getTestStorage(t, "sqlite", getDBName())
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	initial := map[string]api.ProviderResource{
		"sys-a": {
			Resource:       api.Resource{ID: "sys-a", Owner: "system"},
			ProviderConfig: api.ProviderConfig{Name: "A", Description: "first"},
		},
	}
	if err := store.LoadSystemResources(nil, initial); err != nil {
		t.Fatalf("initial load: %v", err)
	}

	before, err := store.GetProvider("sys-a")
	if err != nil {
		t.Fatalf("GetProvider before reload: %v", err)
	}
	if before.Resource.CreatedAt.IsZero() || before.Resource.UpdatedAt.IsZero() {
		t.Fatal("expected non-zero timestamps after initial load")
	}

	reloaded := map[string]api.ProviderResource{
		"sys-a": {
			Resource:       api.Resource{ID: "sys-a", Owner: "system"},
			ProviderConfig: api.ProviderConfig{Name: "A-updated", Description: "second"},
		},
	}
	if err := store.LoadSystemResources(nil, reloaded); err != nil {
		t.Fatalf("reload: %v", err)
	}

	got, err := store.GetProvider("sys-a")
	if err != nil {
		t.Fatalf("GetProvider: %v", err)
	}
	if !got.Resource.CreatedAt.Equal(before.Resource.CreatedAt) {
		t.Fatalf("CreatedAt not preserved: got %v want %v", got.Resource.CreatedAt, before.Resource.CreatedAt)
	}
	if !got.Resource.UpdatedAt.Equal(before.Resource.UpdatedAt) {
		t.Fatalf("UpdatedAt not preserved: got %v want %v", got.Resource.UpdatedAt, before.Resource.UpdatedAt)
	}
	if got.Name != "A-updated" {
		t.Fatalf("expected updated name, got %s", got.Name)
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
