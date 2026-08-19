package proxy

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/runtimes/shared"
)

func TestJobInfoCache_Get(t *testing.T) {
	t.Parallel()
	logger := slog.Default()

	t.Run("loads job info from filesystem", func(t *testing.T) {
		t.Parallel()
		jobsDir := t.TempDir()
		jobID := "job-abc-123"
		writeJobInfo(t, jobsDir, jobID, `{
			"model": {
				"url": "https://model.example.com/v1",
				"auth_secret_mount_path": "file:///home/user/creds"
			}
		}`)

		cache := NewJobInfoCache(jobsDir, DefaultJobCacheTTL, logger)
		target, _, err := cache.Get(jobID)
		if err != nil {
			t.Fatalf("Get() error: %v", err)
		}
		if target.Host != "model.example.com" {
			t.Errorf("target host = %q, want %q", target.Host, "model.example.com")
		}
	})

	t.Run("returns cached entry on second call", func(t *testing.T) {
		t.Parallel()
		jobsDir := t.TempDir()
		jobID := "job-cached"
		writeJobInfo(t, jobsDir, jobID, `{
			"model": {"url": "https://model.example.com:8443/v1"}
		}`)

		cache := NewJobInfoCache(jobsDir, DefaultJobCacheTTL, logger)
		target1, _, err := cache.Get(jobID)
		if err != nil {
			t.Fatalf("first Get() error: %v", err)
		}
		// Remove the file — second call should use cache
		_ = os.Remove(filepath.Join(jobsDir, jobID, shared.SidecarJobInfoFileName))

		target2, _, err := cache.Get(jobID)
		if err != nil {
			t.Fatalf("second Get() error: %v", err)
		}
		if target1 != target2 {
			t.Error("expected same pointer from cache")
		}
	})

	t.Run("serves cached entry after cleanAfter time", func(t *testing.T) {
		t.Parallel()
		jobsDir := t.TempDir()
		jobID := "job-permanent"
		writeJobInfo(t, jobsDir, jobID, `{
			"model": {"url": "https://model.example.com:8443/v1"}
		}`)

		now := time.Now()
		cache := NewJobInfoCache(jobsDir, 1*time.Second, logger)
		cache.now = func() time.Time { return now }

		target1, _, err := cache.Get(jobID)
		if err != nil {
			t.Fatalf("first Get() error: %v", err)
		}

		// Advance well past cleanAfter — entry should still be served
		cache.now = func() time.Time { return now.Add(1 * time.Hour) }

		target2, _, err := cache.Get(jobID)
		if err != nil {
			t.Fatalf("second Get() error: %v", err)
		}
		if target1 != target2 {
			t.Error("expected same pointer — cached entries are served indefinitely")
		}
	})

	t.Run("returns error for missing job", func(t *testing.T) {
		t.Parallel()
		cache := NewJobInfoCache(t.TempDir(), DefaultJobCacheTTL, logger)
		_, _, err := cache.Get("nonexistent-job")
		if err == nil {
			t.Fatal("expected error for missing job")
		}
	})

	t.Run("rejects job-id with path separator", func(t *testing.T) {
		t.Parallel()
		cache := NewJobInfoCache(t.TempDir(), DefaultJobCacheTTL, logger)
		_, _, err := cache.Get("../etc/passwd")
		if err == nil {
			t.Fatal("expected error for job-id with path traversal")
		}
	})

	t.Run("returns error for missing model URL", func(t *testing.T) {
		t.Parallel()
		jobsDir := t.TempDir()
		writeJobInfo(t, jobsDir, "no-url", `{"model": {"url": ""}}`)

		cache := NewJobInfoCache(jobsDir, DefaultJobCacheTTL, logger)
		_, _, err := cache.Get("no-url")
		if err == nil {
			t.Fatal("expected error for empty model URL")
		}
	})

	t.Run("returns error for invalid model URL", func(t *testing.T) {
		t.Parallel()
		jobsDir := t.TempDir()
		writeJobInfo(t, jobsDir, "bad-url", `{"model": {"url": "not-a-url"}}`)

		cache := NewJobInfoCache(jobsDir, DefaultJobCacheTTL, logger)
		_, _, err := cache.Get("bad-url")
		if err == nil {
			t.Fatal("expected error for non-absolute model URL")
		}
	})

	t.Run("returns error for nil model", func(t *testing.T) {
		t.Parallel()
		jobsDir := t.TempDir()
		writeJobInfo(t, jobsDir, "no-model", `{}`)

		cache := NewJobInfoCache(jobsDir, DefaultJobCacheTTL, logger)
		_, _, err := cache.Get("no-model")
		if err == nil {
			t.Fatal("expected error for missing model section")
		}
	})

	t.Run("returns error for invalid JSON", func(t *testing.T) {
		t.Parallel()
		jobsDir := t.TempDir()
		writeJobInfo(t, jobsDir, "bad-json", `not json`)

		cache := NewJobInfoCache(jobsDir, DefaultJobCacheTTL, logger)
		_, _, err := cache.Get("bad-json")
		if err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})

	t.Run("loads entry successfully with auth_secret_mount_path", func(t *testing.T) {
		t.Parallel()
		jobsDir := t.TempDir()
		authDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(authDir, "api-key"), []byte("sk-test-key"), 0o600); err != nil {
			t.Fatal(err)
		}
		writeJobInfo(t, jobsDir, "job-with-creds", `{
			"model": {
				"url": "https://api.example.com:443/v1",
				"auth_secret_mount_path": "file://`+authDir+`"
			}
		}`)

		cache := NewJobInfoCache(jobsDir, DefaultJobCacheTTL, logger)
		target, client, err := cache.Get("job-with-creds")
		if err != nil {
			t.Fatalf("Get() error: %v", err)
		}
		if target.Host != "api.example.com:443" {
			t.Errorf("target host = %q, want %q", target.Host, "api.example.com:443")
		}
		if client == nil {
			t.Error("expected non-nil HTTP client")
		}
	})

	t.Run("returns error when auth_secret_mount_path missing file prefix", func(t *testing.T) {
		t.Parallel()
		jobsDir := t.TempDir()
		writeJobInfo(t, jobsDir, "job-bad-auth", `{
			"model": {
				"url": "https://api.example.com/v1",
				"auth_secret_mount_path": "/home/user/model-auth"
			}
		}`)

		cache := NewJobInfoCache(jobsDir, DefaultJobCacheTTL, logger)
		_, _, err := cache.Get("job-bad-auth")
		if err == nil {
			t.Fatal("expected error for auth_secret_mount_path without file:/// prefix")
		}
		if !errors.Is(err, ErrInvalidAuthSecretPath) {
			t.Errorf("expected ErrInvalidAuthSecretPath, got: %v", err)
		}
	})

	t.Run("returns error when auth_secret_mount_path uses non-local file URI authority", func(t *testing.T) {
		t.Parallel()
		jobsDir := t.TempDir()
		writeJobInfo(t, jobsDir, "job-file-authority", `{
			"model": {
				"url": "https://api.example.com/v1",
				"auth_secret_mount_path": "file://host/path"
			}
		}`)

		cache := NewJobInfoCache(jobsDir, DefaultJobCacheTTL, logger)
		_, _, err := cache.Get("job-file-authority")
		if err == nil {
			t.Fatal("expected error for file:// URI with authority component")
		}
		if !errors.Is(err, ErrInvalidAuthSecretPath) {
			t.Errorf("expected ErrInvalidAuthSecretPath, got: %v", err)
		}
	})

	t.Run("accepts empty auth_secret_mount_path", func(t *testing.T) {
		t.Parallel()
		jobsDir := t.TempDir()
		writeJobInfo(t, jobsDir, "job-no-auth", `{
			"model": {
				"url": "https://api.example.com/v1",
				"auth_secret_mount_path": ""
			}
		}`)

		cache := NewJobInfoCache(jobsDir, DefaultJobCacheTTL, logger)
		_, client, err := cache.Get("job-no-auth")
		if err != nil {
			t.Fatalf("Get() error: %v", err)
		}
		if client == nil {
			t.Error("expected non-nil HTTP client")
		}
	})

	t.Run("caches HTTP client with job entry", func(t *testing.T) {
		t.Parallel()
		jobsDir := t.TempDir()
		writeJobInfo(t, jobsDir, "job-client", `{
			"model": {"url": "https://model.example.com/v1"}
		}`)

		cache := NewJobInfoCache(jobsDir, DefaultJobCacheTTL, logger)
		_, client1, err := cache.Get("job-client")
		if err != nil {
			t.Fatalf("first Get() error: %v", err)
		}
		if client1 == nil {
			t.Fatal("expected non-nil HTTP client")
		}

		_, client2, err := cache.Get("job-client")
		if err != nil {
			t.Fatalf("second Get() error: %v", err)
		}
		if client1 != client2 {
			t.Error("expected same client pointer from cache")
		}
	})

	t.Run("uses custom HTTP timeout from model config", func(t *testing.T) {
		t.Parallel()
		jobsDir := t.TempDir()
		writeJobInfo(t, jobsDir, "job-timeout", `{
			"model": {
				"url": "http://model.example.com/v1",
				"http_timeout": 120000000000
			}
		}`)

		cache := NewJobInfoCache(jobsDir, DefaultJobCacheTTL, logger)
		_, client, err := cache.Get("job-timeout")
		if err != nil {
			t.Fatalf("Get() error: %v", err)
		}
		if client.Timeout != 120*time.Second {
			t.Errorf("timeout = %v, want 2m", client.Timeout)
		}
	})

	t.Run("rejects symlinked job directory escaping jobsDir", func(t *testing.T) {
		t.Parallel()
		jobsDir := t.TempDir()
		outsideDir := t.TempDir()

		writeJobInfo(t, outsideDir, "real-job", `{
			"model": {"url": "https://model.example.com/v1"}
		}`)

		// Create a symlink inside jobsDir that points to outsideDir/real-job
		if err := os.Symlink(
			filepath.Join(outsideDir, "real-job"),
			filepath.Join(jobsDir, "symlink-job"),
		); err != nil {
			t.Skipf("symlinks not supported: %v", err)
		}

		cache := NewJobInfoCache(jobsDir, DefaultJobCacheTTL, logger)
		_, _, err := cache.Get("symlink-job")
		if err == nil {
			t.Fatal("expected error when job directory is a symlink escaping jobsDir")
		}
	})

	t.Run("rejects symlinked job-info file escaping jobsDir", func(t *testing.T) {
		t.Parallel()
		jobsDir := t.TempDir()
		outsideDir := t.TempDir()

		// Write a valid job-info file outside jobsDir
		outsideFile := filepath.Join(outsideDir, shared.SidecarJobInfoFileName)
		if err := os.WriteFile(outsideFile, []byte(`{
			"model": {"url": "https://model.example.com/v1"}
		}`), 0o600); err != nil {
			t.Fatal(err)
		}

		// Create the job directory inside jobsDir, then symlink the info file out
		jobDir := filepath.Join(jobsDir, "symlink-file-job")
		if err := os.MkdirAll(jobDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(
			outsideFile,
			filepath.Join(jobDir, shared.SidecarJobInfoFileName),
		); err != nil {
			t.Skipf("symlinks not supported: %v", err)
		}

		cache := NewJobInfoCache(jobsDir, DefaultJobCacheTTL, logger)
		_, _, err := cache.Get("symlink-file-job")
		if err == nil {
			t.Fatal("expected error when job-info file is a symlink escaping jobsDir")
		}
	})

	t.Run("strips trailing slash from model URL", func(t *testing.T) {
		t.Parallel()
		jobsDir := t.TempDir()
		writeJobInfo(t, jobsDir, "trailing-slash", `{
			"model": {"url": "https://model.example.com:8443/v1/"}
		}`)

		cache := NewJobInfoCache(jobsDir, DefaultJobCacheTTL, logger)
		target, _, err := cache.Get("trailing-slash")
		if err != nil {
			t.Fatalf("Get() error: %v", err)
		}
		if target.Path != "/v1" {
			t.Errorf("target path = %q, want /v1 (trailing slash stripped)", target.Path)
		}
	})
}

