package sql_test

import (
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/common"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestProviderStorage(t *testing.T) {
	tenant := api.Tenant("tenant-1")
	store, err := getTestStorage(t, "sqlite", getDBName())
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	store = store.WithTenant(tenant)

	provider := &api.ProviderResource{
		Resource: api.Resource{
			ID:        "provider-1",
			CreatedAt: time.Now(),
			Tenant:    tenant,
		},
		ProviderConfig: api.ProviderConfig{
			Name:        "Test Provider",
			Description: "A test provider",
			Benchmarks: []api.BenchmarkResource{
				{
					ID:          "bench-1",
					Name:        "Benchmark 1",
					Description: "First benchmark",
				},
			},
		},
	}

	t.Run("CreateUserProvider creates a new provider", func(t *testing.T) {
		err := store.CreateProvider(provider)
		if err != nil {
			t.Fatalf("CreateUserProvider failed: %v", err)
		}
	})

	t.Run("GetUserProvider returns the provider", func(t *testing.T) {
		got, err := store.GetProvider("provider-1")
		if err != nil {
			t.Fatalf("GetUserProvider failed: %v", err)
		}
		if got.Resource.ID != "provider-1" {
			t.Errorf("Expected ID provider-1, got %s", got.Resource.ID)
		}
		if got.Name != "Test Provider" {
			t.Errorf("Expected Name Test Provider, got %s", got.Name)
		}
		if len(got.Benchmarks) != 1 {
			t.Errorf("Expected 1 benchmark, got %d", len(got.Benchmarks))
		}
		if got.Benchmarks[0].ID != "bench-1" {
			t.Errorf("Expected benchmark ID bench-1, got %s", got.Benchmarks[0].ID)
		}
	})

	t.Run("UpdateProvider updates the provider config", func(t *testing.T) {
		updated := &api.ProviderConfig{
			Name:        "Updated Provider",
			Description: "Updated description",
			Benchmarks: []api.BenchmarkResource{
				{ID: "bench-1", Name: "Bench 1"},
				{ID: "bench-2", Name: "Bench 2"},
			},
		}
		got, err := store.UpdateProvider("provider-1", updated)
		if err != nil {
			t.Fatalf("UpdateProvider failed: %v", err)
		}
		if got.Name != "Updated Provider" {
			t.Errorf("Expected Name Updated Provider, got %s", got.Name)
		}
		if got.Description != "Updated description" {
			t.Errorf("Expected Description Updated description, got %s", got.Description)
		}
		if len(got.Benchmarks) != 2 {
			t.Errorf("Expected 2 benchmarks, got %d", len(got.Benchmarks))
		}
	})

	t.Run("PatchProvider patches the provider config", func(t *testing.T) {
		patches := api.Patch{
			{Op: api.PatchOpReplace, Path: "/description", Value: "Patched description"},
		}
		got, err := store.PatchProvider("provider-1", &patches)
		if err != nil {
			t.Fatalf("PatchProvider failed: %v", err)
		}
		if got.Description != "Patched description" {
			t.Errorf("Expected Description Patched description, got %s", got.Description)
		}
		if got.Name != "Updated Provider" {
			t.Errorf("Expected Name unchanged, got %s", got.Name)
		}
	})

	t.Run("GetProviders with name filter returns matching providers", func(t *testing.T) {
		filter := &abstractions.QueryFilter{
			Limit:  10,
			Offset: 0,
			Params: map[string]any{"name": "Updated Provider"},
		}
		got, err := store.GetProviders(filter)
		if err != nil {
			t.Fatalf("GetProviders failed: %v", err)
		}
		if got.TotalCount != 1 {
			t.Errorf("Expected 1 provider, got total_count=%d", got.TotalCount)
		}
		if len(got.Items) != 1 {
			t.Errorf("Expected 1 item, got %d", len(got.Items))
		}
		if len(got.Items) > 0 && got.Items[0].Name != "Updated Provider" {
			t.Errorf("Expected name Updated Provider, got %s", got.Items[0].Name)
		}
	})

	t.Run("GetProviders with tags filter returns matching providers", func(t *testing.T) {
		providerWithTags := &api.ProviderResource{
			Resource: api.Resource{
				ID:        "provider-2",
				CreatedAt: time.Now(),
				Tenant:    api.Tenant("tenant-1"),
			},
			ProviderConfig: api.ProviderConfig{
				Name:        "Tagged Provider",
				Description: "Provider with tags",
				Tags:        []string{"list-test-tag", "searchable"},
			},
		}
		if err := store.CreateProvider(providerWithTags); err != nil {
			t.Fatalf("CreateProvider failed: %v", err)
		}
		t.Cleanup(func() {
			if err := store.DeleteProvider("provider-2"); err != nil {
				t.Errorf("DeleteProvider(provider-2) cleanup: %v", err)
			}
		})

		filter := &abstractions.QueryFilter{
			Limit:  10,
			Offset: 0,
			Params: map[string]any{"tags": "list-test-tag"},
		}
		got, err := store.GetProviders(filter)
		if err != nil {
			t.Fatalf("GetProviders failed: %v", err)
		}
		if got.TotalCount != 1 {
			t.Errorf("Expected 1 provider with tag, got total_count=%d", got.TotalCount)
		}
		if len(got.Items) > 0 && got.Items[0].Name != "Tagged Provider" {
			t.Errorf("Expected name Tagged Provider, got %s", got.Items[0].Name)
		}
	})

	t.Run("GetUserProvider returns not found for missing provider", func(t *testing.T) {
		_, err := store.GetProvider("non-existent")
		if err == nil {
			t.Fatal("Expected error for non-existent provider")
		}
	})

	t.Run("DeleteUserProvider removes the provider", func(t *testing.T) {
		err := store.DeleteProvider("provider-1")
		if err != nil {
			t.Fatalf("DeleteUserProvider failed: %v", err)
		}

		_, err = store.GetProvider("provider-1")
		if err == nil {
			t.Fatal("Expected error after delete, provider should not exist")
		}
	})
}

