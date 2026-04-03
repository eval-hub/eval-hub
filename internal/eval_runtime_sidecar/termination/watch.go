// Package termination watches for adapter-driven shutdown signals shared via the pod emptyDir
// (see internal/eval_hub/runtimes/k8s/job_builders.go: termination-file-volume emptyDir mounts).
package termination

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

const (
	// AdapterTerminationWatchDir is where the job pod mounts the shared termination emptyDir
	// on the sidecar. Must match adapterTerminationSharedMountPath in job_builders.go.
	AdapterTerminationWatchDir = "/shared"
	// AdapterTerminationSignalFile is created by the adapter on the shared volume to request sidecar shutdown.
	AdapterTerminationSignalFile = "terminated"
	defaultPollInterval          = 500 * time.Millisecond
)

// WatchAdapterTerminationSignal polls for a regular file named fileName inside dir.
// When the file appears, it sends once on the returned channel and stops watching.
// If ctx is cancelled, the goroutine exits without sending.
func WatchAdapterTerminationSignal(ctx context.Context, dir, fileName string, pollInterval time.Duration, logger *slog.Logger) <-chan struct{} {
	if pollInterval <= 0 {
		pollInterval = defaultPollInterval
	}
	ch := make(chan struct{}, 1)
	path := filepath.Join(dir, fileName)

	go func() {
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if fileExistsRegular(path) {
					if logger != nil {
						logger.Info("Adapter termination signal file detected; stopping watcher", "path", path)
					}
					select {
					case ch <- struct{}{}:
					default:
					}
					return
				}
			}
		}
	}()

	return ch
}

// WatchDefaultAdapterTermination uses AdapterTerminationWatchDir and AdapterTerminationSignalFile.
func WatchDefaultAdapterTermination(ctx context.Context, logger *slog.Logger) <-chan struct{} {
	return WatchAdapterTerminationSignal(ctx, AdapterTerminationWatchDir, AdapterTerminationSignalFile, defaultPollInterval, logger)
}

func fileExistsRegular(path string) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !fi.IsDir()
}
