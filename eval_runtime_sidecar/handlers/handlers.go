package handlers

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/eval-hub/eval-hub/eval_runtime_sidecar/proxy"
	"github.com/eval-hub/eval-hub/internal/config"
)

const ServiceAccountTokenPathDefault = "/var/run/secrets/kubernetes.io/serviceaccount/token"
const MLFlowTokenPathDefault = "/var/run/secrets/mlflow/token"

// Handlers holds service state for HTTP handlers.
// Having separate HTTP clients for eval-hub and mlflow since we might want to disable TLS for one but not the other etc..
type Handlers struct {
	logger            *slog.Logger
	serviceConfig     *config.Config
	evalHubBaseURL    string
	evalHubHTTPClient *http.Client
	mlflowTrackingURI string
	mlflowHTTPClient  *http.Client
}

func New(config *config.Config, logger *slog.Logger) (*Handlers, error) {
	evalHubHTTPClient, err := proxy.NewEvalHubHTTPClient(config, config.IsOTELEnabled(), logger)
	if err != nil {
		logger.Error("failed to create eval-hub HTTP client", "error", err)
		return nil, fmt.Errorf("failed to create eval-hub HTTP client: %w", err)
	}
	evalHubBaseURL := os.Getenv("EVALHUB_URL")
	if evalHubBaseURL == "" {
		return nil, fmt.Errorf("EVALHUB_URL environment variable is not set")
	}
	mlflowHTTPClient, err := proxy.NewMLFlowHTTPClient(config, config.IsOTELEnabled(), logger)
	if err != nil {
		logger.Error("failed to create mlflow HTTP client", "error", err)
		return nil, fmt.Errorf("failed to create mlflow HTTP client: %w", err)
	}
	mlflowTrackingURI := os.Getenv("MLFLOW_TRACKING_URI")
	if mlflowTrackingURI == "" && config.MLFlow != nil {
		mlflowTrackingURI = strings.TrimSpace(strings.TrimSuffix(config.MLFlow.TrackingURI, "/"))
	}
	if mlflowTrackingURI == "" {
		logger.Warn("mlflow.tracking_uri not set")
		//return nil, fmt.Errorf("mlflow.tracking_uri not set")
	}
	return &Handlers{
		logger:            logger,
		serviceConfig:     config,
		evalHubBaseURL:    evalHubBaseURL,
		evalHubHTTPClient: evalHubHTTPClient,
		mlflowTrackingURI: mlflowTrackingURI,
		mlflowHTTPClient:  mlflowHTTPClient,
	}, nil
}

func (h *Handlers) HandleProxyCall(w http.ResponseWriter, r *http.Request) {
	targetBaseURL, tokenParams, httpClient, err := h.parseProxyCall(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	proxy.ProxyRequest(h.logger, w, r, httpClient, targetBaseURL, *tokenParams)
}

func (h *Handlers) parseProxyCall(r *http.Request) (string, *proxy.AuthTokenInput, *http.Client, error) {
	switch {
	case strings.HasPrefix(r.RequestURI, "/api/v1/evaluations/"):
		ehClientConfig := h.serviceConfig.Sidecar.EvalHub
		if ehClientConfig != nil {
			return h.evalHubBaseURL, &proxy.AuthTokenInput{
				TargetEndpoint:    "eval-hub",
				AuthTokenPath:     ServiceAccountTokenPathDefault,
				AuthToken:         ehClientConfig.Token,
				TokenCacheTimeout: ehClientConfig.TokenCacheTimeout,
			}, h.evalHubHTTPClient, nil
		}
		return "", nil, nil, fmt.Errorf("eval-hub proxy is not configured")
	case strings.HasPrefix(r.RequestURI, "/api/2.0/mlflow/"):
		mlflowClientConfig := h.serviceConfig.MLFlow
		if mlflowClientConfig != nil {
			return h.mlflowTrackingURI, &proxy.AuthTokenInput{
				TargetEndpoint: "mlflow",
				AuthTokenPath:  MLFlowTokenPathDefault,
			}, h.mlflowHTTPClient, nil
		}
		return "", nil, nil, fmt.Errorf("mlflow proxy is not configured")
	default:
		return "", nil, nil, fmt.Errorf("unknown proxy call: %s", r.RequestURI)
	}
}
