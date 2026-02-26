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
	Errors      []string
}

type QueryFilter struct {
	Limit  int
	Offset int
	Params map[string]any
}

type Storage interface {
	WithLogger(logger *slog.Logger) Storage
	WithContext(ctx context.Context) Storage
	WithTenant(tenant api.Tenant) Storage

	Ping(timeout time.Duration) error

	// Evaluation job operations
	CreateEvaluationJob(evaluation *api.EvaluationJobResource) error
	GetEvaluationJob(id string) (*api.EvaluationJobResource, error)
	GetEvaluationJobs(filter QueryFilter) (*QueryResults[api.EvaluationJobResource], error)
	DeleteEvaluationJob(id string) error
	UpdateEvaluationJob(id string, runStatus *api.StatusEvent) error
	// UpdateEvaluationJobStatus is used to update the status of an evaluation job and is internal - do we need it here?
	UpdateEvaluationJobStatus(id string, state api.OverallState, message *api.MessageInfo) error

	// Collection operations
	CreateCollection(collection *api.CollectionResource) error
	GetCollection(id string) (*api.CollectionResource, error)
	GetCollections(filter QueryFilter) (*QueryResults[api.CollectionResource], error)
	UpdateCollection(collection *api.CollectionResource) error
	PatchCollection(id string, patches *api.Patch) error
	DeleteCollection(id string) error

	// Provider operations
	CreateProvider(provider *api.ProviderResource) error
	GetProvider(id string) (*api.ProviderResource, error)
	//GetProviders(limit int, offset int, filter QueryFilter) (*QueryResults[api.ProviderResource], error)
	//UpdateProvider(provider *api.ProviderResource) error
	//PatchProvider(id string, patches *api.Patch) error
	DeleteProvider(id string) error

	// Close the storage connection
	Close() error
}

// This interface must be decoupled from the service HTTP layer.
// Do not pass ExecutionContext, Request or Response wrappers either.
