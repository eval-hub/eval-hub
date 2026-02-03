package abstractions

import (
	"time"

	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"github.com/eval-hub/eval-hub/internal/http_wrappers"
	"github.com/eval-hub/eval-hub/pkg/api"
)

type Storage interface {
	// This is used to identify the storage implementation in the logs and error messages
	GetDatasourceName() string

	Ping(timeout time.Duration) error

	// Evaluation job operations
	CreateEvaluationJob(ctx *executioncontext.ExecutionContext, evaluation *api.EvaluationJobConfig) (*api.EvaluationJobResource, error)
	GetEvaluationJob(ctx *executioncontext.ExecutionContext, id string) (*api.EvaluationJobResource, error)
	GetEvaluationJobs(ctx *executioncontext.ExecutionContext, r http_wrappers.RequestWrapper, limit int, offset int, statusFilter string) (*api.EvaluationJobResourceList, error)
	DeleteEvaluationJob(ctx *executioncontext.ExecutionContext, id string, hardDelete bool) error
	UpdateEvaluationJobStatus(ctx *executioncontext.ExecutionContext, id string, state *api.StatusEvent) error

	// Collection operations
	CreateCollection(ctx *executioncontext.ExecutionContext, collection *api.CollectionResource) error
	GetCollection(ctx *executioncontext.ExecutionContext, id string, summary bool) (*api.CollectionResource, error)
	GetCollections(ctx *executioncontext.ExecutionContext, limit int, offset int) (*api.CollectionResourceList, error)
	UpdateCollection(ctx *executioncontext.ExecutionContext, collection *api.CollectionResource) error
	DeleteCollection(ctx *executioncontext.ExecutionContext, id string) error

	// Close the storage connection
	Close() error
}
