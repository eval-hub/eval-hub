package postgres

import (
	"database/sql"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"

	"github.com/eval-hub/eval-hub/internal/storage/sql/shared"
	"github.com/eval-hub/eval-hub/pkg/api"
)

const (
	INSERT_EVALUATION_STATEMENT = `INSERT INTO evaluations (id, tenant_id, owner, status, experiment_id, entity) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id;`

	INSERT_COLLECTION_STATEMENT = `INSERT INTO collections (id, tenant_id, owner, entity) VALUES ($1, $2, $3, $4) RETURNING id;`

	INSERT_PROVIDER_STATEMENT = `INSERT INTO providers (id, tenant_id, owner, entity) VALUES ($1, $2, $3, $4) RETURNING id;`

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
	// SELECT id, created_at, updated_at, tenant_id, owner, status, experiment_id, entity FROM evaluations WHERE id = $1;
	if query.Resource.Tenant.IsEmpty() {
		return `SELECT id, created_at, updated_at, tenant_id, owner, status, experiment_id, entity FROM evaluations WHERE id = $1;`, []any{&query.Resource.ID}, []any{&query.Resource.ID, &query.Resource.CreatedAt, &query.Resource.UpdatedAt, &query.Resource.Tenant, &query.Resource.Owner, &query.Status, &query.MLFlowExperimentID, &query.EntityJSON}
	}
	return `SELECT id, created_at, updated_at, tenant_id, owner, status, experiment_id, entity FROM evaluations WHERE id = $1 AND tenant_id = $2;`, []any{&query.Resource.ID, query.Resource.Tenant.String()}, []any{&query.Resource.ID, &query.Resource.CreatedAt, &query.Resource.UpdatedAt, &query.Resource.Tenant, &query.Resource.Owner, &query.Status, &query.MLFlowExperimentID, &query.EntityJSON}
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
		// evaluations: name at config.name; providers and collections: name at entity root
		namePath := "entity->>'name'"
		if tableName == shared.TABLE_EVALUATIONS {
			namePath = "entity->'config'->>'name'"
		}
		return fmt.Sprintf("%s = $%d", namePath, index), []any{value}
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

func (s *postgresStatementsFactory) createFilterStatement(tenant api.Tenant, filter map[string]any, orderBy string, limit int, offset int, tableName string) (string, []any) {
	var sb strings.Builder
	var args []any

	index := 1
	and := false

	// we must always filter by tenant_id if it exists
	if !tenant.IsEmpty() {
		sb.WriteString(" WHERE ")
		cond, condArgs := s.entityFilterCondition("tenant_id", tenant.String(), index, tableName)
		index++
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
				cond, condArgs := s.entityFilterCondition(key, value, index, tableName)
				index++
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
		sb.WriteString(" LIMIT $")
		sb.WriteString(strconv.Itoa(index))
		index++
		args = append(args, limit)
	}
	if offset > 0 {
		sb.WriteString(" OFFSET $")
		sb.WriteString(strconv.Itoa(index))
		index++
		args = append(args, offset)
	}

	return sb.String(), args
}

func (s *postgresStatementsFactory) CreateCountEntitiesStatement(tenant api.Tenant, tableName string, filter map[string]any) (string, []any) {
	filterClause, args := s.createFilterStatement(tenant, filter, "", 0, 0, tableName)
	query := fmt.Sprintf(`SELECT COUNT(*) FROM %s%s;`, tableName, filterClause)
	return query, args
}

func (s *postgresStatementsFactory) CreateListEntitiesStatement(tenant api.Tenant, tableName string, limit, offset int, filter map[string]any) (string, []any) {
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

func (s *postgresStatementsFactory) ScanRowForEntity(tenant api.Tenant, tableName string, rows *sql.Rows, query *shared.EntityQuery) error {
	switch tableName {
	case shared.TABLE_EVALUATIONS:
		return rows.Scan(&query.Resource.ID, &query.Resource.CreatedAt, &query.Resource.UpdatedAt, &query.Resource.Tenant, &query.Resource.Owner, &query.Status, &query.MLFlowExperimentID, &query.EntityJSON)
	default:
		return rows.Scan(&query.Resource.ID, &query.Resource.CreatedAt, &query.Resource.UpdatedAt, &query.Resource.Tenant, &query.Resource.Owner, &query.EntityJSON)
	}
}

func (s *postgresStatementsFactory) CreateCheckEntityExistsStatement(tenant api.Tenant, tableName string, id string) (string, []any) {
	if !tenant.IsEmpty() {
		return fmt.Sprintf(`SELECT id, status FROM %s WHERE id = $1 AND tenant_id = $2;`, tableName), []any{id, tenant.String()}
	}
	return fmt.Sprintf(`SELECT id, status FROM %s WHERE id = $1;`, tableName), []any{id}
}

func (s *postgresStatementsFactory) CreateDeleteEntityStatement(tenant api.Tenant, tableName string, id string) (string, []any) {
	if !tenant.IsEmpty() {
		return fmt.Sprintf(`DELETE FROM %s WHERE id = $1 AND tenant_id = $2;`, tableName), []any{id, tenant.String()}
	}
	return fmt.Sprintf(`DELETE FROM %s WHERE id = $1;`, tableName), []any{id}
}

func (s *postgresStatementsFactory) CreateUpdateEntityStatement(tenant api.Tenant, tableName, id string, entityJSON string, status *api.OverallState) (string, []any) {
	// UPDATE "evaluations" SET "status" = ?, "entity" = ?, "updated_at" = CURRENT_TIMESTAMP WHERE "id" = ?;
	switch tableName {
	case shared.TABLE_EVALUATIONS:
		if !tenant.IsEmpty() {
			return fmt.Sprintf(`UPDATE %s SET status = $1, entity = $2, updated_at = CURRENT_TIMESTAMP WHERE id = $3 AND tenant_id = $4;`, tableName), []any{*status, entityJSON, id, tenant.String()}
		}
		return fmt.Sprintf(`UPDATE %s SET status = $1, entity = $2, updated_at = CURRENT_TIMESTAMP WHERE id = $3;`, tableName), []any{*status, entityJSON, id}
	default:
		if !tenant.IsEmpty() {
			return fmt.Sprintf(`UPDATE %s SET entity = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2 AND tenant_id = $3;`, tableName), []any{entityJSON, id, tenant.String()}
		}
		return fmt.Sprintf(`UPDATE %s SET entity = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2;`, tableName), []any{entityJSON, id}
	}
}

func (s *postgresStatementsFactory) CreateProviderAddEntityStatement(provider *api.ProviderResource, entity string) (string, []any) {
	return INSERT_PROVIDER_STATEMENT, []any{provider.Resource.ID, provider.Resource.Tenant, provider.Resource.Owner, entity}
}

func (s *postgresStatementsFactory) CreateProviderGetEntityStatement(query *shared.EntityQuery) (string, []any, []any) {
	// SELECT id, created_at, updated_at, tenant_id, owner, entity FROM providers WHERE id = $1;
	if query.Resource.Tenant.IsEmpty() {
		return `SELECT id, created_at, updated_at, tenant_id, owner, entity FROM providers WHERE id = $1;`, []any{&query.Resource.ID}, []any{&query.Resource.ID, &query.Resource.CreatedAt, &query.Resource.UpdatedAt, &query.Resource.Tenant, &query.Resource.Owner, &query.EntityJSON}
	}
	return `SELECT id, created_at, updated_at, tenant_id, owner, entity FROM providers WHERE id = $1 AND tenant_id = $2;`, []any{&query.Resource.ID, query.Resource.Tenant.String()}, []any{&query.Resource.ID, &query.Resource.CreatedAt, &query.Resource.UpdatedAt, &query.Resource.Tenant, &query.Resource.Owner, &query.EntityJSON}
}

func (s *postgresStatementsFactory) CreateCollectionAddEntityStatement(collection *api.CollectionResource, entity string) (string, []any) {
	return INSERT_COLLECTION_STATEMENT, []any{collection.Resource.ID, collection.Resource.Tenant, collection.Resource.Owner, entity}
}

func (s *postgresStatementsFactory) CreateCollectionGetEntityStatement(query *shared.EntityQuery) (string, []any, []any) {
	// SELECT id, created_at, updated_at, tenant_id, owner, entity FROM collections WHERE id = $1;
	if query.Resource.Tenant.IsEmpty() {
		return `SELECT id, created_at, updated_at, tenant_id, owner, entity FROM collections WHERE id = $1;`, []any{&query.Resource.ID}, []any{&query.Resource.ID, &query.Resource.CreatedAt, &query.Resource.UpdatedAt, &query.Resource.Tenant, &query.Resource.Owner, &query.EntityJSON}
	}
	return `SELECT id, created_at, updated_at, tenant_id, owner, entity FROM collections WHERE id = $1 AND tenant_id = $2;`, []any{&query.Resource.ID, query.Resource.Tenant.String()}, []any{&query.Resource.ID, &query.Resource.CreatedAt, &query.Resource.UpdatedAt, &query.Resource.Tenant, &query.Resource.Owner, &query.EntityJSON}
}
