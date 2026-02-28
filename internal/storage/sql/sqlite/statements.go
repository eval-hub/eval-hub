package sqlite

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/eval-hub/eval-hub/internal/storage/sql/shared"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// allowedFilterColumns returns the set of column names allowed in filter for each table.
func allowedFilterColumns(tableName string) map[string]struct{} {
	switch tableName {
	case shared.TABLE_EVALUATIONS:
		return map[string]struct{}{"tenant_id": {}, "status": {}, "experiment_id": {}}
	case shared.TABLE_COLLECTIONS, shared.TABLE_PROVIDERS:
		return map[string]struct{}{"tenant_id": {}}
	default:
		return nil
	}
}

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

// createFilterStatement builds a WHERE clause and args from the filter.
// It validates each key against the table's allowlist, sorts keys deterministically,
// and returns both the clause and args in matching order.
func (s *sqliteStatementsFactory) createFilterStatement(filter map[string]any, orderBy string, limit int, offset int, tableName string) (string, []any) {
	var args []any
	var sb strings.Builder

	if len(filter) > 0 {
		allowed := allowedFilterColumns(tableName)
		if allowed == nil {
			return "", nil
		}
		keys := slices.Collect(maps.Keys(filter))
		sort.Strings(keys)
		validKeys := make([]string, 0, len(keys))
		for _, key := range keys {
			if _, ok := allowed[key]; ok {
				validKeys = append(validKeys, key)
				args = append(args, filter[key])
			}
		}
		if len(validKeys) > 0 {
			sb.WriteString(" WHERE ")
			for i, key := range validKeys {
				if i > 0 {
					sb.WriteString(" AND ")
				}
				sb.WriteString(fmt.Sprintf("%s = ?", key))
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
		query = fmt.Sprintf(`SELECT id, created_at, updated_at, tenant_id, status, experiment_id, entity FROM %s%s;`, tableName, filterClause)
	default:
		query = fmt.Sprintf(`SELECT id, created_at, updated_at, tenant_id, entity FROM %s%s;`, tableName, filterClause)
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
