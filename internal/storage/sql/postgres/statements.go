package postgres

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
	INSERT_EVALUATION_STATEMENT = `INSERT INTO evaluations (id, tenant_id, owner, status, experiment_id, entity) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id;`
	SELECT_EVALUATION_STATEMENT = `SELECT id, created_at, updated_at, tenant_id, owner, status, experiment_id, entity FROM evaluations WHERE id = $1;`

	INSERT_COLLECTION_STATEMENT = `INSERT INTO collections (id, tenant_id, owner, entity) VALUES ($1, $2, $3, $4) RETURNING id;`
	SELECT_COLLECTION_STATEMENT = `SELECT id, created_at, updated_at, tenant_id, owner, entity FROM collections WHERE id = $1;`

	INSERT_PROVIDER_STATEMENT = `INSERT INTO providers (id, tenant_id, owner, entity) VALUES ($1, $2, $3, $4) RETURNING id;`
	SELECT_PROVIDER_STATEMENT = `SELECT id, created_at, updated_at, tenant_id, owner, entity FROM providers WHERE id = $1;`

	TABLES_SCHEMA = `
CREATE TABLE IF NOT EXISTS evaluations (
    id VARCHAR(36) NOT NULL,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    tenant_id VARCHAR(255) NOT NULL,
    owner VARCHAR(255) NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    experiment_id VARCHAR(255) NOT NULL,
    entity JSONB NOT NULL,
    PRIMARY KEY (id)
);

CREATE TABLE IF NOT EXISTS collections (
    id VARCHAR(36) NOT NULL,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    tenant_id VARCHAR(255) NOT NULL,
    owner VARCHAR(255) NOT NULL,
    entity JSONB NOT NULL,
    PRIMARY KEY (id)
);

CREATE TABLE IF NOT EXISTS providers (
    id VARCHAR(36) NOT NULL,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    tenant_id VARCHAR(255) NOT NULL,
    owner VARCHAR(255) NOT NULL,
    entity JSONB NOT NULL,
    PRIMARY KEY (id)
);
`
)

type postgresStatementsFactory struct {
	logger *slog.Logger
}

func NewStatementsFactory(logger *slog.Logger) shared.SQLStatementsFactory {
	return &postgresStatementsFactory{logger: logger}
}

func (s *postgresStatementsFactory) GetTablesSchema() string {
	return TABLES_SCHEMA
}

func (s *postgresStatementsFactory) CreateEvaluationAddEntityStatement(evaluation *api.EvaluationJobResource, entity string) (string, []any) {
	return INSERT_EVALUATION_STATEMENT, []any{evaluation.Resource.ID, evaluation.Resource.Tenant, evaluation.Resource.Owner, evaluation.Status.State, evaluation.Resource.MLFlowExperimentID, entity}
}

func (s *postgresStatementsFactory) CreateEvaluationGetEntityStatement(query *shared.EntityQuery) (string, []any, []any) {
	return SELECT_EVALUATION_STATEMENT, []any{&query.Resource.ID}, []any{&query.Resource.ID, &query.Resource.CreatedAt, &query.Resource.UpdatedAt, &query.Resource.Tenant, &query.Resource.Owner, &query.Status, &query.MLFlowExperimentID, &query.EntityJSON}
}

