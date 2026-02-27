package sqlite

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/pkg/api"
)

const INSERT_EVALUATION_STATEMENT = `INSERT INTO evaluations (id, tenant_id, status, experiment_id, entity) VALUES (?, ?, ?, ?, ?);`
const INSERT_COLLECTION_STATEMENT = `INSERT INTO collections (id, tenant_id, entity) VALUES (?, ?, ?);`
const INSERT_PROVIDER_STATEMENT = `INSERT INTO providers (id, tenant_id, entity) VALUES (?, ?, ?);`

const GET_EVALUATION_STATEMENT = `SELECT id, created_at, updated_at, tenant_id, status, experiment_id, entity FROM evaluations WHERE id = ?;`
const GET_COLLECTION_STATEMENT = `SELECT id, created_at, updated_at, tenant_id, entity FROM collections WHERE id = ?;`
const GET_PROVIDER_STATEMENT = `SELECT id, created_at, updated_at, tenant_id, entity FROM providers WHERE id = ?;`

const LIST_EVALUATIONS_STATEMENT = `SELECT id, created_at, updated_at, tenant_id, status, experiment_id, entity FROM evaluations %s;`
const LIST_COLLECTIONS_STATEMENT = `SELECT id, created_at, updated_at, tenant_id, entity FROM collections %s;`
const LIST_PROVIDERS_STATEMENT = `SELECT id, created_at, updated_at, tenant_id, entity FROM providers %s;`

const UPDATE_STATEMENT = `UPDATE %s SET %s WHERE id = ?;`
const CHECK_ENTITY_EXISTS_STATEMENT = `SELECT id FROM %s WHERE id = ?;`
const DELETE_STATEMENT = `DELETE FROM %s WHERE id = ?;`
const COUNT_STATEMENT = `SELECT COUNT(*) FROM %s %s;`

// createCheckEntityExistsStatement returns a driver-specific SELECT statement
// to check if an entity exists by ID and retrieve its status
func createCheckEntityExistsStatement(tableName string) string {
	return fmt.Sprintf(CHECK_ENTITY_EXISTS_STATEMENT, tableName)
}

// createDeleteEntityStatement returns a driver-specific DELETE statement
// to delete an entity by ID
func createDeleteEntityStatement(tableName string) string {
	return fmt.Sprintf(DELETE_STATEMENT, tableName)
}

// createCountEntitiesStatement returns a driver-specific COUNT statement
// to count total entities in the table, optionally filtered by the given parameters
func createCountEntitiesStatement(tableName string, filter map[string]any) (string, []any, error) {

	filterStatement, err := createFilterStatement(filter, "", 0, 0)
	if err != nil {
		return "", nil, err
	}

	var query string
	var args []any

	query = fmt.Sprintf(COUNT_STATEMENT, tableName, filterStatement)
	args = slices.Collect(maps.Values(filter))

	return query, args, nil
}

func getParams(params abstractions.QueryFilter) map[string]any {
	filter := maps.Clone(params.Params)
	maps.DeleteFunc(filter, func(k string, v any) bool {
		return v == "" // delete empty values
	})
	return filter
}

func createFilterStatement(filter map[string]any, orderBy string, limit int, offset int) (string, error) {

	var sb strings.Builder

	index := 0

	if len(filter) > 0 {
		sb.WriteString(" WHERE ")
		for key := range filter {
			if index > 0 {
				sb.WriteString(" AND ")
			}
			fmt.Fprintf(&sb, "%s = ?", key)
			index++
		}
	}

	// ORDER BY id DESC LIMIT $2 OFFSET $3
	if orderBy != "" {
		// note that we use the value here and not ?
		fmt.Fprintf(&sb, " ORDER BY %s", orderBy)
	}
	if limit > 0 {
		sb.WriteString(" LIMIT ?")
	}
	if offset > 0 {
		sb.WriteString(" OFFSET ?")
	}

	return sb.String(), nil
}

// createListEntitiesStatement returns a driver-specific SELECT statement
// to list entities with pagination (LIMIT and OFFSET), optionally filtered by status
func createListEntitiesStatement(tableName string, limit, offset int, filter map[string]any) (string, []any, error) {

	filterStatement, err := createFilterStatement(filter, "id DESC", limit, offset)
	if err != nil {
		return "", nil, err
	}

	var query string
	var args = slices.Collect(maps.Values(filter))
	if limit > 0 {
		args = append(args, limit)
	}
	if offset > 0 {
		args = append(args, offset)
	}

	switch tableName {
	case TABLE_EVALUATIONS:
		query = fmt.Sprintf(LIST_EVALUATIONS_STATEMENT, filterStatement)
	case TABLE_COLLECTIONS:
		query = fmt.Sprintf(LIST_COLLECTIONS_STATEMENT, filterStatement)
	case TABLE_PROVIDERS:
		query = fmt.Sprintf(LIST_PROVIDERS_STATEMENT, filterStatement)
	default:
		return "", nil, fmt.Errorf("unsupported table: %s", tableName)
	}

	return query, args, nil
}

func createUpdateEvaluationStatement(id string, status api.OverallState, entityJSON string) (string, []any) {

	args := []any{entityJSON}
	set := "entity = $1, updated_at = CURRENT_TIMESTAMP"
	if status != "" {
		set += ", status = $2"
		args = append(args, status)

	}
	query := fmt.Sprintf(UPDATE_STATEMENT, "evaluations", set)
	args = append(args, id)
	return query, args
}

func createUpdateCollectionStatement(id string, entityJSON string) (string, []any) {
	query := fmt.Sprintf(UPDATE_STATEMENT, "collections", "entity = ?, updated_at = CURRENT_TIMESTAMP")
	args := []any{entityJSON, id}
	return query, args
}

func createUpdateProviderStatement(id string, entityJSON string) (string, []any) {
	query := fmt.Sprintf(UPDATE_STATEMENT, "providers", "entity = ?, updated_at = CURRENT_TIMESTAMP")
	args := []any{entityJSON, id}
	return query, args
}

// Returns the limit, offset, and filtered params
func extractQueryParams(filter abstractions.QueryFilter) abstractions.QueryFilter {
	params := getParams(filter)
	// TODO - remove this delete after adding owner in storage layer
	delete(params, "owner")
	return abstractions.QueryFilter{
		Limit:  filter.Limit,
		Offset: filter.Offset,
		Params: params,
	}
}
