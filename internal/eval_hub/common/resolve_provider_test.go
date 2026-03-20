package common

import (
	"fmt"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/pkg/api"
)

type fakeStorage struct {
	abstractions.Storage
	lastStatusID      string
	lastStatus        api.OverallState
	job               *api.EvaluationJobResource
	deleteID          string
	providerConfigs   map[string]api.ProviderResource
	collectionConfigs map[string]api.CollectionResource
}

func (f *fakeStorage) GetProvider(id string) (*api.ProviderResource, error) {
	if p, ok := f.providerConfigs[id]; ok {
		return &p, nil
	}
	return nil, fmt.Errorf("provider %q not found", id)
}

func TestResolveProvider_FromMap(t *testing.T) {
	providers := map[string]api.ProviderResource{
		"p1": {Resource: api.Resource{ID: "p1"}},
	}
	storage := &fakeStorage{providerConfigs: providers}
	got, err := ResolveProvider("p1", storage)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got == nil || got.Resource.ID != "p1" {
		t.Fatalf("expected provider p1, got %v", got)
	}
}

func TestResolveProvider_NotFound(t *testing.T) {
	storage := &fakeStorage{}
	got, err := ResolveProvider("missing", storage)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got != nil {
		t.Fatalf("expected nil provider, got %v", got)
	}
	if !strings.Contains(err.Error(), `provider "missing" not found`) {
		t.Fatalf("expected 'provider \"missing\" not found', got %q", err.Error())
	}
}
