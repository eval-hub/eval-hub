package sqlite

import (
	"database/sql"
	"fmt"
	"log/slog"
	"strings"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/storage/sql/shared"
	"github.com/eval-hub/eval-hub/pkg/api"
)

const (
	INSERT_EVALUATION_STATEMENT = `INSERT INTO evaluations (id, tenant_id, owner, status, experiment_id, entity) VALUES (?, ?, ?, ?, ?, ?);`

	INSERT_COLLECTION_STATEMENT = `INSERT INTO collections (id, tenant_id, owner, entity) VALUES (?, ?, ?, ?);`

	INSERT_PROVIDER_STATEMENT = `INSERT INTO providers (id, tenant_id, owner, entity) VALUES (?, ?, ?, ?);`

	TABLES_SCHEMA = `
CREATE TABLE IF NOT EXISTS evaluations (
    id VARCHAR(36) NOT NULL,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    tenant_id VARCHAR(255) NOT NULL,
    owner VARCHAR(255) NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    experiment_id VARCHAR(255) NOT NULL,
    entity TEXT NOT NULL,
    PRIMARY KEY (id)
);

CREATE TABLE IF NOT EXISTS collections (
    id VARCHAR(36) NOT NULL,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    tenant_id VARCHAR(255) NOT NULL,
    owner VARCHAR(255) NOT NULL,
    entity TEXT NOT NULL,
    PRIMARY KEY (id)
);

CREATE TABLE IF NOT EXISTS providers (
    id VARCHAR(36) NOT NULL,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    tenant_id VARCHAR(255) NOT NULL,
    owner VARCHAR(255) NOT NULL,
    entity TEXT NOT NULL,
    PRIMARY KEY (id)
);

CREATE INDEX IF NOT EXISTS idx_eval_entity
ON evaluations (id);

CREATE INDEX IF NOT EXISTS idx_collection_entity
ON collections (id);

CREATE INDEX IF NOT EXISTS idx_provider_entity
ON providers (id);
`
)

type sqliteStatementsFactory struct {
	logger *slog.Logger
}

func NewStatementsFactory(logger *slog.Logger) shared.SQLStatementsFactory {
	return &sqliteStatementsFactory{logger: logger}
}

func (s *sqliteStatementsFactory) GetLogger() *slog.Logger {
	return s.logger
}

func (s *sqliteStatementsFactory) GetTablesSchema() string {
	return TABLES_SCHEMA
}

// allowedFilterColumns returns the set of column/param names allowed in filter for each table.
func (s *sqliteStatementsFactory) GetAllowedFilterColumns(tableName string) []string {
	allColumns := []string{"owner", "name", "tags"}
	switch tableName {
	case shared.TABLE_EVALUATIONS:
		return append(allColumns, "status", "experiment_id")
	case shared.TABLE_PROVIDERS:
		return allColumns // "benchmarks" and "scope" are not allowed filters for providers from the database
	case shared.TABLE_COLLECTIONS:
		return append(allColumns, "category") // "scope" is not allowed filter for collections from the database
	default:
		return nil
	}
}

func (s *sqliteStatementsFactory) CreateEvaluationAddEntityStatement(evaluation *api.EvaluationJobResource, entity string) (string, []any) {
	return INSERT_EVALUATION_STATEMENT, []any{evaluation.Resource.ID, evaluation.Resource.Tenant, evaluation.Resource.Owner, evaluation.Status.State, evaluation.Resource.MLFlowExperimentID, entity}
}

func (s *sqliteStatementsFactory) CreateEvaluationGetEntityStatement(query *shared.EntityQuery) (string, []any, []any) {
	where, whereArgs := s.getWhereStatement(query.Resource.Tenant, query.Resource.ID)
	return fmt.Sprintf(`SELECT id, created_at, updated_at, tenant_id, owner, status, experiment_id, entity FROM evaluations WHERE %s;`, where), whereArgs, []any{&query.Resource.ID, &query.Resource.CreatedAt, &query.Resource.UpdatedAt, &query.Resource.Tenant, &query.Resource.Owner, &query.Status, &query.MLFlowExperimentID, &query.EntityJSON}
}

