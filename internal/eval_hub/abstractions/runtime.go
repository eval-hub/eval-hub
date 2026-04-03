package abstractions

import (
	"context"
	"log/slog"

	"github.com/eval-hub/eval-hub/pkg/api"
)

// Runtime interface defines the methods for running evaluation jobs. Concrete implementations
// hold the specific aspects of various runtimes (i.e. K8s, local, etc.). No other places in the code should
// be pointing directly to K8s or other runtime specific details.

// RuntimeStorage interface is used to update the evaluation job status and benchmarks
// and query providers. This is required because we might need these operations to be
// in a transaction and we don't want to give direct access to the storage layer because
// this will shortcut certain checks that are needed for the operation to be successful.
type RuntimeStorage interface {
	GetProvider(id string) (*api.ProviderResource, error)
	UpdateEvaluationJob(id string, runStatus *api.StatusEvent) error
}

type Runtime interface {
	WithLogger(logger *slog.Logger) Runtime
	WithContext(ctx context.Context) Runtime
	Name() string
	RunEvaluationJob(evaluation *api.EvaluationJobResource, benchmarks []api.EvaluationBenchmarkConfig, storage RuntimeStorage) error
	DeleteEvaluationJobResources(evaluation *api.EvaluationJobResource) error
}

// This interface must be decoupled from the service HTTP layer
