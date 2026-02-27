package sql_test

import (
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/logging"
	"github.com/eval-hub/eval-hub/internal/storage"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestProviderStorage(t *testing.T) {
	logger := logging.FallbackLogger()
	databaseConfig := map[string]any{
		"driver":        "sqlite",
		"url":           "file::memory:?mode=memory&cache=shared",
		"database_name": "eval_hub",
	}
	store, err := storage.NewStorage(&databaseConfig, false, logger)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	now := time.Now()
	provider := &api.ProviderResource{
		Resource: api.Resource{
			ID:        "provider-1",
			CreatedAt: &now,
			Tenant:    func() *api.Tenant { t := api.Tenant("tenant-1"); return &t }(),
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
		updated := &api.ProviderResource{
			ProviderConfig: api.ProviderConfig{
				Name:        "Updated Provider",
				Description: "Updated description",
				Benchmarks: []api.BenchmarkResource{
					{ID: "bench-1", Name: "Bench 1"},
					{ID: "bench-2", Name: "Bench 2"},
				},
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
