package handlers

import (
	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/config"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/go-playground/validator/v10"
)

// Contains the service state information that handlers can access
type Handlers struct {
	storage         abstractions.Storage
	validate        *validator.Validate
	runtime         abstractions.Runtime
	mlflowClient    interface{} // TODO: replace this placeholder with the actual mlflow client
	providerConfigs map[string]api.ProviderResource
	serviceConfig   *config.Config
}

func New(storage abstractions.Storage, validate *validator.Validate, runtime abstractions.Runtime, mlflowClient interface{}, providerConfigs map[string]api.ProviderResource, serviceConfig *config.Config) *Handlers {
	return &Handlers{
		storage:         storage,
		validate:        validate,
		runtime:         runtime,
		mlflowClient:    mlflowClient,
		providerConfigs: providerConfigs,
		serviceConfig:   serviceConfig,
	}
}
