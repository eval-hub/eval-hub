package eval_hub

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/eval-hub/eval-hub/eval_runtime_sidecar/proxies/common"
	"github.com/eval-hub/eval-hub/internal/config"
)

const defaultHTTPTimeout = 30 * time.Second

// NewHTTPClient creates an HTTP client for the eval-hub service from config.
// Returns (nil, nil) when Sidecar.EvalHub is not configured.
func NewHTTPClient(serviceConfig *config.Config, isOTELEnabled bool, logger *slog.Logger) (*http.Client, error) {
	if serviceConfig == nil || serviceConfig.Sidecar == nil || serviceConfig.Sidecar.EvalHub == nil {
		return nil, nil
	}
	cfg := serviceConfig.Sidecar.EvalHub

	timeout := cfg.HTTPTimeout
	if timeout == 0 {
		timeout = defaultHTTPTimeout
	}

	tlsConfig, err := common.BuildTLSConfig(cfg.CACertPath, cfg.InsecureSkipVerify, logger, "EvalHub")
	if err != nil {
		return nil, err
	}

	client := common.NewHTTPClient(timeout, tlsConfig, isOTELEnabled, logger, "EvalHub")
	return client, nil
}
