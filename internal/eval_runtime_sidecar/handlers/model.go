package handlers

import (
	"fmt"
	"log/slog"
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
// to real credentials, injects the SA token when no auth is present, and forwards to the
// configured target URL.
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

	rp := proxy.NewModelReverseProxy(target, modelHTTPClient, logger, secretMountPath, ServiceAccountTokenPathDefault)
	logger.Info("Model proxy enabled", "url", targetURL)
	return rp, nil
}
