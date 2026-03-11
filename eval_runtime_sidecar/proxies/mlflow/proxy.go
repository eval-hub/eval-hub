package mlflow

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/eval-hub/eval-hub/eval_runtime_sidecar/proxies/common"
	"github.com/eval-hub/eval-hub/internal/config"
)

const MLFlowTokenPathDefault = "/var/run/secrets/mlflow/token"

// Proxy forwards the request to the MLflow service using client and config.
// tokenCacheTimeout is the TTL for the auth token cache (e.g. from sidecar.mlflow.token_cache_timeout); 0 uses default.
// Responds with 503 if client is nil, cfg is nil, or tracking_uri is not set.
func Proxy(logger *slog.Logger, w http.ResponseWriter, r *http.Request, client *http.Client, trackingURI string, cfg *config.MLFlowConfig, tokenCacheTimeout time.Duration) {
	if client == nil {
		http.Error(w, "mlflow proxy is not configured", http.StatusServiceUnavailable)
		return
	}

	if cfg == nil {
		http.Error(w, "mlflow is not configured", http.StatusServiceUnavailable)
		return
	}

	tokenPath := cfg.TokenPath
	if tokenPath == "" {
		tokenPath = MLFlowTokenPathDefault
	}

	common.ProxyRequest(logger, w, r, client, trackingURI, common.AuthTokenInput{
		TargetEndpoint:    "mlflow",
		AuthTokenPath:     tokenPath,
		AuthToken:         cfg.Token,
		TokenCacheTimeout: tokenCacheTimeout,
	})
}
