package shared

import (
	"database/sql"
	"log/slog"

	"github.com/eval-hub/eval-hub/pkg/api"
)

type SQLStatementsFactory interface {
	GetLogger() *slog.Logger

	GetTablesSchema() string

	GetAllowedFilterColumns(tableName string) []string
	CreateEntityFilterCondition(key string, value any, index int, tableName string) (condition string, args []any)

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
	ScanRowForEntity(tenant api.Tenant, ableName string, rows *sql.Rows, query *EntityQuery) error
	CreateCheckEntityExistsStatement(tenant api.Tenant, tableName string, id string) (string, []any)
	CreateDeleteEntityStatement(tenant api.Tenant, tableName string, id string) (string, []any)
	CreateUpdateEntityStatement(tenant api.Tenant, tableName, id string, entityJSON string, status *api.OverallState) (string, []any)
}
