package abstractions

import (
	"context"
	"fmt"
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

// ExtractQueryParams returns the limit, offset, and filtered params
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

// HasParams returns true if all the given params are present in the filter and have non-empty values
func (filter *QueryFilter) HasParams(params ...string) bool {
	queryParams := filter.ExtractQueryParams().Params
	for _, param := range params {
		if _, exists := queryParams[param]; !exists {
			return false
		}
	}
	return true
}

func (filter *QueryFilter) String() string {
	return fmt.Sprintf(`{"limit":%d,"offset":%d,"params":%v,"tenant":"%s"}`, filter.Limit, filter.Offset, filter.Params, filter.Tenant)
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
	UpdateEvaluationJob(id string, runStatus *api.StatusEvent, benchmarks []api.BenchmarkConfig) error
	// UpdateEvaluationJobStatus is used to update the status of an evaluation job and is internal - do we need it here?
	UpdateEvaluationJobStatus(id string, state api.OverallState, message *api.MessageInfo) error

	// Collection operations
	CreateCollection(collection *api.CollectionResource) error
	GetCollection(id string) (*api.CollectionResource, error)
	GetCollections(filter *QueryFilter) (*QueryResults[api.CollectionResource], error)
	UpdateCollection(id string, collection *api.CollectionConfig) (*api.CollectionResource, error)
	PatchCollection(id string, patches *api.Patch) (*api.CollectionResource, error)
	DeleteCollection(id string) error

	// Provider operations
	CreateProvider(provider *api.ProviderResource) error
	GetProvider(id string) (*api.ProviderResource, error)
	GetProviders(filter *QueryFilter) (*QueryResults[api.ProviderResource], error)
	UpdateProvider(id string, providerConfig *api.ProviderConfig) (*api.ProviderResource, error)
	PatchProvider(id string, patches *api.Patch) (*api.ProviderResource, error)
	DeleteProvider(id string) error

	// Close the storage connection
	Close() error
}

// This interface must be decoupled from the service HTTP layer.
// Do not pass ExecutionContext, Request or Response wrappers either.
