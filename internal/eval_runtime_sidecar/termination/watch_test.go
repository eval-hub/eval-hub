package termination

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatchAdapterTerminationSignalDetectsFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fileName := "terminated"
	path := filepath.Join(dir, fileName)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := WatchAdapterTerminationSignal(ctx, dir, fileName, 50*time.Millisecond, slog.Default())

	select {
	case <-ch:
		t.Fatal("should not signal before file exists")
	case <-time.After(120 * time.Millisecond):
	}

	if err := os.WriteFile(path, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}

	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for termination signal")
	}
}

func TestWatchAdapterTerminationSignalStopsOnCancel(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ch := WatchAdapterTerminationSignal(ctx, dir, "terminated", 50*time.Millisecond, nil)

	select {
	case <-ch:
		t.Fatal("should not signal when ctx already cancelled")
	case <-time.After(200 * time.Millisecond):
	}
}
