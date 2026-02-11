package local

import (
	"context"
	"log/slog"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/pkg/api"
)

type LocalRuntime struct {
	logger *slog.Logger
	ctx    context.Context
}

func NewLocalRuntime(logger *slog.Logger) (abstractions.Runtime, error) {
	return &LocalRuntime{logger: logger}, nil
}

func (r *LocalRuntime) WithLogger(logger *slog.Logger) abstractions.Runtime {
	return &LocalRuntime{
		logger: logger,
		ctx:    r.ctx,
	}
}

func (r *LocalRuntime) WithContext(ctx context.Context) abstractions.Runtime {
	return &LocalRuntime{
		logger: r.logger,
		ctx:    ctx,
	}
}

func (r *LocalRuntime) RunEvaluationJob(evaluation *api.EvaluationJobResource, storage *abstractions.Storage) error {
	return nil
}

func (r *LocalRuntime) DeleteEvaluationJobResources(evaluation *api.EvaluationJobResource) error {
	return nil
}

func (r *LocalRuntime) Name() string {
	return "local"
}
