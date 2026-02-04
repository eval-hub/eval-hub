package abstractions

import (
	"context"
	"log/slog"
	"time"

	"github.com/eval-hub/eval-hub/pkg/api"
)

type QueryResults[T any] struct {
	Items       []T
	TotalStored int
}

type Storage interface {
	WithLogger(logger *slog.Logger) Storage
	WithContext(ctx context.Context) Storage

	// This is used to identify the storage implementation in the logs and error messages
	GetDatasourceName() string

	Ping(timeout time.Duration) error

	// Evaluation job operations
	CreateEvaluationJob(evaluation *api.EvaluationJobConfig) (*api.EvaluationJobResource, error)
	GetEvaluationJob(id string) (*api.EvaluationJobResource, error)
	GetEvaluationJobs(limit int, offset int, statusFilter string) (*QueryResults[api.EvaluationJobResource], error)
	DeleteEvaluationJob(id string, hardDelete bool) error
	UpdateEvaluationJobStatus(id string, state *api.StatusEvent) error

	// Collection operations
	CreateCollection(collection *api.CollectionResource) error
	GetCollection(id string, summary bool) (*api.CollectionResource, error)
	GetCollections(limit int, offset int) (*QueryResults[api.CollectionResource], error)
	UpdateCollection(collection *api.CollectionResource) error
	DeleteCollection(id string) error

	// Close the storage connection
	Close() error
}

// This interface must be decoupled from the service HTTP layer.
// Do not pass ExecutionContext, Request or Response wrappers either.
