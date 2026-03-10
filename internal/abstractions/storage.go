package abstractions

import (
	"context"
	"log/slog"
	"maps"
	"time"

	"github.com/eval-hub/eval-hub/pkg/api"
)

type QueryResults[T any] struct {
	Items      []T
	TotalCount int
	Errors     []string
}

type QueryFilter struct {
	Limit  int
	Offset int
	Params map[string]any
	// Make tenant explicit because it is not a user parameter
	Tenant api.Tenant
}

// Returns the limit, offset, and filtered params
func (filter *QueryFilter) ExtractQueryParams() *QueryFilter {
	params := maps.Clone(filter.Params)
	// delete empty values
	maps.DeleteFunc(params, func(k string, v any) bool {
		return v == ""
	})
	return &QueryFilter{
		Limit:  filter.Limit,
		Offset: filter.Offset,
		Params: params,
		Tenant: filter.Tenant,
	}
}

type Storage interface {
	WithLogger(logger *slog.Logger) Storage
	WithContext(ctx context.Context) Storage
	WithTenant(tenant api.Tenant) Storage
	WithOwner(owner api.User) Storage

	Ping(timeout time.Duration) error

	// Evaluation job operations
	CreateEvaluationJob(evaluation *api.EvaluationJobResource) error
	GetEvaluationJob(id string) (*api.EvaluationJobResource, error)
	GetEvaluationJobs(filter *QueryFilter) (*QueryResults[api.EvaluationJobResource], error)
	DeleteEvaluationJob(id string) error
	UpdateEvaluationJob(id string, runStatus *api.StatusEvent) error
	// UpdateEvaluationJobStatus is used to update the status of an evaluation job and is internal - do we need it here?
	UpdateEvaluationJobStatus(id string, state api.OverallState, message *api.MessageInfo) error

	// Collection operations
	CreateCollection(collection *api.CollectionResource) error
	GetCollection(id string) (*api.CollectionResource, error)
	GetCollections(filter *QueryFilter) (*QueryResults[api.CollectionResource], error)
	UpdateCollection(collection *api.CollectionResource) error
	PatchCollection(id string, patches *api.Patch) error
	DeleteCollection(id string) error

	// Provider operations
	CreateProvider(provider *api.ProviderResource) error
	GetProvider(id string) (*api.ProviderResource, error)
	GetProviders(filter *QueryFilter) (*QueryResults[api.ProviderResource], error)
	UpdateProvider(id string, provider *api.ProviderResource) (*api.ProviderResource, error)
	PatchProvider(id string, patches *api.Patch) (*api.ProviderResource, error)
	DeleteProvider(id string) error

	// Close the storage connection
	Close() error
}

// This interface must be decoupled from the service HTTP layer.
// Do not pass ExecutionContext, Request or Response wrappers either.
