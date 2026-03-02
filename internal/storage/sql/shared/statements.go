package shared

import (
	"github.com/eval-hub/eval-hub/pkg/api"
)

type SQLStatementsFactory interface {
	GetTablesSchema() string

	// evaluations operations
	CreateEvaluationAddEntityStatement(evaluation *api.EvaluationJobResource, entity string) (string, []any)
	CreateEvaluationGetEntityStatement(query *EvaluationJobQuery) (string, []any, []any)

	// collections operations
	CreateCollectionAddEntityStatement(collection *api.CollectionResource, entity string) (string, []any)
	CreateCollectionGetEntityStatement(query *CollectionQuery) (string, []any, []any)

	// providers operations
	CreateProviderAddEntityStatement(provider *api.ProviderResource, entity string) (string, []any)
	CreateProviderGetEntityStatement(query *ProviderQuery) (string, []any, []any)

	// common operations
	CreateCountEntitiesStatement(tableName string, filter map[string]any) (string, []any)
	CreateListEntitiesStatement(tableName string, limit, offset int, filter map[string]any) (string, []any)
	CreateCheckEntityExistsStatement(tableName string) string
	CreateDeleteEntityStatement(tableName string) string
	CreateUpdateEntityStatement(tableName, id string, entityJSON string, status *api.OverallState) (string, []any)
}
