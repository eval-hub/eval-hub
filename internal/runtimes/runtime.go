package runtimes

import (
	"log/slog"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/config"
	"github.com/eval-hub/eval-hub/internal/runtimes/k8s"
	"github.com/eval-hub/eval-hub/internal/runtimes/local"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func NewRuntime(logger *slog.Logger, serviceConfig *config.Config, providerConfigs map[string]api.ProviderResource) (abstractions.Runtime, error) {
	var runtime abstractions.Runtime
	var err error

	if serviceConfig.Service.LocalMode {
		runtime, err = local.NewLocalRuntime(logger)
	} else {
		runtime, err = k8s.NewK8sRuntime(logger, providerConfigs)
	}

	return runtime, err
}
