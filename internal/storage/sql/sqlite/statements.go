package sqlite

import (
	"database/sql"
	"fmt"
	"log/slog"
	"slices"
	"strings"

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
		return allColumns // "benchmarks" and "system_defined" are not allowed filters for providers from the database
	case shared.TABLE_COLLECTIONS:
		return allColumns // "system_defined" is not allowed filter for collections from the database
	default:
		return nil
	}
}

func (s *sqliteStatementsFactory) CreateEvaluationAddEntityStatement(evaluation *api.EvaluationJobResource, entity string) (string, []any) {
	return INSERT_EVALUATION_STATEMENT, []any{evaluation.Resource.ID, evaluation.Resource.Tenant, evaluation.Resource.Owner, evaluation.Status.State, evaluation.Resource.MLFlowExperimentID, entity}
}

func (s *sqliteStatementsFactory) CreateEvaluationGetEntityStatement(query *shared.EntityQuery) (string, []any, []any) {
	if query.Resource.Tenant.IsEmpty() {
		return `SELECT id, created_at, updated_at, tenant_id, owner, status, experiment_id, entity FROM evaluations WHERE id = ?;`, []any{&query.Resource.ID}, []any{&query.Resource.ID, &query.Resource.CreatedAt, &query.Resource.UpdatedAt, &query.Resource.Tenant, &query.Resource.Owner, &query.Status, &query.MLFlowExperimentID, &query.EntityJSON}
	}
	return `SELECT id, created_at, updated_at, tenant_id, owner, status, experiment_id, entity FROM evaluations WHERE id = ? AND tenant_id = ?;`, []any{&query.Resource.ID, query.Resource.Tenant.String()}, []any{&query.Resource.ID, &query.Resource.CreatedAt, &query.Resource.UpdatedAt, &query.Resource.Tenant, &query.Resource.Owner, &query.Status, &query.MLFlowExperimentID, &query.EntityJSON}
}

// entityFilterCondition returns the SQL condition and args for a filter key.
func (s *sqliteStatementsFactory) entityFilterCondition(key string, value any, tableName string) (condition string, args []any) {
	switch key {
	case "name":
		// evaluations: name at config.name; providers and collections: name at entity root
		namePath := "$.name"
		if tableName == shared.TABLE_EVALUATIONS {
			namePath = "$.config.name"
		}
		// name at top level
		return fmt.Sprintf("json_extract(entity, '%s') = ?", namePath), []any{value}
	case "tags":
		tagStr, _ := value.(string)
		// evaluations: tags at config.tags; providers and collections: tags at entity root
		tagsPath := "$.tags"
		if tableName == shared.TABLE_EVALUATIONS {
			tagsPath = "$.config.tags"
		}
		return fmt.Sprintf("json_type(json_extract(entity, '%s')) = 'array' AND EXISTS (SELECT 1 FROM json_each(json_extract(entity, '%s')) WHERE value = ?)", tagsPath, tagsPath), []any{tagStr}
	default:
		return key + " = ?", []any{value}
	}
}

// createFilterStatement builds a WHERE clause and args from the filter.
// It validates each key against the table's allowlist, sorts keys deterministically,
// and returns both the clause and args in matching order. Returns an error if any
// filter key is not in the allowlist (fail closed).
func (s *sqliteStatementsFactory) createFilterStatement(tenant api.Tenant, filter map[string]any, orderBy string, limit int, offset int, tableName string) (string, []any) {
	var args []any
	var sb strings.Builder

	and := false

	// we must always filter by tenant_id if it exists
	if !tenant.IsEmpty() {
		sb.WriteString(" WHERE ")
		cond, condArgs := s.entityFilterCondition("tenant_id", tenant.String(), tableName)
		sb.WriteString(cond)
		args = append(args, condArgs...)
		and = true
	}

	if len(filter) > 0 {
		allowed := s.GetAllowedFilterColumns(tableName)
		for key, value := range filter {
			if slices.Contains(allowed, key) {
				if !and {
					sb.WriteString(" WHERE ")
					and = true
				} else if and {
					sb.WriteString(" AND ")
				}
				cond, condArgs := s.entityFilterCondition(key, value, tableName)
				sb.WriteString(cond)
				args = append(args, condArgs...)
			} else {
				// should never get here as we validate the filter before calling this function
				s.logger.Warn("Disallowed filter key", "key", key, "tableName", tableName)
			}
		}
	}

	if orderBy != "" {
		sb.WriteString(" ORDER BY ")
		sb.WriteString(orderBy)
	}
	if limit > 0 {
		sb.WriteString(" LIMIT ?")
		args = append(args, limit)
	}
	if offset > 0 {
		sb.WriteString(" OFFSET ?")
		args = append(args, offset)
	}

	return sb.String(), args
}

