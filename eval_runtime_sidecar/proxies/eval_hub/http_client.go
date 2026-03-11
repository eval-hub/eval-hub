package eval_hub

import (
	"crypto/tls"
	"log/slog"
	"net/http"
	"time"

	"github.com/eval-hub/eval-hub/eval_runtime_sidecar/proxies/common"
	"github.com/eval-hub/eval-hub/internal/config"
)

const defaultHTTPTimeout = 30 * time.Second

// NewHTTPClient creates an HTTP client for the eval-hub service from config.
// Returns (nil, nil) when Sidecar.EvalHub is not configured.
func NewHTTPClient(config *config.Config, isOTELEnabled bool, logger *slog.Logger) (*http.Client, error) {
	if config == nil || config.Sidecar == nil {
		return nil, nil
	}
	cfg := config.Sidecar.EvalHub

	timeout := defaultHTTPTimeout

	if cfg != nil && cfg.HTTPTimeout > 0 {
		timeout = cfg.HTTPTimeout
	}

	var tlsConfig *tls.Config
	var err error
	if cfg != nil {
		tlsConfig, err = common.BuildTLSConfig(cfg.CACertPath, cfg.InsecureSkipVerify, logger, "EvalHub")
		if err != nil {
			return nil, err
		}
	}

	client := common.NewHTTPClient(timeout, tlsConfig, isOTELEnabled, logger, "EvalHub")
	return client, nil
}