// entityFilterCondition returns the SQL condition and args for a filter key.
func (s *sqliteStatementsFactory) CreateEntityFilterCondition(key string, value any, index int, tableName string) (condition string, args []any) {
	switch key {
	case "name":
		// evaluations: name at config.name; providers and collections: name at entity root
		namePath := "$.name"
		if tableName == shared.TABLE_EVALUATIONS {
			namePath = "$.config.name"
		}
		// name at top level
		return fmt.Sprintf("json_extract(entity, '%s') = ?", namePath), []any{value}
	case "category":
		if tableName == shared.TABLE_COLLECTIONS {
			// collections: category at entity root
			categoryPath := "$.category"
			return fmt.Sprintf("json_extract(entity, '%s') = ?", categoryPath), []any{value}
		}
		// should never get here as we validate the filter before calling this function
		return "", []any{}
	case "tags":
		tagStr, _ := value.(string)
		// evaluations: tags at config.tags; providers and collections: tags at entity root
		tagsPath := "$.tags"
		if tableName == shared.TABLE_EVALUATIONS {
			tagsPath = "$.config.tags"
		}
		return fmt.Sprintf("json_type(json_extract(entity, '%s')) = 'array' AND EXISTS (SELECT 1 FROM json_each(json_extract(entity, '%s')) WHERE value = ?)", tagsPath, tagsPath), []any{tagStr}
	case "ORDER BY":
		return "ORDER BY " + value.(string), []any{}
	case "LIMIT", "OFFSET":
		return key + " ?", []any{value}
	default:
		if v, ok := value.(string); ok && strings.HasPrefix(v, "!") {
			return "NOT (" + key + " = ?)", []any{v[1:]}
		}
		return key + " = ?", []any{value}
	}
}

func (s *sqliteStatementsFactory) CreateCountEntitiesStatement(tenant api.Tenant, tableName string, filter map[string]any) (string, []any) {
	where, whereArgs := s.getWhereStatement(tenant, "") // we don't need to filter by id as we want to count all entities
	filterClause, args := shared.CreateFilterStatement(s, where, whereArgs, filter, "", 0, 0, tableName)
	query := fmt.Sprintf(`SELECT COUNT(*) FROM %s%s;`, tableName, filterClause)
	return query, args
}

func (s *sqliteStatementsFactory) CreateListEntitiesStatement(tenant api.Tenant, tableName string, limit, offset int, filter map[string]any) (string, []any) {
	where, whereArgs := s.getWhereStatement(tenant, "") // we don't need to filter by id as we want to count all entities
	filterClause, args := shared.CreateFilterStatement(s, where, whereArgs, filter, "id DESC", limit, offset, tableName)

	var query string
	switch tableName {
	case shared.TABLE_EVALUATIONS:
		query = fmt.Sprintf(`SELECT id, created_at, updated_at, tenant_id, owner, status, experiment_id, entity FROM %s%s;`, tableName, filterClause)
	default:
		query = fmt.Sprintf(`SELECT id, created_at, updated_at, tenant_id, owner, entity FROM %s%s;`, tableName, filterClause)
	}

	return query, args
}

func (s *sqliteStatementsFactory) ScanRowForEntity(tenant api.Tenant, tableName string, rows *sql.Rows, query *shared.EntityQuery) error {
	switch tableName {
	case shared.TABLE_EVALUATIONS:
		return rows.Scan(&query.Resource.ID, &query.Resource.CreatedAt, &query.Resource.UpdatedAt, &query.Resource.Tenant, &query.Resource.Owner, &query.Status, &query.MLFlowExperimentID, &query.EntityJSON)
	default:
		return rows.Scan(&query.Resource.ID, &query.Resource.CreatedAt, &query.Resource.UpdatedAt, &query.Resource.Tenant, &query.Resource.Owner, &query.EntityJSON)
	}
}

