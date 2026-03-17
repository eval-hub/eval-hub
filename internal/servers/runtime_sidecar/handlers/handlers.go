package handlers

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"

	"github.com/eval-hub/eval-hub/internal/config"
	"github.com/eval-hub/eval-hub/internal/servers/runtime_sidecar/proxy"
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
	mlflowHTTPClient, err := proxy.NewMLFlowHTTPClient(config, config.IsOTELEnabled(), logger)
	if err != nil {
		logger.Error("failed to create mlflow HTTP client", "error", err)
		return nil, fmt.Errorf("failed to create mlflow HTTP client: %w", err)
	}
	mlflowTrackingURI := os.Getenv("MLFLOW_TRACKING_URI")
	if mlflowTrackingURI == "" {
		logger.Warn("MLFLOW_TRACKING_URI is not set")
	}
	mlflowTarget, err := url.Parse(strings.TrimSuffix(mlflowTrackingURI, "/"))
	if err != nil {
		return nil, fmt.Errorf("invalid MLFLOW_TRACKING_URI: %w", err)
	}

	mlflowProxy := proxy.NewReverseProxy(mlflowTarget, mlflowHTTPClient, logger)
	return mlflowProxy, nil
}

func newEvalhubProxy(config *config.Config, logger *slog.Logger) (*httputil.ReverseProxy, error) {
	evalHubHTTPClient, err := proxy.NewEvalHubHTTPClient(config, config.IsOTELEnabled(), logger)
	if err != nil {
		logger.Error("failed to create eval-hub HTTP client", "error", err)
		return nil, fmt.Errorf("failed to create eval-hub HTTP client: %w", err)
	}
	evalHubBaseURL := os.Getenv("EVALHUB_URL")
	if evalHubBaseURL == "" {
		return nil, fmt.Errorf("EVALHUB_URL environment is not set")
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
		mlflowClientConfig := h.serviceConfig.MLFlow
		if mlflowClientConfig != nil && h.mlflowProxy != nil {
			return h.mlflowProxy, &proxy.AuthTokenInput{
				TargetEndpoint: "mlflow",
				AuthTokenPath:  MLFlowTokenPathDefault,
			}, nil
		}
		return nil, nil, fmt.Errorf("mlflow proxy is not configured")
	default:
		return nil, nil, fmt.Errorf("unknown proxy call: %s", r.RequestURI)
	}
}
