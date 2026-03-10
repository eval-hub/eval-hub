package mlflow

import (
	"log/slog"
	"net/http"

	"github.com/eval-hub/eval-hub/eval_runtime_sidecar/proxies/common"
	"github.com/eval-hub/eval-hub/internal/config"
)

// Proxy forwards the request to the MLflow service using client and config.
// Responds with 503 if client is nil, cfg is nil, or tracking_uri is not set.
func Proxy(logger *slog.Logger, w http.ResponseWriter, r *http.Request, client *http.Client, trackingURI string, cfg *config.MLFlowConfig) {
	if client == nil {
		http.Error(w, "mlflow proxy not configured", http.StatusServiceUnavailable)
		return
	}
	if cfg == nil {
		http.Error(w, "mlflow not configured", http.StatusServiceUnavailable)
		return
	}
	tokenPath := cfg.TokenPath
	if tokenPath == "" {
		tokenPath = common.DefaultTokenPath
	}
	token := ResolveAuthToken(tokenPath, cfg.Token)
	common.ProxyRequest(logger, w, r, client, trackingURI, token)
}