func (s *sqliteStatementsFactory) CreateDeleteEntityStatement(tenant api.Tenant, tableName string, id string) (string, []any) {
	// these WHERE statements are okay because we can only delete user resources
	if !tenant.IsEmpty() {
		return fmt.Sprintf(`DELETE FROM %s WHERE id = ? AND tenant_id = ?;`, tableName), []any{id, tenant.String()}
	}
	return fmt.Sprintf(`DELETE FROM %s WHERE id = ?;`, tableName), []any{id}
}

func (s *sqliteStatementsFactory) CreateUpdateEntityStatement(tenant api.Tenant, tableName, id string, entityJSON string, status *api.OverallState) (string, []any) {
	// these WHERE statements are okay because we can only update user resources
	switch tableName {
	case shared.TABLE_EVALUATIONS:
		if !tenant.IsEmpty() {
			return fmt.Sprintf(`UPDATE %s SET status = ?, entity = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND tenant_id = ?;`, tableName), []any{*status, entityJSON, id, tenant.String()}
		}
		return fmt.Sprintf(`UPDATE %s SET status = ?, entity = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?;`, tableName), []any{*status, entityJSON, id}
	default:
		if !tenant.IsEmpty() {
			return fmt.Sprintf(`UPDATE %s SET entity = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND tenant_id = ?;`, tableName), []any{entityJSON, id, tenant.String()}
		}
		return fmt.Sprintf(`UPDATE %s SET entity = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?;`, tableName), []any{entityJSON, id}
	}
}

func (s *sqliteStatementsFactory) CreateProviderAddEntityStatement(provider *api.ProviderResource, entity string) (string, []any) {
	return INSERT_PROVIDER_STATEMENT, []any{provider.Resource.ID, provider.Resource.Tenant, provider.Resource.Owner, entity}
}

func (s *sqliteStatementsFactory) getWhereStatement(tenant api.Tenant, id string) (string, []any) {
	var sb strings.Builder
	var args []any
	if id != "" {
		sb.WriteString("id = ?")
		args = append(args, id)
	}
	// As we want to allow system providers to be selected without a tenant_id, we have to select
	// either with tenant_id == tenant_id OR owner == system
	if !tenant.IsEmpty() {
		if sb.Len() > 0 {
			sb.WriteString(" AND ")
		}
		sb.WriteString(fmt.Sprintf("(tenant_id = ? OR owner = '%s')", abstractions.OwnerSystem))
		args = append(args, tenant.String())
	}
	return sb.String(), args
}

func (s *sqliteStatementsFactory) CreateProviderGetEntityStatement(query *shared.EntityQuery) (string, []any, []any) {
	where, whereArgs := s.getWhereStatement(query.Resource.Tenant, query.Resource.ID)
	return fmt.Sprintf(`SELECT id, created_at, updated_at, tenant_id, owner, entity FROM providers WHERE %s;`, where), whereArgs, []any{&query.Resource.ID, &query.Resource.CreatedAt, &query.Resource.UpdatedAt, &query.Resource.Tenant, &query.Resource.Owner, &query.EntityJSON}
}

func (s *sqliteStatementsFactory) CreateCollectionAddEntityStatement(collection *api.CollectionResource, entity string) (string, []any) {
	return INSERT_COLLECTION_STATEMENT, []any{collection.Resource.ID, collection.Resource.Tenant, collection.Resource.Owner, entity}
}

func (s *sqliteStatementsFactory) CreateCollectionGetEntityStatement(query *shared.EntityQuery) (string, []any, []any) {
	where, whereArgs := s.getWhereStatement(query.Resource.Tenant, query.Resource.ID)
	return fmt.Sprintf(`SELECT id, created_at, updated_at, tenant_id, owner, entity FROM collections WHERE %s;`, where), whereArgs, []any{&query.Resource.ID, &query.Resource.CreatedAt, &query.Resource.UpdatedAt, &query.Resource.Tenant, &query.Resource.Owner, &query.EntityJSON}
}
