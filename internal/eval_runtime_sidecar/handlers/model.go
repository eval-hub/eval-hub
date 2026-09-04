package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_runtime_sidecar/proxy"
)

// newModelProxy creates a reverse proxy for model request forwarding when sidecar.model
// is configured. Returns (nil, nil) when no model URL is configured (standalone sidecar use).
// For eval-hub job pods, sidecar_config.json always contains a model section so this proxy
// is always active. The proxy resolves ref-token Authorization headers (e.g. "Bearer api-key:ref")
// to real credentials, injects the SA token for all other requests (replacing adapter
// placeholders such as "Bearer local"), and forwards explicit hardcoded tokens sent as
// "Bearer token:<secret>".
func newModelProxy(config *config.Config, logger *slog.Logger) (*httputil.ReverseProxy, error) {
	if config == nil || config.Sidecar == nil || config.Sidecar.Model == nil {
		return nil, nil
	}
	mc := config.Sidecar.Model
	targetURL := strings.TrimSpace(mc.URL)
	if targetURL == "" {
		return nil, nil
	}

	modelHTTPClient, err := proxy.NewModelHTTPClient(config, config.IsOTELEnabled(), logger)
	if err != nil {
		logger.Error("failed to create model HTTP client", "error", err)
		return nil, fmt.Errorf("failed to create model HTTP client: %w", err)
	}

	target, err := url.Parse(strings.TrimSuffix(targetURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("invalid model url %q: %w", targetURL, err)
	}

	secretMountPath := strings.TrimSpace(mc.AuthSecretMountPath)

	rp := proxy.NewModelReverseProxy(target, modelHTTPClient, logger, secretMountPath, ServiceAccountAuthFileDefault)
	logger.Info("Model proxy enabled", "url", targetURL)
	return rp, nil
}

// newLocalModelProxy creates a reverse proxy for local mode that routes /model/<job-id>/<path>
// requests to per-job upstream model URLs using a TTL cache of sidecar-job-info.json files.
// A background sweep goroutine is started and will exit when ctx is cancelled.
func newLocalModelProxy(ctx context.Context, localCfg *config.LocalConfig, logger *slog.Logger) http.Handler {
	ttl := proxy.DefaultJobCacheTTL
	sweepInterval := proxy.DefaultJobCacheSweepInterval
	if localCfg != nil {
		if localCfg.JobCacheEntryTTL.Duration > 0 {
			ttl = localCfg.JobCacheEntryTTL.Duration
		}
		if localCfg.JobCacheSweepInterval.Duration > 0 {
			sweepInterval = localCfg.JobCacheSweepInterval.Duration
		}
	}

	cache := proxy.NewJobInfoCache("", ttl, logger)
	cache.StartSweep(ctx, sweepInterval)
	logger.Info("Job cache sweep started", "sweep_interval", sweepInterval, "entry_ttl", ttl)
	return proxy.NewLocalModelReverseProxy(cache, logger)
}
