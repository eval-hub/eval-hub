package config

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/validation"
	"github.com/eval-hub/eval-hub/internal/logging"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/fsnotify/fsnotify"
)

// mockStorage implements abstractions.Storage for testing the watcher's reload
// behaviour. Only LoadSystemResources is functional; other methods panic.
type mockStorage struct {
	abstractions.Storage // embed to satisfy interface; unused methods will panic
	mu                   sync.Mutex
	loadCalls            int
	lastCollections      map[string]api.CollectionResource
	lastProviders        map[string]api.ProviderResource
}

func (m *mockStorage) LoadSystemResources(collections map[string]api.CollectionResource, providers map[string]api.ProviderResource) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loadCalls++
	m.lastCollections = collections
	m.lastProviders = providers
	return nil
}

func (m *mockStorage) getLoadCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.loadCalls
}

func (m *mockStorage) getLastProviders() map[string]api.ProviderResource {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastProviders
}

func TestWatcher_ReloadsOnFileChange(t *testing.T) {
	dir := t.TempDir()
	provDir := filepath.Join(dir, "providers")
	collDir := filepath.Join(dir, "collections")
	os.MkdirAll(provDir, 0755)
	os.MkdirAll(collDir, 0755)

	// Write initial provider config
	writeTestProvider(t, provDir, "alpha", "Alpha Provider")

	logger := logging.FallbackLogger()
	store := &mockStorage{}
	validate := validation.NewValidator()

	w := NewWatcher(logger, validate, store, dir)
	w.debounce = 100 * time.Millisecond // speed up tests

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	watchReady := make(chan struct{}, 1)
	go func() {
		close(watchReady)
		_ = w.Watch(ctx)
	}()
	<-watchReady
	// Give fsnotify time to set up watches
	time.Sleep(200 * time.Millisecond)

	// Modify a provider file to trigger reload
	writeTestProvider(t, provDir, "beta", "Beta Provider")

	// Wait for the debounced reload to fire
	deadline := time.After(3 * time.Second)
	for {
		if store.getLoadCalls() > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("Timed out waiting for reload after file change")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}

	providers := store.getLastProviders()
	if len(providers) != 2 {
		t.Fatalf("Expected 2 providers after reload, got %d", len(providers))
	}
	if _, ok := providers["beta"]; !ok {
		t.Fatal("Expected provider 'beta' after reload")
	}
}

func TestWatcher_DebouncesMutipleEvents(t *testing.T) {
	dir := t.TempDir()
	provDir := filepath.Join(dir, "providers")
	collDir := filepath.Join(dir, "collections")
	os.MkdirAll(provDir, 0755)
	os.MkdirAll(collDir, 0755)

	logger := logging.FallbackLogger()
	store := &mockStorage{}
	validate := validation.NewValidator()

	w := NewWatcher(logger, validate, store, dir)
	w.debounce = 200 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = w.Watch(ctx) }()
	time.Sleep(200 * time.Millisecond)

	// Write multiple files rapidly — should result in a single reload
	for i := range 5 {
		writeTestProvider(t, provDir, "p"+string(rune('a'+i)), "Provider")
		time.Sleep(20 * time.Millisecond)
	}

	// Wait for debounce + processing
	time.Sleep(500 * time.Millisecond)

	calls := store.getLoadCalls()
	if calls > 2 {
		t.Fatalf("Expected at most 2 reload calls (debounced), got %d", calls)
	}
	if calls == 0 {
		t.Fatal("Expected at least 1 reload call")
	}
}

func TestWatcher_StopsOnContextCancel(t *testing.T) {
	dir := t.TempDir()
	provDir := filepath.Join(dir, "providers")
	os.MkdirAll(provDir, 0755)

	logger := logging.FallbackLogger()
	store := &mockStorage{}
	validate := validation.NewValidator()

	w := NewWatcher(logger, validate, store, dir)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		_ = w.Watch(ctx)
		close(done)
	}()
	time.Sleep(100 * time.Millisecond)

	cancel()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("Watcher did not stop after context cancellation")
	}
}

func TestIsRelevantEvent(t *testing.T) {
	tests := []struct {
		name     string
		op       fsnotify.Op
		relevant bool
	}{
		{"Create", fsnotify.Create, true},
		{"Write", fsnotify.Write, true},
		{"Remove", fsnotify.Remove, true},
		{"Rename", fsnotify.Rename, false},
		{"Chmod", fsnotify.Chmod, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := fsnotify.Event{Name: "test.yaml", Op: tt.op}
			if got := isRelevantEvent(event); got != tt.relevant {
				t.Fatalf("isRelevantEvent(%v) = %v, want %v", tt.op, got, tt.relevant)
			}
		})
	}
}

func writeTestProvider(t *testing.T, dir, id, name string) {
	t.Helper()
	content := "id: " + id + "\nname: " + name + "\ndescription: test provider\n"
	if err := os.WriteFile(filepath.Join(dir, id+".yaml"), []byte(content), 0600); err != nil {
		t.Fatalf("Failed to write test provider: %v", err)
	}
}
