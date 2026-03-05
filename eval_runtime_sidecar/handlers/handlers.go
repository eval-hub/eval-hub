package handlers

import (
	"github.com/eval-hub/eval-hub/eval_runtime_sidecar/clients"
	"github.com/eval-hub/eval-hub/internal/config"
)

// Contains the service state information that handlers can access
type Handlers struct {
	serviceConfig *config.Config
	evalHubClient *clients.EvalHubClient
}

func New(serviceConfig *config.Config, evalHubClient *clients.EvalHubClient) *Handlers {
	return &Handlers{
		serviceConfig: serviceConfig,
		evalHubClient: evalHubClient,
	}
}
