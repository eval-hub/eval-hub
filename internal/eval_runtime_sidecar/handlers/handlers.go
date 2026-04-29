package handlers

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_runtime_sidecar/proxy"
)

// Handlers holds service state for HTTP handlers.
// Reverse proxies are created once at startup and reused for all requests.
type Handlers struct {
	logger           *slog.Logger
	serviceConfig    *config.Config
	evalHubProxy     *httputil.ReverseProxy
	mlflowProxy      *httputil.ReverseProxy
	ociProxy         *httputil.ReverseProxy
	ociTokenProducer *proxy.OCITokenProducer // created once at startup for OCI auth
	ociRepository    string                  // from job spec; used to route requests to /registry/{ociRepository}
}

// New creates handlers and builds reverse proxies for eval-hub, MLflow, and optionally OCI.
func New(config *config.Config, logger *slog.Logger) (*Handlers, error) {
	evalHubProxy, err := newEvalhubProxy(config, logger)
	if err != nil {
		return nil, err
	}

	mlflowProxy, err := newMlflowProxy(config, logger)
	if err != nil {
		return nil, err
	}

	ociProxy, ociTokenProducer, ociRepository, err := newOciProxy(config, logger)
	if err != nil {
		return nil, err
	}

	return &Handlers{
		logger:           logger,
		serviceConfig:    config,
		evalHubProxy:     evalHubProxy,
		mlflowProxy:      mlflowProxy,
		ociProxy:         ociProxy,
		ociTokenProducer: ociTokenProducer,
		ociRepository:    ociRepository,
	}, nil
}

// HandleHealth responds OK for liveness.
func (h *Handlers) HandleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// HandleProxyCall routes the request to the correct reverse proxy (Eval Hub API, MLflow, or OCI).
func (h *Handlers) HandleProxyCall(w http.ResponseWriter, r *http.Request) {
	proxyHandler, tokenParams, err := h.parseProxyCall(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ctx := proxy.ContextWithAuthInput(r.Context(), *tokenParams)
	ctx = proxy.ContextWithOriginalRequest(ctx, r)
	r = r.WithContext(ctx)
	proxyHandler.ServeHTTP(w, r)
}

// requestPathForRouting returns the URL path only (no query or fragment) for proxy routing.
func requestPathForRouting(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		return uri
	}
	return u.EscapedPath()
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

	case isMLflowProxyPath(requestPathForRouting(r.RequestURI)):
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

	case h.ociRouteMatch(r.RequestURI):
		if h.ociProxy != nil {
			// Reuse the TokenProducer created at startup; token cache and refresh in resolveOCIAuthToken.
			return h.ociProxy, &proxy.AuthTokenInput{
				TargetEndpoint:   "oci",
				OCITokenProducer: h.ociTokenProducer,
				OCIRepository:    h.ociRepository,
			}, nil
		}
		return nil, nil, fmt.Errorf("oci proxy is not configured")
	default:
		return nil, nil, fmt.Errorf("unknown proxy call: %s", r.RequestURI)
	}
}
