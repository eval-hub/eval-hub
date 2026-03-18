package handlers

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/eval-hub/eval-hub/eval_runtime_sidecar/proxy"
	"github.com/eval-hub/eval-hub/internal/config"
)

const ServiceAccountTokenPathDefault = "/var/run/secrets/kubernetes.io/serviceaccount/token"
const MLFlowTokenPathDefault = "/var/run/secrets/mlflow/token"

// Handlers holds service state for HTTP handlers.
// Reverse proxies are created once at startup and reused for all requests.
type Handlers struct {
	logger        *slog.Logger
	serviceConfig *config.Config
	evalHubProxy  *httputil.ReverseProxy
	mlflowProxy   *httputil.ReverseProxy
}

func New(config *config.Config, logger *slog.Logger) (*Handlers, error) {
	evalHubProxy, err := newEvalhubProxy(config, logger)
	if err != nil {
		return nil, err
	}

	mlflowProxy, err := newMlflowProxy(config, logger)
	if err != nil {
		return nil, err
	}

	return &Handlers{
		logger:        logger,
		serviceConfig: config,
		evalHubProxy:  evalHubProxy,
		mlflowProxy:   mlflowProxy,
	}, nil
}

func newMlflowProxy(config *config.Config, logger *slog.Logger) (*httputil.ReverseProxy, error) {
	mlflowTrackingURI := ""
	if config.MLFlow != nil {
		mlflowTrackingURI = strings.TrimSpace(config.MLFlow.TrackingURI)
	}
	if mlflowTrackingURI == "" {
		logger.Warn("mlflow.tracking_uri is not set in sidecar config")
		return nil, nil
	}
	mlflowHTTPClient, err := proxy.NewMLFlowHTTPClient(config, config.IsOTELEnabled(), logger)
	if err != nil {
		logger.Error("failed to create mlflow HTTP client", "error", err)
		return nil, fmt.Errorf("failed to create mlflow HTTP client: %w", err)
	}
	mlflowTarget, err := url.Parse(strings.TrimSuffix(mlflowTrackingURI, "/"))
	if err != nil {
		return nil, fmt.Errorf("invalid mlflow.tracking_uri: %w", err)
	}
	return proxy.NewReverseProxy(mlflowTarget, mlflowHTTPClient, logger), nil
}

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
		return nil, fmt.Errorf("invalid EVALHUB_URL: %w", err)
	}
	evalHubProxy := proxy.NewReverseProxy(evalHubTarget, evalHubHTTPClient, logger)
	return evalHubProxy, nil
}

func (h *Handlers) HandleProxyCall(w http.ResponseWriter, r *http.Request) {
	proxyHandler, tokenParams, err := h.parseProxyCall(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	r = r.WithContext(proxy.ContextWithAuthInput(r.Context(), *tokenParams))
	proxyHandler.ServeHTTP(w, r)
}

func (h *Handlers) parseProxyCall(r *http.Request) (*httputil.ReverseProxy, *proxy.AuthTokenInput, error) {
	switch {
	case strings.HasPrefix(r.RequestURI, "/api/v1/evaluations/"):
		ehClientConfig := h.serviceConfig.Sidecar.EvalHub
		if ehClientConfig != nil {
			return h.evalHubProxy, &proxy.AuthTokenInput{
				TargetEndpoint:    "eval-hub",
				AuthTokenPath:     ServiceAccountTokenPathDefault,
				AuthToken:         ehClientConfig.Token,
				TokenCacheTimeout: ehClientConfig.TokenCacheTimeout,
			}, nil
		}
		return nil, nil, fmt.Errorf("eval-hub proxy is not configured")

	case strings.Contains(r.RequestURI, "/mlflow/"):
		if h.serviceConfig.MLFlow != nil && strings.TrimSpace(h.serviceConfig.MLFlow.TrackingURI) != "" && h.mlflowProxy != nil {
			tokenPath := MLFlowTokenPathDefault
			if h.serviceConfig.Sidecar != nil && h.serviceConfig.Sidecar.MLFlow != nil {
				if p := strings.TrimSpace(h.serviceConfig.Sidecar.MLFlow.TokenPath); p != "" {
					tokenPath = p
				}
			}
			return h.mlflowProxy, &proxy.AuthTokenInput{
				TargetEndpoint: "mlflow",
				AuthTokenPath:  tokenPath,
			}, nil
		}
		return nil, nil, fmt.Errorf("mlflow proxy is not configured")
	default:
		return nil, nil, fmt.Errorf("unknown proxy call: %s", r.RequestURI)
	}
}
