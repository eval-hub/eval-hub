package sqlite

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/eval-hub/eval-hub/internal/storage/sql/shared"
	"github.com/eval-hub/eval-hub/pkg/api"
)

const (
	INSERT_EVALUATION_STATEMENT = `INSERT INTO evaluations (id, tenant_id, status, experiment_id, entity) VALUES (?, ?, ?, ?, ?);`
	SELECT_EVALUATION_STATEMENT = `SELECT id, created_at, updated_at, tenant_id, status, experiment_id, entity FROM evaluations WHERE id = ?;`

	INSERT_COLLECTION_STATEMENT = `INSERT INTO collections (id, tenant_id, entity) VALUES (?, ?, ?);`

	INSERT_PROVIDER_STATEMENT = `INSERT INTO providers (id, tenant_id, entity) VALUES (?, ?, ?);`
	SELECT_PROVIDER_STATEMENT = `SELECT id, created_at, updated_at, tenant_id, entity FROM providers WHERE id = ?;`
)

type sqliteStatementsFactory struct {
}

func NewStatementsFactory() shared.SQLStatementsFactory {
	return &sqliteStatementsFactory{}
}

func (s *sqliteStatementsFactory) CreateEvaluationAddEntityStatement(evaluation *api.EvaluationJobResource, entity string) (string, []any) {
	return INSERT_EVALUATION_STATEMENT, []any{evaluation.Resource.ID, evaluation.Resource.Tenant, evaluation.Status.State, evaluation.Resource.MLFlowExperimentID, entity}
}

func (s *sqliteStatementsFactory) CreateEvaluationGetEntityStatement(query *shared.EvaluationJobQuery) (string, []any, []any) {
	return SELECT_EVALUATION_STATEMENT, []any{&query.ID}, []any{&query.ID, &query.CreatedAt, &query.UpdatedAt, &query.Tenant, &query.Status, &query.ExperimentID, &query.EntityJSON}
}

func (s *sqliteStatementsFactory) createFilterStatement(filter map[string]any, orderBy string, limit int, offset int) string {
	var sb strings.Builder

	if len(filter) > 0 {
		first := true
		sb.WriteString(" WHERE ")
		for key := range maps.Keys(filter) {
			if !first {
				sb.WriteString(" AND ")
			}
			sb.WriteString(fmt.Sprintf("%s = ?", key))
			first = false
		}
	}

	// ORDER BY id DESC LIMIT $2 OFFSET $3
	if orderBy != "" {
		// note that we use the value here and not ?
		sb.WriteString(fmt.Sprintf(" ORDER BY %s", orderBy))
	}
	if limit > 0 {
		sb.WriteString(" LIMIT ?")
	}
	if offset > 0 {
		sb.WriteString(" OFFSET ?")
	}

	return sb.String()
}

func (s *sqliteStatementsFactory) CreateCountEntitiesStatement(tableName string, filter map[string]any) (string, []any) {
	filterStatement := s.createFilterStatement(filter, "", 0, 0)
	query := fmt.Sprintf(`SELECT COUNT(*) FROM %s%s;`, tableName, filterStatement)
	args := slices.Collect(maps.Values(filter))
	return query, args
}

func (s *sqliteStatementsFactory) CreateListEntitiesStatement(tableName string, limit, offset int, filter map[string]any) (string, []any) {
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
	return INSERT_PROVIDER_STATEMENT, []any{provider.Resource.ID, provider.Resource.Tenant, entity}
}

func (s *sqliteStatementsFactory) CreateProviderGetEntityStatement(query *shared.ProviderQuery) (string, []any, []any) {
	return SELECT_PROVIDER_STATEMENT, []any{&query.ID}, []any{&query.ID, &query.CreatedAt, &query.UpdatedAt, &query.Tenant, &query.EntityJSON}
}
