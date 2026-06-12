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

// ModelAuthSecretMountPathDefault is the default path for the model credentials
// secret mount in the sidecar container. Must match modelAuthRealMountPath in
// internal/eval_hub/runtimes/k8s/job_builders.go.
const ModelAuthSecretMountPathDefault = "/var/run/secrets/model"

// newModelProxy creates a reverse proxy for model credential injection when sidecar.model
// is configured. Returns (nil, nil) when model credential injection is not configured.
// The proxy replaces ref-token Authorization headers (e.g. "Bearer api-key:ref") with the
// real credential read from the secret mount, then forwards to the configured target URL.
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
	if secretMountPath == "" {
		secretMountPath = ModelAuthSecretMountPathDefault
	}

	rp := proxy.NewModelReverseProxy(target, modelHTTPClient, logger, secretMountPath)
	logger.Info("Model credential-injection proxy enabled", "url", targetURL)
	return rp, nil
}
