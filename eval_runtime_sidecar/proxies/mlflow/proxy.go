package mlflow

import (
	"net/http"
	"strings"

	"github.com/eval-hub/eval-hub/eval_runtime_sidecar/proxies/common"
	"github.com/eval-hub/eval-hub/internal/config"
)

const defaultTokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"

// Proxy forwards the request to the MLflow service using client and config.
// Responds with 503 if client is nil, cfg is nil, or tracking_uri is not set.
func Proxy(w http.ResponseWriter, r *http.Request, client *http.Client, cfg *config.MLFlowConfig) {
	if client == nil {
		http.Error(w, "mlflow proxy not configured", http.StatusServiceUnavailable)
		return
	}
	if cfg == nil {
		http.Error(w, "mlflow not configured", http.StatusServiceUnavailable)
		return
	}
	baseURL := strings.TrimSpace(strings.TrimSuffix(cfg.TrackingURI, "/"))
	if baseURL == "" {
		http.Error(w, "mlflow.tracking_uri not set", http.StatusServiceUnavailable)
		return
	}
	tokenPath := cfg.TokenPath
	if tokenPath == "" {
		tokenPath = defaultTokenPath
	}
	token := ResolveAuthToken(tokenPath, cfg.Token)
	common.ProxyRequest(w, r, client, baseURL, token)
}
