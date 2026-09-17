package proxy

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/runtimes/shared"
)

// ErrInvalidAuthSecretPath indicates that auth_secret_mount_path is present
// but does not use the required file:/// URI scheme.
var ErrInvalidAuthSecretPath = errors.New("invalid auth_secret_mount_path")

const (
	DefaultJobCacheTTL           = 2 * time.Hour
	DefaultJobCacheSweepInterval = 90 * time.Minute
	DefaultJobsDir               = "/tmp/evalhub-jobs"
)

type jobCacheEntry struct {
	target     *url.URL
	httpClient *http.Client
	cleanAfter time.Time
}

// JobInfoCache maps job-id to resolved proxy state with a per-entry TTL.
// Entries are lazy-loaded from the filesystem on first access and refreshed
// after the TTL expires.
type JobInfoCache struct {
	mu      sync.RWMutex
	entries map[string]*jobCacheEntry
	ttl     time.Duration
	jobsDir string
	now     func() time.Time
	logger  *slog.Logger
}

// NewJobInfoCache creates a job info cache that reads sidecar-job-info.json
// files from jobsDir (defaults to /tmp/evalhub-jobs).
func NewJobInfoCache(jobsDir string, ttl time.Duration, logger *slog.Logger) *JobInfoCache {
	if jobsDir == "" {
		jobsDir = DefaultJobsDir
	}
	if ttl <= 0 {
		ttl = DefaultJobCacheTTL
	}
	return &JobInfoCache{
		entries: make(map[string]*jobCacheEntry),
		ttl:     ttl,
		jobsDir: jobsDir,
		now:     time.Now,
		logger:  logger,
	}
}

// Get returns the parsed target URL and per-job HTTP client for the given job-id.
// Loads from the filesystem on cache miss. Cached entries are served
// indefinitely; cleanAfter is only a hint for future passive sweep.
func (c *JobInfoCache) Get(jobID string) (*url.URL, *http.Client, error) {
	c.mu.RLock()
	if entry, ok := c.entries[jobID]; ok {
		c.mu.RUnlock()
		return entry.target, entry.httpClient, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if entry, ok := c.entries[jobID]; ok {
		return entry.target, entry.httpClient, nil
	}

	entry, err := c.loadEntry(jobID)
	if err != nil {
		return nil, nil, err
	}
	entry.cleanAfter = c.now().Add(c.ttl)
	c.entries[jobID] = entry
	c.logger.Info("Loaded job info into cache", "job_id", jobID, "target", entry.target.String())
	return entry.target, entry.httpClient, nil
}

func (c *JobInfoCache) loadEntry(jobID string) (*jobCacheEntry, error) {
	if strings.ContainsAny(jobID, "/\\") {
		return nil, fmt.Errorf("invalid job-id %q", jobID)
	}

	relPath := filepath.Join(jobID, shared.SidecarJobInfoFileName)
	f, err := os.OpenInRoot(c.jobsDir, relPath)
	if err != nil {
		return nil, fmt.Errorf("job info not found for %q: %w", jobID, err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("job info not found for %q: %w", jobID, err)
	}

	var info shared.SidecarJobInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("invalid job info JSON for %q: %w", jobID, err)
	}
	if info.Model == nil || strings.TrimSpace(info.Model.URL) == "" {
		return nil, fmt.Errorf("job info for %q is missing model URL", jobID)
	}

	target, err := url.Parse(strings.TrimSuffix(strings.TrimSpace(info.Model.URL), "/"))
	if err != nil || !target.IsAbs() || target.Host == "" {
		return nil, fmt.Errorf("invalid model URL %q in job info for %q", info.Model.URL, jobID)
	}

	timeout := DefaultHTTPTimeout
	if info.Model.HTTPTimeout > 0 {
		timeout = info.Model.HTTPTimeout
	}

	caCertPath := ""
	if mountPath := strings.TrimSpace(info.Model.AuthSecretMountPath); mountPath != "" {
		fsPath, ok := strings.CutPrefix(mountPath, "file://")
		if !ok {
			c.logger.Error("auth_secret_mount_path missing file:/// prefix",
				"job_id", jobID, "auth_secret_mount_path", mountPath)
			return nil, fmt.Errorf("%w: missing file:/// prefix in %q for job %q",
				ErrInvalidAuthSecretPath, mountPath, jobID)
		}
		if !strings.HasPrefix(fsPath, "/") {
			c.logger.Error("auth_secret_mount_path has non-local authority component",
				"job_id", jobID, "auth_secret_mount_path", mountPath)
			return nil, fmt.Errorf("%w: file:// URI must use an empty authority (file:///…), got %q for job %q",
				ErrInvalidAuthSecretPath, mountPath, jobID)
		}
		candidate := filepath.Join(fsPath, "ca_cert")
		if _, statErr := os.Stat(candidate); statErr == nil {
			caCertPath = candidate
		}
	}

	label := "Job-" + jobID
	var tlsCfg *tls.Config
	if schemeRequiresTLS(info.Model.URL) {
		var tlsErr error
		tlsCfg, tlsErr = buildTLSConfig(caCertPath, info.Model.InsecureSkipVerify, c.logger, label)
		if tlsErr != nil {
			return nil, fmt.Errorf("TLS config for job %q: %w", jobID, tlsErr)
		}
	}
	httpClient := newHTTPClient(timeout, tlsCfg, false, c.logger, label)

	return &jobCacheEntry{
		target:     target,
		httpClient: httpClient,
	}, nil
}

// StartSweep launches a background goroutine that periodically removes expired
// cache entries. It returns immediately. The goroutine exits when ctx is cancelled.
func (c *JobInfoCache) StartSweep(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultJobCacheSweepInterval
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.sweep()
			}
		}
	}()
}

func (c *JobInfoCache) sweep() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	for jobID, entry := range c.entries {
		if now.After(entry.cleanAfter) {
			delete(c.entries, jobID)
			c.logger.Info("Swept expired job cache entry", "job_id", jobID)
		}
	}
}
