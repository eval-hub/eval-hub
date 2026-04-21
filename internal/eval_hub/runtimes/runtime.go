package runtimes

import (
	"log/slog"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_hub/runtimes/k8s"
	"github.com/eval-hub/eval-hub/internal/eval_hub/runtimes/local"
)

func NewRuntime(
	logger *slog.Logger,
	serviceConfig *config.Config,
) (abstractions.Runtime, error) {
	if serviceConfig.Service.LocalMode {
		return local.NewLocalRuntime(logger, serviceConfig)
	}
	return k8s.NewK8sRuntime(logger, serviceConfig)
}
