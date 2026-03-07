package common

import (
	"testing"

	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestResolveProvider_FromMap(t *testing.T) {
	providers := map[string]api.ProviderResource{
		"p1": {Resource: api.Resource{ID: "p1"}},
	}
	got, err := ResolveProvider("p1", providers, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got == nil || got.Resource.ID != "p1" {
		t.Fatalf("expected provider p1, got %v", got)
	}
}

func TestResolveProvider_NotFound(t *testing.T) {
	providers := map[string]api.ProviderResource{}
	got, err := ResolveProvider("missing", providers, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got != nil {
		t.Fatalf("expected nil provider, got %v", got)
	}
	if err.Error() != `provider "missing" not found` {
		t.Fatalf("expected 'provider \"missing\" not found', got %q", err.Error())
	}
}