func (s *sqliteStatementsFactory) CreateCountEntitiesStatement(tenant api.Tenant, tableName string, filter map[string]any) (string, []any) {
	filterClause, args := s.createFilterStatement(tenant, filter, "", 0, 0, tableName)
	query := fmt.Sprintf(`SELECT COUNT(*) FROM %s%s;`, tableName, filterClause)
	return query, args
}

func (s *sqliteStatementsFactory) CreateListEntitiesStatement(tenant api.Tenant, tableName string, limit, offset int, filter map[string]any) (string, []any) {
	filterClause, args := s.createFilterStatement(tenant, filter, "id DESC", limit, offset, tableName)

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

func (s *sqliteStatementsFactory) CreateCheckEntityExistsStatement(tenant api.Tenant, tableName string, id string) (string, []any) {
	// SELECT id, created_at, updated_at, tenant_id, owner, status, experiment_id, entity FROM evaluations WHERE id = ?;
	if !tenant.IsEmpty() {
		return fmt.Sprintf(`SELECT id, status FROM %s WHERE id = ? AND tenant_id = ?;`, tableName), []any{id, tenant.String()}
	}
	return fmt.Sprintf(`SELECT id, status FROM %s WHERE id = ?;`, tableName), []any{id}
}

func (s *sqliteStatementsFactory) CreateDeleteEntityStatement(tenant api.Tenant, tableName string, id string) (string, []any) {
	if !tenant.IsEmpty() {
		return fmt.Sprintf(`DELETE FROM %s WHERE id = ? AND tenant_id = ?;`, tableName), []any{id, tenant.String()}
	}
	return fmt.Sprintf(`DELETE FROM %s WHERE id = ?;`, tableName), []any{id}
}

func (s *sqliteStatementsFactory) CreateUpdateEntityStatement(tenant api.Tenant, tableName, id string, entityJSON string, status *api.OverallState) (string, []any) {
	// UPDATE "evaluations" SET "status" = ?, "entity" = ?, "updated_at" = CURRENT_TIMESTAMP WHERE "id" = ?;
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

func (s *sqliteStatementsFactory) CreateProviderGetEntityStatement(query *shared.EntityQuery) (string, []any, []any) {
	// SELECT id, created_at, updated_at, tenant_id, owner, entity FROM providers WHERE id = ?;
	if query.Resource.Tenant.IsEmpty() {
		return `SELECT id, created_at, updated_at, tenant_id, owner, entity FROM providers WHERE id = ?;`, []any{&query.Resource.ID}, []any{&query.Resource.ID, &query.Resource.CreatedAt, &query.Resource.UpdatedAt, &query.Resource.Tenant, &query.Resource.Owner, &query.EntityJSON}
	}
	return `SELECT id, created_at, updated_at, tenant_id, owner, entity FROM providers WHERE id = ? AND tenant_id = ?;`, []any{&query.Resource.ID, query.Resource.Tenant.String()}, []any{&query.Resource.ID, &query.Resource.CreatedAt, &query.Resource.UpdatedAt, &query.Resource.Tenant, &query.Resource.Owner, &query.EntityJSON}
}

func (s *sqliteStatementsFactory) CreateCollectionAddEntityStatement(collection *api.CollectionResource, entity string) (string, []any) {
	return INSERT_COLLECTION_STATEMENT, []any{collection.Resource.ID, collection.Resource.Tenant, collection.Resource.Owner, entity}
}

func (s *sqliteStatementsFactory) CreateCollectionGetEntityStatement(query *shared.EntityQuery) (string, []any, []any) {
	// SELECT id, created_at, updated_at, tenant_id, owner, entity FROM collections WHERE id = ?;
	if query.Resource.Tenant.IsEmpty() {
		return `SELECT id, created_at, updated_at, tenant_id, owner, entity FROM collections WHERE id = ?;`, []any{&query.Resource.ID}, []any{&query.Resource.ID, &query.Resource.CreatedAt, &query.Resource.UpdatedAt, &query.Resource.Tenant, &query.Resource.Owner, &query.EntityJSON}
	}
	return `SELECT id, created_at, updated_at, tenant_id, owner, entity FROM collections WHERE id = ? AND tenant_id = ?;`, []any{&query.Resource.ID, query.Resource.Tenant.String()}, []any{&query.Resource.ID, &query.Resource.CreatedAt, &query.Resource.UpdatedAt, &query.Resource.Tenant, &query.Resource.Owner, &query.EntityJSON}
}
