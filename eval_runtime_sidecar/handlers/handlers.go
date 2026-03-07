package handlers

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/eval-hub/eval-hub/eval_runtime_sidecar/proxies/eval_hub"
	"github.com/eval-hub/eval-hub/eval_runtime_sidecar/proxies/mlflow"
	"github.com/eval-hub/eval-hub/internal/config"
)

// Handlers holds service state for HTTP handlers.
// Having separate HTTP clients for eval-hub and mlflow since we might want to disable TLS for one but not the other etc..
type Handlers struct {
	serviceConfig     *config.Config
	evalHubHTTPClient *http.Client
	mlflowHTTPClient  *http.Client
}

func New(serviceConfig *config.Config, logger *slog.Logger) (*Handlers, error) {
	evalHubHTTPClient, err := eval_hub.NewHTTPClient(serviceConfig, serviceConfig.IsOTELEnabled(), logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create eval-hub HTTP client: %w", err)
	}
	mlflowHTTPClient, err := mlflow.NewHTTPClient(serviceConfig, serviceConfig.IsOTELEnabled(), logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create mlflow HTTP client: %w", err)
	}
	return &Handlers{
		serviceConfig:     serviceConfig,
		evalHubHTTPClient: evalHubHTTPClient,
		mlflowHTTPClient:  mlflowHTTPClient,
	}, nil
}

// HandleEvalHubProxy forwards the request to the eval-hub service.
func (h *Handlers) HandleEvalHubProxy(w http.ResponseWriter, r *http.Request) {
	var cfg *config.EvalHubClientConfig
	if h.serviceConfig != nil && h.serviceConfig.Sidecar != nil {
		cfg = h.serviceConfig.Sidecar.EvalHub
	}
	eval_hub.Proxy(w, r, h.evalHubHTTPClient, cfg)
}

// HandleMLflowProxy forwards the request to the MLflow service.
func (h *Handlers) HandleMLflowProxy(w http.ResponseWriter, r *http.Request) {
	var cfg *config.MLFlowConfig
	if h.serviceConfig != nil {
		cfg = h.serviceConfig.MLFlow
	}
	mlflow.Proxy(w, r, h.mlflowHTTPClient, cfg)
}
