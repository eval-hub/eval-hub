package handlers

import (
	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/pkg/mlflowclient"
	"github.com/go-playground/validator/v10"
)

// Contains the service state information that handlers can access
type Handlers struct {
	storage       abstractions.Storage
	validate      *validator.Validate
	runtime       abstractions.Runtime
	mlflowClient  *mlflowclient.Client
	serviceConfig *config.Config
}

func New(storage abstractions.Storage, validate *validator.Validate, runtime abstractions.Runtime, mlflowClient *mlflowclient.Client, serviceConfig *config.Config) *Handlers {
	return &Handlers{
		storage:       storage,
		validate:      validate,
		runtime:       runtime,
		mlflowClient:  mlflowClient,
		serviceConfig: serviceConfig,
	}
}
