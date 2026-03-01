package postgres

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/eval-hub/eval-hub/internal/storage/sql/shared"
	"github.com/eval-hub/eval-hub/pkg/api"
)

const (
	INSERT_EVALUATION_STATEMENT = `INSERT INTO evaluations (id, tenant_id, status, experiment_id, entity) VALUES ($1, $2, $3, $4, $5) RETURNING id;`
	SELECT_EVALUATION_STATEMENT = `SELECT id, created_at, updated_at, tenant_id, status, experiment_id, entity FROM evaluations WHERE id = $1;`

	INSERT_COLLECTION_STATEMENT = `INSERT INTO collections (id, tenant_id, entity) VALUES ($1, $2, $3) RETURNING id;`
	SELECT_COLLECTION_STATEMENT = `SELECT id, created_at, updated_at, tenant_id, entity FROM collections WHERE id = $1;`

	INSERT_PROVIDER_STATEMENT = `INSERT INTO providers (id, tenant_id, entity) VALUES ($1, $2, $3) RETURNING id;`
	SELECT_PROVIDER_STATEMENT = `SELECT id, created_at, updated_at, tenant_id, entity FROM providers WHERE id = $1;`

	TABLES_SCHEMA = `
CREATE TABLE IF NOT EXISTS evaluations (
    id VARCHAR(36) NOT NULL,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    tenant_id VARCHAR(255) NOT NULL,
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
    entity JSONB NOT NULL,
    PRIMARY KEY (id)
);

CREATE TABLE IF NOT EXISTS providers (
    id VARCHAR(36) NOT NULL,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    tenant_id VARCHAR(255) NOT NULL,
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
	return INSERT_EVALUATION_STATEMENT, []any{evaluation.Resource.ID, evaluation.Resource.Tenant, evaluation.Status.State, evaluation.Resource.MLFlowExperimentID, entity}
}

func (s *postgresStatementsFactory) CreateEvaluationGetEntityStatement(query *shared.EvaluationJobQuery) (string, []any, []any) {
	return SELECT_EVALUATION_STATEMENT, []any{&query.ID}, []any{&query.ID, &query.CreatedAt, &query.UpdatedAt, &query.Tenant, &query.Status, &query.ExperimentID, &query.EntityJSON}
}

func (s *postgresStatementsFactory) createFilterStatement(filter map[string]any, orderBy string, limit int, offset int) string {
	var sb strings.Builder

	index := 1

	if len(filter) > 0 {
		sb.WriteString(" WHERE ")
		for key := range maps.Keys(filter) {
			if index > 1 {
				sb.WriteString(" AND ")
			}
			sb.WriteString(fmt.Sprintf("%s = $%d", key, index))
			index++
		}
	}

	// ORDER BY id DESC LIMIT $2 OFFSET $3
	if orderBy != "" {
		// note that we use the value here and not $n
		sb.WriteString(fmt.Sprintf(" ORDER BY %s", orderBy))
	}
	if limit > 0 {
		sb.WriteString(fmt.Sprintf(" LIMIT $%d", index))
		index++
	}
	if offset > 0 {
		sb.WriteString(fmt.Sprintf(" OFFSET $%d", index))
		index++
	}

	return sb.String()
}

func (s *postgresStatementsFactory) CreateCountEntitiesStatement(tableName string, filter map[string]any) (string, []any) {
	filterStatement := s.createFilterStatement(filter, "", 0, 0)
	query := fmt.Sprintf(`SELECT COUNT(*) FROM %s%s;`, tableName, filterStatement)
	args := slices.Collect(maps.Values(filter))
	return query, args
}

func (s *postgresStatementsFactory) CreateListEntitiesStatement(tableName string, limit, offset int, filter map[string]any) (string, []any) {
	filterStatement := s.createFilterStatement(filter, "id DESC", limit, offset)

	var query string
	var args = slices.Collect(maps.Values(filter))
	if limit > 0 {
		args = append(args, limit)
	}
	if offset > 0 {
		args = append(args, offset)
	}

	switch tableName {
	case shared.TABLE_EVALUATIONS:
		query = fmt.Sprintf(`SELECT id, created_at, updated_at, tenant_id, status, experiment_id, entity FROM %s %s;`, tableName, filterStatement)
	default:
		query = fmt.Sprintf(`SELECT id, created_at, updated_at, tenant_id, entity FROM %s %s;`, tableName, filterStatement)
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
	return INSERT_PROVIDER_STATEMENT, []any{provider.Resource.ID, provider.Resource.Tenant, entity}
}

func (s *postgresStatementsFactory) CreateProviderGetEntityStatement(query *shared.ProviderQuery) (string, []any, []any) {
	return SELECT_PROVIDER_STATEMENT, []any{&query.ID}, []any{&query.ID, &query.CreatedAt, &query.UpdatedAt, &query.Tenant, &query.EntityJSON}
}

func (s *postgresStatementsFactory) CreateCollectionAddEntityStatement(collection *api.CollectionResource, entity string) (string, []any) {
	return INSERT_COLLECTION_STATEMENT, []any{collection.Resource.ID, collection.Resource.Tenant, entity}
}

func (s *postgresStatementsFactory) CreateCollectionGetEntityStatement(query *shared.CollectionQuery) (string, []any, []any) {
	return SELECT_COLLECTION_STATEMENT, []any{&query.ID}, []any{&query.ID, &query.CreatedAt, &query.UpdatedAt, &query.Tenant, &query.EntityJSON}
}
