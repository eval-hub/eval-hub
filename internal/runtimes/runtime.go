package runtimes

import (
	"log/slog"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/config"
	"github.com/eval-hub/eval-hub/internal/runtimes/k8s"
	"github.com/eval-hub/eval-hub/internal/runtimes/local"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func NewRuntime(
	logger *slog.Logger,
	serviceConfig *config.Config,
	providerConfigs map[string]api.ProviderResource,
	collectionConfigs map[string]api.CollectionResource,
) (abstractions.Runtime, error) {
	if serviceConfig.Service.LocalMode {
		return local.NewLocalRuntime(logger, providerConfigs, collectionConfigs)
	}
	return k8s.NewK8sRuntime(logger, serviceConfig, providerConfigs, collectionConfigs)
}
