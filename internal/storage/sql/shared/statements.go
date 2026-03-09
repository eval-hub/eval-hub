package shared

import (
	"database/sql"

	"github.com/eval-hub/eval-hub/pkg/api"
)

type SQLStatementsFactory interface {
	GetTablesSchema() string

	GetAllowedFilterColumns(tableName string) []string

	// evaluations operations
	CreateEvaluationAddEntityStatement(evaluation *api.EvaluationJobResource, entity string) (string, []any)
	CreateEvaluationGetEntityStatement(query *EntityQuery) (string, []any, []any)

	// collections operations
	CreateCollectionAddEntityStatement(collection *api.CollectionResource, entity string) (string, []any)
	CreateCollectionGetEntityStatement(query *EntityQuery) (string, []any, []any)

	// providers operations
	CreateProviderAddEntityStatement(provider *api.ProviderResource, entity string) (string, []any)
	CreateProviderGetEntityStatement(query *EntityQuery) (string, []any, []any)

	// common operations
	CreateCountEntitiesStatement(tenant api.Tenant, tableName string, filter map[string]any) (string, []any)
	CreateListEntitiesStatement(tenant api.Tenant, tableName string, limit, offset int, filter map[string]any) (string, []any)
	ScanRowForEntity(tableName string, rows *sql.Rows, query *EntityQuery) error
	CreateCheckEntityExistsStatement(tableName string) string
	CreateDeleteEntityStatement(tableName string) string
	CreateUpdateEntityStatement(tableName, id string, entityJSON string, status *api.OverallState) (string, []any)
}
