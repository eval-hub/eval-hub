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

// ServiceAccountTokenPathDefault is the in-cluster path for the k8s SA token (Eval Hub API auth).
const ServiceAccountTokenPathDefault = "/var/run/secrets/kubernetes.io/serviceaccount/token"

// newEvalhubProxy builds a reverse proxy to eval_hub.base_url (Eval Hub REST API, e.g. /api/v1/evaluations/ for job callbacks to the hub).
func newEvalhubProxy(config *config.Config, logger *slog.Logger) (*httputil.ReverseProxy, error) {
	evalHubHTTPClient, err := proxy.NewEvalHubHTTPClient(config, config.IsOTELEnabled(), logger)
	if err != nil {
		logger.Error("failed to create eval-hub HTTP client", "error", err)
		return nil, fmt.Errorf("failed to create eval-hub HTTP client: %w", err)
	}
	evalHubBaseURL := ""
	if config.Sidecar != nil && config.Sidecar.EvalHub != nil {
		evalHubBaseURL = strings.TrimSpace(config.Sidecar.EvalHub.BaseURL)
	}
	if evalHubBaseURL == "" {
		return nil, fmt.Errorf("eval_hub.base_url is not set in sidecar config")
	}
	evalHubTarget, err := url.Parse(strings.TrimSuffix(evalHubBaseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("invalid eval_hub.base_url: %w", err)
	}
	evalHubProxy := proxy.NewReverseProxy(evalHubTarget, evalHubHTTPClient, logger, nil)
	return evalHubProxy, nil
}
