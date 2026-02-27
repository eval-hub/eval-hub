package abstractions

import (
	"context"
	"log/slog"

	"github.com/eval-hub/eval-hub/pkg/api"
)

// Runtime interface defines the methods for running evaluation jobs. Concrete implemementation
// hold the specific aspects of various runtimes (i.e. K8s, local, etc.). No other places in the code should
// be pointing directly to K8s or other runtime specific details.

type Runtime interface {
	WithLogger(logger *slog.Logger) Runtime
	WithContext(ctx context.Context) Runtime
	Name() string
	RunEvaluationJob(evaluation *api.EvaluationJobResource, storage *Storage) error
	DeleteEvaluationJobResources(evaluation *api.EvaluationJobResource) error
}

// This intrerface must be decoupled from the service HTTP layer