// allowedFilterColumns returns the set of column/param names allowed in filter for each table.
func (s *postgresStatementsFactory) GetAllowedFilterColumns(tableName string) []string {
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

// entityFilterCondition returns the SQL condition and args for a filter key.
func (s *postgresStatementsFactory) entityFilterCondition(key string, value any, index int, tableName string) (condition string, args []any) {
	switch key {
	case "name":
		// name at top level
		return fmt.Sprintf("entity->>'name' = $%d", index), []any{value}
	case "experiment_id":
		return fmt.Sprintf("entity->>'resource.mlflow_experiment_id' = $%d", index), []any{value}
	case "tags":
		tagStr, _ := value.(string)
		// evaluations: tags at config.tags; providers and collections: tags at entity root
		tagsPath := "entity->'tags'"
		if tableName == shared.TABLE_EVALUATIONS {
			tagsPath = "entity->'config'->'tags'"
		}
		return fmt.Sprintf("jsonb_typeof(%s) = 'array' AND EXISTS (SELECT 1 FROM jsonb_array_elements_text(%s) AS tag WHERE tag = $%d)", tagsPath, tagsPath, index), []any{tagStr}
	default:
		return fmt.Sprintf("%s = $%d", key, index), []any{value}
	}
}

func (s *postgresStatementsFactory) createFilterStatement(filter map[string]any, orderBy string, limit int, offset int, tableName string) (string, []any) {
	var sb strings.Builder
	var args []any

	allowed := s.GetAllowedFilterColumns(tableName)
	if allowed == nil {
		return "", nil
	}
	keys := slices.Collect(maps.Keys(filter))
	sort.Strings(keys)
	index := 1
	for _, key := range keys {
		if !slices.Contains(allowed, key) {
			continue
		}
		if index > 1 {
			sb.WriteString(" AND ")
		}
		var cond string
		var condArgs []any
		cond, condArgs = s.entityFilterCondition(key, filter[key], index, tableName)
		index++
		sb.WriteString(cond)
		args = append(args, condArgs...)
	}
	filterClause := ""
	if len(args) > 0 {
		filterClause = " WHERE " + sb.String()
	}

	if orderBy != "" {
		filterClause += fmt.Sprintf(" ORDER BY %s", orderBy)
	}
	if limit > 0 {
		filterClause += fmt.Sprintf(" LIMIT $%d", index)
		args = append(args, limit)
		index++
	}
	if offset > 0 {
		filterClause += fmt.Sprintf(" OFFSET $%d", index)
		args = append(args, offset)
	}

	return filterClause, args
}

func (s *postgresStatementsFactory) CreateCountEntitiesStatement(tableName string, filter map[string]any) (string, []any) {
	filterClause, args := s.createFilterStatement(filter, "", 0, 0, tableName)
	query := fmt.Sprintf(`SELECT COUNT(*) FROM %s%s;`, tableName, filterClause)
	return query, args
}

func (s *postgresStatementsFactory) CreateListEntitiesStatement(tableName string, limit, offset int, filter map[string]any) (string, []any) {
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

func (s *postgresStatementsFactory) ScanRowForEntity(tableName string, rows *sql.Rows, query *shared.EntityQuery) error {
	switch tableName {
	case shared.TABLE_EVALUATIONS:
		return rows.Scan(&query.Resource.ID, &query.Resource.CreatedAt, &query.Resource.UpdatedAt, &query.Resource.Tenant, &query.Resource.Owner, &query.Status, &query.MLFlowExperimentID, &query.EntityJSON)
	default:
		return rows.Scan(&query.Resource.ID, &query.Resource.CreatedAt, &query.Resource.UpdatedAt, &query.Resource.Tenant, &query.Resource.Owner, &query.EntityJSON)
	}
}

func (s *postgresStatementsFactory) CreateCheckEntityExistsStatement(tableName string) string {
	return fmt.Sprintf(`SELECT id, status FROM %s WHERE id = $1;`, tableName)
}

func (s *postgresStatementsFactory) CreateDeleteEntityStatement(tableName string) string {
	return fmt.Sprintf(`DELETE FROM %s WHERE id = $1;`, tableName)
}

func (s *postgresStatementsFactory) CreateUpdateEntityStatement(tableName, id string, entityJSON string, status *api.OverallState) (string, []any) {
	// UPDATE "evaluations" SET "status" = ?, "entity" = ?, "updated_at" = CURRENT_TIMESTAMP WHERE "id" = ?;
	switch tableName {
	case shared.TABLE_EVALUATIONS:
		return fmt.Sprintf(`UPDATE %s SET status = $1, entity = $2, updated_at = CURRENT_TIMESTAMP WHERE id = $3;`, tableName), []any{*status, entityJSON, id}
	default:
		return fmt.Sprintf(`UPDATE %s SET entity = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2;`, tableName), []any{entityJSON, id}
	}
}

func (s *postgresStatementsFactory) CreateProviderAddEntityStatement(provider *api.ProviderResource, entity string) (string, []any) {
	return INSERT_PROVIDER_STATEMENT, []any{provider.Resource.ID, provider.Resource.Tenant, provider.Resource.Owner, entity}
}

func (s *postgresStatementsFactory) CreateProviderGetEntityStatement(query *shared.EntityQuery) (string, []any, []any) {
	return SELECT_PROVIDER_STATEMENT, []any{&query.Resource.ID}, []any{&query.Resource.ID, &query.Resource.CreatedAt, &query.Resource.UpdatedAt, &query.Resource.Tenant, &query.Resource.Owner, &query.EntityJSON}
}

func (s *postgresStatementsFactory) CreateCollectionAddEntityStatement(collection *api.CollectionResource, entity string) (string, []any) {
	return INSERT_COLLECTION_STATEMENT, []any{collection.Resource.ID, collection.Resource.Tenant, collection.Resource.Owner, entity}
}

func (s *postgresStatementsFactory) CreateCollectionGetEntityStatement(query *shared.EntityQuery) (string, []any, []any) {
	return SELECT_COLLECTION_STATEMENT, []any{&query.Resource.ID}, []any{&query.Resource.ID, &query.Resource.CreatedAt, &query.Resource.UpdatedAt, &query.Resource.Tenant, &query.Resource.Owner, &query.EntityJSON}
}
