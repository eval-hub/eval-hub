package eval_hub

import (
	"net/http"
	"strings"

	"github.com/eval-hub/eval-hub/eval_runtime_sidecar/proxies/common"
	"github.com/eval-hub/eval-hub/internal/config"
)

const defaultTokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"

// Proxy forwards the request to the eval-hub service using client and config.
// Responds with 503 if client is nil, cfg is nil, or base_url is not set.
func Proxy(w http.ResponseWriter, r *http.Request, client *http.Client, cfg *config.EvalHubClientConfig) {
	if client == nil {
		http.Error(w, "eval-hub proxy not configured", http.StatusServiceUnavailable)
		return
	}
	if cfg == nil {
		http.Error(w, "eval_hub not configured", http.StatusServiceUnavailable)
		return
	}
	baseURL := strings.TrimSpace(strings.TrimSuffix(cfg.BaseURL, "/"))
	if baseURL == "" {
		http.Error(w, "eval_hub.base_url not set", http.StatusServiceUnavailable)
		return
	}
	tokenPath := cfg.TokenPath
	if tokenPath == "" {
		tokenPath = defaultTokenPath
	}
	token := ResolveAuthToken(tokenPath, cfg.Token)
	common.ProxyRequest(w, r, client, baseURL, token)
}
