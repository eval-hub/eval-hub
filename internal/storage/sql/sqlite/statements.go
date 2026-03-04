package sqlite

import (
	"database/sql"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/eval-hub/eval-hub/internal/storage/sql/shared"
	"github.com/eval-hub/eval-hub/pkg/api"
)

const (
	INSERT_EVALUATION_STATEMENT = `INSERT INTO evaluations (id, tenant_id, owner, status, experiment_id, entity) VALUES (?, ?, ?, ?, ?, ?);`
	SELECT_EVALUATION_STATEMENT = `SELECT id, created_at, updated_at, tenant_id, owner, status, experiment_id, entity FROM evaluations WHERE id = ?;`

	INSERT_COLLECTION_STATEMENT = `INSERT INTO collections (id, tenant_id, owner, entity) VALUES (?, ?, ?, ?);`
	SELECT_COLLECTION_STATEMENT = `SELECT id, created_at, updated_at, tenant_id, owner, entity FROM collections WHERE id = ?;`

	INSERT_PROVIDER_STATEMENT = `INSERT INTO providers (id, tenant_id, owner, entity) VALUES (?, ?, ?, ?);`
	SELECT_PROVIDER_STATEMENT = `SELECT id, created_at, updated_at, tenant_id, owner, entity FROM providers WHERE id = ?;`

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
	allColumns := []string{"tenant_id", "owner", "name", "tags"}
	switch tableName {
	case shared.TABLE_EVALUATIONS:
		return append(allColumns, "status", "experiment_id")
	case shared.TABLE_PROVIDERS:
		return allColumns // "benchmarks" and "system_defined" are not allowed filters for providers from the database
	case shared.TABLE_COLLECTIONS:
		return allColumns
	default:
		return nil
	}
}

func (s *sqliteStatementsFactory) CreateEvaluationAddEntityStatement(evaluation *api.EvaluationJobResource, entity string) (string, []any) {
	return INSERT_EVALUATION_STATEMENT, []any{evaluation.Resource.ID, evaluation.Resource.Tenant, evaluation.Resource.Owner, evaluation.Status.State, evaluation.Resource.MLFlowExperimentID, entity}
}

func (s *sqliteStatementsFactory) CreateEvaluationGetEntityStatement(query *shared.EntityQuery) (string, []any, []any) {
	return SELECT_EVALUATION_STATEMENT, []any{&query.Resource.ID}, []any{&query.Resource.ID, &query.Resource.CreatedAt, &query.Resource.UpdatedAt, &query.Resource.Tenant, &query.Resource.Owner, &query.Status, &query.MLFlowExperimentID, &query.EntityJSON}
}

// evaluationFilterCondition returns the SQL condition and args for an evaluation filter key.
// For "name" and "tags" (stored in entity JSON), uses json_extract/json_each.
// Tags supports "key" (match by key) or "key:value" (match by key and value).
func (s *sqliteStatementsFactory) evaluationFilterCondition(key string, value any) (condition string, args []any) {
	switch key {
	case "name":
		return "json_extract(entity, '$.config.experiment.name') = ?", []any{value}
	case "tags":
		tagStr, _ := value.(string)
		if keyPart, valuePart, ok := strings.Cut(tagStr, ":"); ok && valuePart != "" {
			// tags=key:value - match by both key and value
			return "EXISTS (SELECT 1 FROM json_each(json_extract(entity, '$.config.experiment.tags')) WHERE json_extract(value, '$.key') = ? AND json_extract(value, '$.value') = ?)", []any{keyPart, valuePart}
		}
		// tags=key - match by key only
		return "EXISTS (SELECT 1 FROM json_each(json_extract(entity, '$.config.experiment.tags')) WHERE json_extract(value, '$.key') = ?)", []any{tagStr}
	default:
		return key + " = ?", []any{value}
	}
}