func TestNewJobInfoCache_Defaults(t *testing.T) {
	t.Parallel()
	logger := slog.Default()

	t.Run("uses default jobs dir when empty", func(t *testing.T) {
		t.Parallel()
		cache := NewJobInfoCache("", DefaultJobCacheTTL, logger)
		if cache.jobsDir != DefaultJobsDir {
			t.Errorf("jobsDir = %q, want %q", cache.jobsDir, DefaultJobsDir)
		}
	})

	t.Run("uses default TTL when zero", func(t *testing.T) {
		t.Parallel()
		cache := NewJobInfoCache("/tmp", 0, logger)
		if cache.ttl != DefaultJobCacheTTL {
			t.Errorf("ttl = %v, want %v", cache.ttl, DefaultJobCacheTTL)
		}
	})
}

func TestJobInfoCache_Sweep(t *testing.T) {
	t.Parallel()
	logger := slog.Default()

	t.Run("removes expired entries", func(t *testing.T) {
		t.Parallel()
		jobsDir := t.TempDir()
		writeJobInfo(t, jobsDir, "job-old", `{"model": {"url": "https://model.example.com:443/v1"}}`)
		writeJobInfo(t, jobsDir, "job-new", `{"model": {"url": "https://model.example.com:443/v1"}}`)

		now := time.Now()
		cache := NewJobInfoCache(jobsDir, 1*time.Hour, logger)
		cache.now = func() time.Time { return now }

		// Load both entries
		if _, _, err := cache.Get("job-old"); err != nil {
			t.Fatal(err)
		}

		// Advance 90 minutes, then load job-new (so job-old is expired, job-new is not)
		cache.now = func() time.Time { return now.Add(90 * time.Minute) }
		if _, _, err := cache.Get("job-new"); err != nil {
			t.Fatal(err)
		}

		// Advance to 2h+1m — job-old has cleanAfter at now+1h, job-new at now+90m+1h
		cache.now = func() time.Time { return now.Add(2*time.Hour + 1*time.Minute) }
		cache.sweep()

		cache.mu.Lock()
		_, oldExists := cache.entries["job-old"]
		_, newExists := cache.entries["job-new"]
		cache.mu.Unlock()

		if oldExists {
			t.Error("expected job-old to be swept")
		}
		if !newExists {
			t.Error("expected job-new to still be cached")
		}
	})

	t.Run("no-op when all entries are fresh", func(t *testing.T) {
		t.Parallel()
		jobsDir := t.TempDir()
		writeJobInfo(t, jobsDir, "job-fresh", `{"model": {"url": "https://model.example.com:443/v1"}}`)

		cache := NewJobInfoCache(jobsDir, DefaultJobCacheTTL, logger)
		if _, _, err := cache.Get("job-fresh"); err != nil {
			t.Fatal(err)
		}

		cache.sweep()

		cache.mu.Lock()
		count := len(cache.entries)
		cache.mu.Unlock()

		if count != 1 {
			t.Errorf("expected 1 entry, got %d", count)
		}
	})

	t.Run("stops when context is cancelled", func(t *testing.T) {
		t.Parallel()
		jobsDir := t.TempDir()
		cache := NewJobInfoCache(jobsDir, DefaultJobCacheTTL, logger)

		ctx, cancel := context.WithCancel(context.Background())
		cache.StartSweep(ctx, 10*time.Millisecond)
		cancel()

		// Give the goroutine time to exit
		time.Sleep(50 * time.Millisecond)
	})

	t.Run("swept entry reloads from filesystem on next Get", func(t *testing.T) {
		t.Parallel()
		jobsDir := t.TempDir()
		writeJobInfo(t, jobsDir, "job-reload", `{"model": {"url": "https://model.example.com:443/v1"}}`)

		now := time.Now()
		cache := NewJobInfoCache(jobsDir, 1*time.Hour, logger)
		cache.now = func() time.Time { return now }

		if _, _, err := cache.Get("job-reload"); err != nil {
			t.Fatal(err)
		}

		// Expire and sweep
		cache.now = func() time.Time { return now.Add(2 * time.Hour) }
		cache.sweep()

		cache.mu.Lock()
		_, exists := cache.entries["job-reload"]
		cache.mu.Unlock()
		if exists {
			t.Fatal("expected entry to be swept")
		}

		// Get should reload from filesystem
		target, _, err := cache.Get("job-reload")
		if err != nil {
			t.Fatalf("Get() after sweep: %v", err)
		}
		if target.Host != "model.example.com:443" {
			t.Errorf("target host = %q after reload", target.Host)
		}
	})
}

func writeJobInfo(t *testing.T, jobsDir, jobID, content string) {
	t.Helper()
	dir := filepath.Join(jobsDir, jobID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, shared.SidecarJobInfoFileName), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
