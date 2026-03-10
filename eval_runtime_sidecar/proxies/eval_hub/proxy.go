package eval_hub

import (
	"log/slog"
	"net/http"

	"github.com/eval-hub/eval-hub/eval_runtime_sidecar/proxies/common"
	"github.com/eval-hub/eval-hub/internal/config"
)

// Proxy forwards the request to the eval-hub service using client and config.
// Responds with 503 if client is nil, cfg is nil, or base_url is not set.
func Proxy(logger *slog.Logger, w http.ResponseWriter, r *http.Request, client *http.Client, baseURL string, cfg *config.EvalHubClientConfig) {
	if client == nil {
		http.Error(w, "eval-hub proxy not configured", http.StatusServiceUnavailable)
		return
	}
	if cfg == nil {
		http.Error(w, "eval_hub not configured", http.StatusServiceUnavailable)
		return
	}

	tokenPath := cfg.TokenPath
	if tokenPath == "" {
		tokenPath = common.DefaultTokenPath
	}
	token := ResolveAuthToken(logger, tokenPath, cfg.Token)
	common.ProxyRequest(logger, w, r, client, baseURL, token)
}
