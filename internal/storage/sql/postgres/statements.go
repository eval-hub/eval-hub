package postgres

import (
	"fmt"
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
}

func NewStatementsFactory() shared.SQLStatementsFactory {
	return &postgresStatementsFactory{}
}

func (s *postgresStatementsFactory) GetTablesSchema() string {
	return TABLES_SCHEMA
}

func (s *postgresStatementsFactory) CreateEvaluationAddEntityStatement(evaluation *api.EvaluationJobResource, entity string) (string, []any) {
	return INSERT_EVALUATION_STATEMENT, []any{evaluation.Resource.ID, evaluation.Resource.Tenant, evaluation.Resource.Owner, evaluation.Status.State, evaluation.Resource.MLFlowExperimentID, entity}
}

func (s *postgresStatementsFactory) CreateEvaluationGetEntityStatement(query *shared.EvaluationJobQuery) (string, []any, []any) {
	return SELECT_EVALUATION_STATEMENT, []any{&query.Resource.ID}, []any{&query.Resource.ID, &query.Resource.CreatedAt, &query.Resource.UpdatedAt, &query.Resource.Tenant, &query.Resource.Owner, &query.Status, &query.Resource.MLFlowExperimentID, &query.EntityJSON}
}

// allowedFilterColumns returns the set of column/param names allowed in filter for each table.
func (s *postgresStatementsFactory) GetAllowedFilterColumns(tableName string) []string {
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

// evaluationFilterCondition returns the SQL condition and args for an evaluation filter key.
// Tags supports "key" (match by key) or "key:value" (match by key and value).
func (s *postgresStatementsFactory) evaluationFilterCondition(key string, value any, index int) (condition string, args []any, nextIndex int) {
	switch key {
	case "name":
		return fmt.Sprintf("entity->'config'->'experiment'->>'name' = $%d", index), []any{value}, index + 1
	case "tags":
		tagStr, _ := value.(string)
		if keyPart, valuePart, ok := strings.Cut(tagStr, ":"); ok && valuePart != "" {
			// tags=key:value - match by both key and value
			return fmt.Sprintf("EXISTS (SELECT 1 FROM jsonb_array_elements(entity->'config'->'experiment'->'tags') AS tag WHERE tag->>'key' = $%d AND tag->>'value' = $%d)", index, index+1), []any{keyPart, valuePart}, index + 2
		}
		// tags=key - match by key only
		return fmt.Sprintf("EXISTS (SELECT 1 FROM jsonb_array_elements(entity->'config'->'experiment'->'tags') AS tag WHERE tag->>'key' = $%d)", index), []any{tagStr}, index + 1
	default:
		return fmt.Sprintf("%s = $%d", key, index), []any{value}, index + 1
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
		cond, condArgs, index = s.evaluationFilterCondition(key, filter[key], index)
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

func (s *postgresStatementsFactory) CreateProviderGetEntityStatement(query *shared.ProviderQuery) (string, []any, []any) {
	return SELECT_PROVIDER_STATEMENT, []any{&query.Resource.ID}, []any{&query.Resource.ID, &query.Resource.CreatedAt, &query.Resource.UpdatedAt, &query.Resource.Tenant, &query.Resource.Owner, &query.EntityJSON}
}

func (s *postgresStatementsFactory) CreateCollectionAddEntityStatement(collection *api.CollectionResource, entity string) (string, []any) {
	return INSERT_COLLECTION_STATEMENT, []any{collection.Resource.ID, collection.Resource.Tenant, collection.Resource.Owner, entity}
}

func (s *postgresStatementsFactory) CreateCollectionGetEntityStatement(query *shared.CollectionQuery) (string, []any, []any) {
	return SELECT_COLLECTION_STATEMENT, []any{&query.Resource.ID}, []any{&query.Resource.ID, &query.Resource.CreatedAt, &query.Resource.UpdatedAt, &query.Resource.Tenant, &query.Resource.Owner, &query.EntityJSON}
}