func TestProviders_OwnerIsolation(t *testing.T) {
	store, err := getTestStorage(t, "sqlite", getDBName())
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	now := time.Now()
	tenant := api.Tenant(getTenant("prov-owner-iso"))

	makeProvider := func(id string, owner api.User) *api.ProviderResource {
		return &api.ProviderResource{
			Resource: api.Resource{
				ID:        id,
				Tenant:    tenant,
				Owner:     owner,
				CreatedAt: now,
				UpdatedAt: now,
			},
			ProviderConfig: api.ProviderConfig{
				Name:        "Provider " + id,
				Description: "Owned by " + string(owner),
			},
		}
	}

	provA := common.GUID()
	if err := store.CreateProvider(makeProvider(provA, "user-a")); err != nil {
		t.Fatalf("create provider user-a: %v", err)
	}
	provB := common.GUID()
	if err := store.CreateProvider(makeProvider(provB, "user-b")); err != nil {
		t.Fatalf("create provider user-b: %v", err)
	}

	filter := &abstractions.QueryFilter{Limit: 50, Offset: 0, Params: map[string]any{}}

	t.Run("user-a sees only own providers", func(t *testing.T) {
		scoped := store.WithTenant(tenant).WithOwner("user-a")
		res, err := scoped.GetProviders(filter)
		if err != nil {
			t.Fatalf("GetProviders: %v", err)
		}
		if len(res.Items) != 1 {
			t.Fatalf("expected 1 provider, got %d", len(res.Items))
		}
		if res.Items[0].Resource.ID != provA {
			t.Fatalf("expected provider %s, got %s", provA, res.Items[0].Resource.ID)
		}
	})

	t.Run("user-b sees only own providers", func(t *testing.T) {
		scoped := store.WithTenant(tenant).WithOwner("user-b")
		res, err := scoped.GetProviders(filter)
		if err != nil {
			t.Fatalf("GetProviders: %v", err)
		}
		if len(res.Items) != 1 {
			t.Fatalf("expected 1 provider, got %d", len(res.Items))
		}
		if res.Items[0].Resource.ID != provB {
			t.Fatalf("expected provider %s, got %s", provB, res.Items[0].Resource.ID)
		}
	})

	t.Run("user-a cannot GET user-b provider by ID", func(t *testing.T) {
		scoped := store.WithTenant(tenant).WithOwner("user-a")
		_, err := scoped.GetProvider(provB)
		if err == nil {
			t.Fatal("expected error getting other user's provider")
		}
	})

	t.Run("user-a cannot delete user-b provider", func(t *testing.T) {
		scoped := store.WithTenant(tenant).WithOwner("user-a")
		err := scoped.DeleteProvider(provB)
		if err == nil {
			t.Fatal("expected error deleting other user's provider")
		}
	})

	t.Run("system providers visible to all owners", func(t *testing.T) {
		sysProv := common.GUID()
		if err := store.CreateProvider(makeProvider(sysProv, "system")); err != nil {
			t.Fatalf("create system provider: %v", err)
		}
		scoped := store.WithTenant(tenant).WithOwner("user-a")
		res, err := scoped.GetProviders(filter)
		if err != nil {
			t.Fatalf("GetProviders: %v", err)
		}
		found := false
		for _, p := range res.Items {
			if p.Resource.ID == sysProv {
				found = true
				break
			}
		}
		if !found {
			t.Fatal("expected system provider to be visible to user-a")
		}
	})
}
