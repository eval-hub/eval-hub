package handlers

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/eval-hub/eval-hub/eval_runtime_sidecar/proxies/eval_hub"
	"github.com/eval-hub/eval-hub/eval_runtime_sidecar/proxies/mlflow"
	"github.com/eval-hub/eval-hub/internal/config"
)

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
	evalHubHTTPClient, err := eval_hub.NewHTTPClient(config, config.IsOTELEnabled(), logger)
	if err != nil {
		logger.Error("failed to create eval-hub HTTP client", "error", err)
		return nil, fmt.Errorf("failed to create eval-hub HTTP client: %w", err)
	}
	evalHubBaseURL := os.Getenv("EVALHUB_URL")
	if evalHubBaseURL == "" {
		return nil, fmt.Errorf("EVALHUB_URL environment variable is not set")
	}
	mlflowHTTPClient, err := mlflow.NewHTTPClient(config, config.IsOTELEnabled(), logger)
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

// HandleEvalHubProxy forwards the request to the eval-hub service.
func (h *Handlers) HandleEvalHubProxy(w http.ResponseWriter, r *http.Request) {
	h.logger.Info("Handling eval-hub proxy request", "method", r.Method, "url", r.URL.Path)
	var cfg *config.EvalHubClientConfig
	if h.serviceConfig != nil && h.serviceConfig.Sidecar != nil {
		cfg = h.serviceConfig.Sidecar.EvalHub
	}
	eval_hub.Proxy(h.logger, w, r, h.evalHubHTTPClient, h.evalHubBaseURL, cfg)
}

// HandleMLflowProxy forwards the request to the MLflow service.
func (h *Handlers) HandleMLflowProxy(w http.ResponseWriter, r *http.Request) {
	var cfg *config.MLFlowConfig
	if h.serviceConfig != nil {
		cfg = h.serviceConfig.MLFlow
	}
	var tokenCacheTimeout time.Duration
	if h.serviceConfig != nil && h.serviceConfig.Sidecar != nil && h.serviceConfig.Sidecar.MLFlow != nil {
		tokenCacheTimeout = h.serviceConfig.Sidecar.MLFlow.TokenCacheTimeout
	}
	mlflow.Proxy(h.logger, w, r, h.mlflowHTTPClient, h.mlflowTrackingURI, cfg, tokenCacheTimeout)
}
