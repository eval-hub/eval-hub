package eval_hub

import (
	"log/slog"
	"net/http"

	"github.com/eval-hub/eval-hub/eval_runtime_sidecar/proxies/common"
	"github.com/eval-hub/eval-hub/internal/config"
)

// This is the fixed path where kubernetes projects the service account token.
const ServiceAccountTokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"

// Proxy forwards the request to the eval-hub service using client and config.
// Responds with 503 if client is nil, cfg is nil, or base_url is not set.
func Proxy(logger *slog.Logger, w http.ResponseWriter, r *http.Request, client *http.Client, baseURL string, cfg *config.EvalHubClientConfig) {
	if client == nil {
		http.Error(w, "eval-hub proxy is not configured", http.StatusServiceUnavailable)
		return
	}
	if cfg == nil {
		http.Error(w, "eval_hub is not configured", http.StatusServiceUnavailable)
		return
	}

	common.ProxyRequest(logger, w, r, client, baseURL, common.AuthTokenInput{
		TargetEndpoint:    "eval-hub",
		AuthTokenPath:     ServiceAccountTokenPath,
		AuthToken:         cfg.Token,
		TokenCacheTimeout: cfg.TokenCacheTimeout,
	})
}
