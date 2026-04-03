package config

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/fsnotify/fsnotify"
	"github.com/go-playground/validator/v10"
)

// Watcher monitors provider and collection config directories for changes
// and reloads system resources into storage when files are modified.
type Watcher struct {
	logger    *slog.Logger
	validate  *validator.Validate
	storage   abstractions.Storage
	configDir string
	debounce  time.Duration
}

// NewWatcher creates a config watcher that monitors the given config directory
// for changes to provider and collection YAML files.
func NewWatcher(logger *slog.Logger, validate *validator.Validate, storage abstractions.Storage, configDir string) *Watcher {
	return &Watcher{
		logger:    logger.With("component", "config-watcher"),
		validate:  validate,
		storage:   storage,
		configDir: configDir,
		debounce:  500 * time.Millisecond,
	}
}

// Watch starts watching the provider and collection config directories for
// changes. It blocks until the context is cancelled. Kubernetes ConfigMap
// volume mounts update via atomic symlink swaps, which appear as Create
// events on the "..data" symlink; this watcher handles that pattern.
func (w *Watcher) Watch(ctx context.Context) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	providerDir := w.resolveDir("providers")
	collectionDir := w.resolveDir("collections")

	dirs := []string{providerDir, collectionDir}
	watched := 0
	for _, dir := range dirs {
		if _, err := os.Stat(dir); err != nil {
			w.logger.Warn("Config directory not found, skipping watch", "dir", dir)
			continue
		}
		if err := watcher.Add(dir); err != nil {
			w.logger.Error("Failed to watch config directory", "dir", dir, "error", err.Error())
			continue
		}
		w.logger.Info("Watching config directory for changes", "dir", dir)
		watched++
	}

	// Also watch the parent config dir for Kubernetes ConfigMap symlink swaps.
	// When a ConfigMap is updated, kubelet atomically replaces the ..data symlink,
	// which triggers a Create event on the parent directory.
	parentDir := w.resolveParentDir()
	if parentDir != "" {
		if _, err := os.Stat(parentDir); err == nil {
			if err := watcher.Add(parentDir); err != nil {
				w.logger.Warn("Failed to watch parent config directory", "dir", parentDir, "error", err.Error())
			} else {
				w.logger.Info("Watching parent config directory for ConfigMap symlink swaps", "dir", parentDir)
				watched++
			}
		}
	}

	if watched == 0 {
		w.logger.Warn("No config directories found to watch")
		<-ctx.Done()
		return nil
	}

	// reloadWg tracks in-flight reload goroutines spawned by time.AfterFunc.
	// Watch waits for them before returning so that callers (e.g. main) can
	// safely close storage after Watch returns without racing with reload().
	var reloadWg sync.WaitGroup
	var debounceTimer *time.Timer
	for {
		select {
		case <-ctx.Done():
			w.logger.Info("Config watcher stopping")
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			reloadWg.Wait()
			return nil

		case event, ok := <-watcher.Events:
			if !ok {
				reloadWg.Wait()
				return nil
			}
			if !isRelevantEvent(event) {
				continue
			}
			w.logger.Info("Config file change detected", "event", event.Op.String(), "file", event.Name)

			// Debounce: ConfigMap updates and file writes can trigger multiple
			// events in rapid succession. Wait for events to settle.
			if debounceTimer != nil {
				if debounceTimer.Stop() {
					// Timer was pending and has been stopped; the callback
					// won't fire, so release the WaitGroup ticket.
					reloadWg.Done()
				}
			}
			reloadWg.Add(1)
			debounceTimer = time.AfterFunc(w.debounce, func() {
				defer reloadWg.Done()
				w.reload()
			})

		case err, ok := <-watcher.Errors:
			if !ok {
				reloadWg.Wait()
				return nil
			}
			w.logger.Error("Config watcher error", "error", err.Error())
		}
	}
}

// isRelevantEvent returns true for filesystem events that indicate a config
// file may have changed. This includes Create (new files, symlink swaps),
// Write (in-place edits), and Remove (deleted files).
func isRelevantEvent(event fsnotify.Event) bool {
	return event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove) != 0
}

// reload re-reads provider and collection configs from disk and updates
// the storage layer. Errors are logged but do not stop the watcher.
func (w *Watcher) reload() {
	w.logger.Info("Reloading system providers and collections")

	providerConfigs, providerErr := LoadProviderConfigs(w.logger, w.validate, w.configDir)
	collectionConfigs, collectionErr := LoadCollectionConfigs(w.logger, w.validate, w.configDir)

	if providerErr != nil || collectionErr != nil {
		if providerErr != nil {
			w.logger.Error("Failed to reload provider configs", "error", providerErr.Error())
		}
		if collectionErr != nil {
			w.logger.Error("Failed to reload collection configs", "error", collectionErr.Error())
		}
		return
	}

	if err := w.storage.LoadSystemResources(collectionConfigs, providerConfigs); err != nil {
		w.logger.Error("Failed to update system resources in storage", "error", err.Error())
		return
	}

	w.logger.Info("Successfully reloaded system resources",
		"providers", len(providerConfigs),
		"collections", len(collectionConfigs),
	)
}

func (w *Watcher) resolveDir(subdir string) string {
	if w.configDir != "" {
		return filepath.Join(w.configDir, subdir)
	}
	// Fall back to default config lookup paths
	for _, dir := range configLookup {
		path := filepath.Join(dir, subdir)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return filepath.Join("config", subdir)
}

func (w *Watcher) resolveParentDir() string {
	if w.configDir != "" {
		return w.configDir
	}
	for _, dir := range configLookup {
		if _, err := os.Stat(dir); err == nil {
			abs, err := filepath.Abs(dir)
			if err == nil {
				return abs
			}
			return dir
		}
	}
	return ""
}

func SetupWatcher(logger *slog.Logger, validate *validator.Validate, storage abstractions.Storage, configDir string) (chan struct{}, context.CancelFunc) {
	// Start config watcher to reload system providers and collections on file changes
	watcherCtx, watcherCancel := context.WithCancel(context.Background())
	doneCh := make(chan struct{})

	configWatcher := NewWatcher(logger, validate, storage, configDir)
	go func() {
		defer close(doneCh)
		if err := configWatcher.Watch(watcherCtx); err != nil {
			logger.Error("Config watcher failed", "error", err.Error())
		}
	}()

	return doneCh, watcherCancel
}