// createFilterStatement builds a WHERE clause and args from the filter.
// It validates each key against the table's allowlist, sorts keys deterministically,
// and returns both the clause and args in matching order. Returns an error if any
// filter key is not in the allowlist (fail closed).
func (s *sqliteStatementsFactory) createFilterStatement(filter map[string]any, orderBy string, limit int, offset int, tableName string) (string, []any) {
	var args []any
	var sb strings.Builder

	if len(filter) > 0 {
		allowed := s.GetAllowedFilterColumns(tableName)
		if allowed == nil {
			return "", nil
		}
		keys := slices.Collect(maps.Keys(filter))
		sort.Strings(keys)
		var disallowed []string
		validKeys := make([]string, 0, len(keys))
		for _, key := range keys {
			if slices.Contains(allowed, key) {
				validKeys = append(validKeys, key)
			} else {
				disallowed = append(disallowed, key)
			}
		}
		if len(disallowed) > 0 {
			// ignore this for now as we validate the filter before calling this function
			s.logger.Warn("Disallowed filter keys", "keys", disallowed, "tableName", tableName)
		}
		if len(validKeys) > 0 {
			sb.WriteString(" WHERE ")
			for i, key := range validKeys {
				if i > 0 {
					sb.WriteString(" AND ")
				}
				cond, condArgs := s.evaluationFilterCondition(key, filter[key])
				sb.WriteString(cond)
				args = append(args, condArgs...)
			}
		}
	}

	if orderBy != "" {
		sb.WriteString(fmt.Sprintf(" ORDER BY %s", orderBy))
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

func (s *sqliteStatementsFactory) CreateCountEntitiesStatement(tableName string, filter map[string]any) (string, []any) {
	filterClause, args := s.createFilterStatement(filter, "", 0, 0, tableName)
	query := fmt.Sprintf(`SELECT COUNT(*) FROM %s%s;`, tableName, filterClause)
	return query, args
}

func (s *sqliteStatementsFactory) CreateListEntitiesStatement(tableName string, limit, offset int, filter map[string]any) (string, []any) {
	filterClause, args := s.createFilterStatement(filter, "id DESC", limit, offset, tableName)

	var query string
	switch tableName {
	case shared.TABLE_EVALUATIONS:
		query = fmt.Sprintf(`SELECT id, created_at, updated_at, tenant_id, owner, status, experiment_id, entity FROM %s%s;`, tableName, filterClause)
	default:
		query = fmt.Sprintf(`SELECT id, created_at, updated_at, tenant_id, owner, entity FROM %s%s;`, tableName, filterClause)
	}

	return query, args
}

func (s *sqliteStatementsFactory) ScanRowForEntity(tableName string, rows *sql.Rows, query *shared.EntityQuery) error {
	switch tableName {
	case shared.TABLE_EVALUATIONS:
		return rows.Scan(&query.Resource.ID, &query.Resource.CreatedAt, &query.Resource.UpdatedAt, &query.Resource.Tenant, &query.Resource.Owner, &query.Status, &query.MLFlowExperimentID, &query.EntityJSON)
	default:
		return rows.Scan(&query.Resource.ID, &query.Resource.CreatedAt, &query.Resource.UpdatedAt, &query.Resource.Tenant, &query.Resource.Owner, &query.EntityJSON)
	}
}

func (s *sqliteStatementsFactory) CreateCheckEntityExistsStatement(tableName string) string {
	return fmt.Sprintf(`SELECT id, status FROM %s WHERE id = ?;`, tableName)
}

func (s *sqliteStatementsFactory) CreateDeleteEntityStatement(tableName string) string {
	return fmt.Sprintf(`DELETE FROM %s WHERE id = ?;`, tableName)
}

func (s *sqliteStatementsFactory) CreateUpdateEntityStatement(tableName, id string, entityJSON string, status *api.OverallState) (string, []any) {
	// UPDATE "evaluations" SET "status" = ?, "entity" = ?, "updated_at" = CURRENT_TIMESTAMP WHERE "id" = ?;
	switch tableName {
	case shared.TABLE_EVALUATIONS:
		return fmt.Sprintf(`UPDATE %s SET status = ?, entity = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?;`, tableName), []any{*status, entityJSON, id}
	default:
		return fmt.Sprintf(`UPDATE %s SET entity = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?;`, tableName), []any{entityJSON, id}
	}
}

func (s *sqliteStatementsFactory) CreateProviderAddEntityStatement(provider *api.ProviderResource, entity string) (string, []any) {
	return INSERT_PROVIDER_STATEMENT, []any{provider.Resource.ID, provider.Resource.Tenant, provider.Resource.Owner, entity}
}

func (s *sqliteStatementsFactory) CreateProviderGetEntityStatement(query *shared.EntityQuery) (string, []any, []any) {
	return SELECT_PROVIDER_STATEMENT, []any{&query.Resource.ID}, []any{&query.Resource.ID, &query.Resource.CreatedAt, &query.Resource.UpdatedAt, &query.Resource.Tenant, &query.Resource.Owner, &query.EntityJSON}
}

func (s *sqliteStatementsFactory) CreateCollectionAddEntityStatement(collection *api.CollectionResource, entity string) (string, []any) {
	return INSERT_COLLECTION_STATEMENT, []any{collection.Resource.ID, collection.Resource.Tenant, collection.Resource.Owner, entity}
}

func (s *sqliteStatementsFactory) CreateCollectionGetEntityStatement(query *shared.EntityQuery) (string, []any, []any) {
	return SELECT_COLLECTION_STATEMENT, []any{&query.Resource.ID}, []any{&query.Resource.ID, &query.Resource.CreatedAt, &query.Resource.UpdatedAt, &query.Resource.Tenant, &query.Resource.Owner, &query.EntityJSON}
}
