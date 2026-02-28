package sql

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// TODO - do we want to pull out all the SQL statements like this or leave them in the functions?

// SQLite: use ? placeholders
const SQLITE_INSERT_EVALUATION_STATEMENT = `INSERT INTO evaluations (id, tenant_id, status, experiment_id, entity) VALUES (?, ?, ?, ?, ?);`

// PostgreSQL: use $1, $2 placeholders and RETURNING id clause
const POSTGRES_INSERT_EVALUATION_STATEMENT = `INSERT INTO evaluations (id, tenant_id, status, experiment_id, entity) VALUES ($1, $2, $3, $4, $5) RETURNING id;`

// SQLite: use ? placeholders
const SQLITE_INSERT_COLLECTION_STATEMENT = `INSERT INTO collections (id, tenant_id, entity) VALUES (?, ?, ?);`

// PostgreSQL: use $1, $2 placeholders and RETURNING id clause
const POSTGRES_INSERT_COLLECTION_STATEMENT = `INSERT INTO collections (id, tenant_id, entity) VALUES ($1, $2, $3) RETURNING id;`

// SQLite: use ? placeholders
const SQLITE_INSERT_PROVIDER_STATEMENT = `INSERT INTO providers (id, tenant_id, entity) VALUES (?, ?, ?);`

// PostgreSQL: use $1, $2 placeholders and RETURNING id clause
const POSTGRES_INSERT_PROVIDER_STATEMENT = `INSERT INTO providers (id, tenant_id, entity) VALUES ($1, $2, $3) RETURNING id;`

func getUnsupportedDriverError(driver string) error {
	return fmt.Errorf("unsupported driver: %s", driver)
}

func schemasForDriver(driver string) (string, error) {
	switch driver {
	case SQLITE_DRIVER:
		// better to be safe than sorry
		return strings.ReplaceAll(SQLITE_SCHEMA, "pending", string(api.StatePending)), nil
	case POSTGRES_DRIVER:
		// better to be safe than sorry
		return strings.ReplaceAll(POSTGRES_SCHEMA, "pending", string(api.StatePending)), nil
	default:
		return "", getUnsupportedDriverError(driver)
	}
}

// createAddEntityStatement returns a driver-specific INSERT statement
// with properly quoted table name and appropriate placeholder syntax
func createAddEntityStatement(driver, tableName string) (string, error) {
	switch driver + tableName {
	case POSTGRES_DRIVER + TABLE_EVALUATIONS:
		return POSTGRES_INSERT_EVALUATION_STATEMENT, nil
	case SQLITE_DRIVER + TABLE_EVALUATIONS:
		// SQLite: use ? placeholders
		return SQLITE_INSERT_EVALUATION_STATEMENT, nil
	case POSTGRES_DRIVER + TABLE_COLLECTIONS:
		return POSTGRES_INSERT_COLLECTION_STATEMENT, nil
	case SQLITE_DRIVER + TABLE_COLLECTIONS:
		// SQLite: use ? placeholders
		return SQLITE_INSERT_COLLECTION_STATEMENT, nil
	case POSTGRES_DRIVER + TABLE_PROVIDERS:
		return POSTGRES_INSERT_PROVIDER_STATEMENT, nil
	case SQLITE_DRIVER + TABLE_PROVIDERS:
		return SQLITE_INSERT_PROVIDER_STATEMENT, nil
	default:
		return "", getUnsupportedDriverError(driver)
	}
}

// quoteIdentifier properly quotes an identifier for the given driver
func quoteIdentifier(_ /*driver*/ string, identifier string) string {
	// Escape double quotes by doubling them
	escaped := strings.ReplaceAll(identifier, `"`, `""`)
	return fmt.Sprintf(`"%s"`, escaped)
}

// createGetEntityStatement returns a driver-specific SELECT statement
// to retrieve an entity by ID
func createGetEntityStatement(driver, tableName string) (string, error) {
	quotedTable := quoteIdentifier(driver, tableName)

	switch driver + tableName {
	case POSTGRES_DRIVER + TABLE_EVALUATIONS:
		return fmt.Sprintf(`SELECT id, created_at, updated_at, tenant_id, status, experiment_id, entity FROM %s WHERE id = $1;`, quotedTable), nil
	case SQLITE_DRIVER + TABLE_EVALUATIONS:
		// SQLite: use ? placeholder
		return fmt.Sprintf(`SELECT id, created_at, updated_at, tenant_id, status, experiment_id, entity FROM %s WHERE id = ?;`, quotedTable), nil
	case POSTGRES_DRIVER + TABLE_COLLECTIONS:
		return fmt.Sprintf(`SELECT id, created_at, updated_at, tenant_id, entity FROM %s WHERE id = $1;`, quotedTable), nil
	case SQLITE_DRIVER + TABLE_COLLECTIONS:
		// SQLite: use ? placeholder
		return fmt.Sprintf(`SELECT id, created_at, updated_at, tenant_id, entity FROM %s WHERE id = ?;`, quotedTable), nil
	case POSTGRES_DRIVER + TABLE_PROVIDERS:
		return fmt.Sprintf(`SELECT id, created_at, updated_at, tenant_id, entity FROM %s WHERE id = $1;`, quotedTable), nil
	case SQLITE_DRIVER + TABLE_PROVIDERS:
		return fmt.Sprintf(`SELECT id, created_at, updated_at, tenant_id, entity FROM %s WHERE id = ?;`, quotedTable), nil
	default:
		return "", getUnsupportedDriverError(driver)
	}
}

// createCheckEntityExistsStatement returns a driver-specific SELECT statement
// to check if an entity exists by ID and retrieve its status
func createCheckEntityExistsStatement(driver, tableName string) (string, error) {
	quotedTable := quoteIdentifier(driver, tableName)

	switch driver {
	case POSTGRES_DRIVER:
		return fmt.Sprintf(`SELECT id, status FROM %s WHERE id = $1;`, quotedTable), nil
	case SQLITE_DRIVER:
		// SQLite: use ? placeholder
		return fmt.Sprintf(`SELECT id, status FROM %s WHERE id = ?;`, quotedTable), nil
	default:
		return "", getUnsupportedDriverError(driver)
	}
}

// createDeleteEntityStatement returns a driver-specific DELETE statement
// to delete an entity by ID
func createDeleteEntityStatement(driver, tableName string) (string, error) {
	quotedTable := quoteIdentifier(driver, tableName)

	switch driver {
	case POSTGRES_DRIVER:
		// PostgreSQL: use $1 placeholder
		return fmt.Sprintf(`DELETE FROM %s WHERE id = $1;`, quotedTable), nil
	case SQLITE_DRIVER:
		// SQLite: use ? placeholder
		return fmt.Sprintf(`DELETE FROM %s WHERE id = ?;`, quotedTable), nil
	default:
		return "", getUnsupportedDriverError(driver)
	}
}

// createCountEntitiesStatement returns a driver-specific COUNT statement
// to count total entities in the table, optionally filtered by the given parameters
func createCountEntitiesStatement(driver, tableName string, filter map[string]any) (string, []any, error) {
	quotedTable := quoteIdentifier(driver, tableName)

	filterStatement, err := createFilterStatement(driver, filter, "", 0, 0)
	if err != nil {
		return "", nil, err
	}

	var query string
	var args []any

	switch driver {
	case POSTGRES_DRIVER:
		query = fmt.Sprintf(`SELECT COUNT(*) FROM %s%s;`, quotedTable, filterStatement)
		args = slices.Collect(maps.Values(filter))
	case SQLITE_DRIVER:
		query = fmt.Sprintf(`SELECT COUNT(*) FROM %s%s;`, quotedTable, filterStatement)
		args = slices.Collect(maps.Values(filter))
	default:
		return "", nil, getUnsupportedDriverError(driver)
	}

	return query, args, nil
}

func getParam(driver string) string {
	switch driver {
	case POSTGRES_DRIVER:
		return "$"
	case SQLITE_DRIVER:
		return "?"
	default:
		return "?"
	}
}

func getParamValue(param string, index int) string {
	if param == "$" {
		// PostgreSQL placeholders are 1-based: $1, $2, ...
		return fmt.Sprintf("$%d", index+1)
	}
	return param
}

func getParams(params *abstractions.QueryFilter) map[string]any {
	filter := maps.Clone(params.Params)
	maps.DeleteFunc(filter, func(k string, v any) bool {
		return v == "" // delete empty values
	})
	return filter
}

func createFilterStatement(driver string, filter map[string]any, orderBy string, limit int, offset int) (string, error) {
	param := getParam(driver)

	var sb strings.Builder

	index := 0

	if len(filter) > 0 {
		sb.WriteString(" WHERE ")
		for key := range maps.Keys(filter) {
			if index > 0 {
				sb.WriteString(" AND ")
			}
			sb.WriteString(fmt.Sprintf("%s = %s", key, getParamValue(param, index)))
			index++
		}
	}

	// ORDER BY id DESC LIMIT $2 OFFSET $3
	if orderBy != "" {
		// note that we use the value here and not ?
		sb.WriteString(fmt.Sprintf(" ORDER BY %s", orderBy))
	}
	if limit > 0 {
		sb.WriteString(fmt.Sprintf(" LIMIT %s", getParamValue(param, index)))
		index++
	}
	if offset > 0 {
		sb.WriteString(fmt.Sprintf(" OFFSET %s", getParamValue(param, index)))
		index++
	}

	return sb.String(), nil
}

// createListEntitiesStatement returns a driver-specific SELECT statement
// to list entities with pagination (LIMIT and OFFSET), optionally filtered by status
func createListEntitiesStatement(driver, tableName string, limit, offset int, filter map[string]any) (string, []any, error) {
	quotedTable := quoteIdentifier(driver, tableName)

	filterStatement, err := createFilterStatement(driver, filter, "id DESC", limit, offset)
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

	switch driver {
	case POSTGRES_DRIVER:
		switch tableName {
		case TABLE_EVALUATIONS:
			query = fmt.Sprintf(`SELECT id, created_at, updated_at, tenant_id, status, experiment_id, entity FROM %s %s;`, quotedTable, filterStatement)
		default:
			query = fmt.Sprintf(`SELECT id, created_at, updated_at, tenant_id, entity FROM %s %s;`, quotedTable, filterStatement)
		}
	case SQLITE_DRIVER:
		switch tableName {
		case TABLE_EVALUATIONS:
			query = fmt.Sprintf(`SELECT id, created_at, updated_at, tenant_id, status, experiment_id, entity FROM %s %s;`, quotedTable, filterStatement)
		default:
			query = fmt.Sprintf(`SELECT id, created_at, updated_at, tenant_id, entity FROM %s %s;`, quotedTable, filterStatement)
		}
	default:
		return "", nil, getUnsupportedDriverError(driver)
	}

	return query, args, nil
}

// CreateUpdateEvaluationStatement returns a driver-specific UPDATE statement for the evaluations table,
// setting only the non-empty fields (status, entity) and updated_at, filtered by id.
// If status is empty, the query does not set status; if entityJSON is empty, the query does not set entity.
// Returns the query, args in SET order then id, and an optional error.
func CreateUpdateEvaluationStatement(driver, tableName, id string, status api.OverallState, entityJSON string) (query string, args []any, err error) {
	quotedTable, quotedID, setParts, argsList := tableAndArgsList(driver, tableName, status, entityJSON, id)
	return createUpdateStatement(driver, quotedTable, quotedID, setParts, argsList)
}

func CreateUpdateCollectionStatement(driver, tableName, id string, entityJSON string) (query string, args []any, err error) {
	quotedTable, quotedID, setParts, argsList := tableAndArgsList(driver, tableName, "", entityJSON, id)
	return createUpdateStatement(driver, quotedTable, quotedID, setParts, argsList)
}

func createUpdateStatement(driver, quotedTable, quotedID string, setParts []string, argsList []any) (query string, args []any, err error) {
	switch driver {
	case POSTGRES_DRIVER:
		return createUpdateStatementForPostgres(setParts, argsList, query, quotedTable, quotedID, args)
	case SQLITE_DRIVER:
		return createUpdateStatementForSQLite(setParts, argsList, query, quotedTable, quotedID, args)
	default:
		return "", nil, getUnsupportedDriverError(driver)
	}
}

func tableAndArgsList(driver string, tableName string, status api.OverallState, entityJSON string, id string) (string, string, []string, []any) {
	quotedTable := quoteIdentifier(driver, tableName)
	quotedStatus := quoteIdentifier(driver, "status")
	quotedEntity := quoteIdentifier(driver, "entity")
	quotedUpdatedAt := quoteIdentifier(driver, "updated_at")
	quotedID := quoteIdentifier(driver, "id")

	var setParts []string
	var argsList []any
	if status != "" {
		setParts = append(setParts, quotedStatus)
		argsList = append(argsList, status)
	}
	if entityJSON != "" {
		setParts = append(setParts, quotedEntity)
		argsList = append(argsList, entityJSON)
	}
	setParts = append(setParts, fmt.Sprintf("%s = CURRENT_TIMESTAMP", quotedUpdatedAt))
	argsList = append(argsList, id)
	return quotedTable, quotedID, setParts, argsList
}

func createUpdateStatementForSQLite(setParts []string, argsList []any, query string, quotedTable string, quotedID string, args []any) (string, []any, error) {
	placeholders := make([]string, 0, len(setParts))
	for i, part := range setParts {
		if i < len(setParts)-1 {
			placeholders = append(placeholders, part+" = ?")
		} else {
			placeholders = append(placeholders, part)
		}
	}
	query = fmt.Sprintf(`UPDATE %s SET %s WHERE %s = ?;`,
		quotedTable, strings.Join(placeholders, ", "), quotedID)
	args = argsList
	return query, args, nil
}

func createUpdateStatementForPostgres(setParts []string, argsList []any, query string, quotedTable string, quotedID string, args []any) (string, []any, error) {
	placeholders := make([]string, 0, len(setParts))
	for i := range setParts {
		if i < len(setParts)-1 {
			placeholders = append(placeholders, fmt.Sprintf("%s = $%d", setParts[i], i+1))
		} else {
			placeholders = append(placeholders, setParts[i])
		}
	}
	whereIdx := len(argsList)
	query = fmt.Sprintf(`UPDATE %s SET %s WHERE %s = $%d;`,
		quotedTable, strings.Join(placeholders, ", "), quotedID, whereIdx)
	args = argsList
	return query, args, nil
}

// Returns the limit, offset, and filtered params
func extractQueryParams(filter *abstractions.QueryFilter) *abstractions.QueryFilter {
	params := getParams(filter)
	// TODO - remove this delete after adding owner in storage layer
	delete(params, "owner")
	return &abstractions.QueryFilter{
		Limit:  filter.Limit,
		Offset: filter.Offset,
		Params: params,
	}
}

// createCollectionFilterWhereAndArgs builds WHERE clause and args for collections list/count.
// Entity column stores CollectionConfig JSON (name, tags at top level). Filters: tenant_id, name, tags.
func createCollectionFilterWhereAndArgs(driver string, params map[string]any) (where string, args []any, err error) {
	var conditions []string
	args = make([]any, 0)
	idx := 0
	paramPlaceholder := getParam(driver)

	if v, ok := params["tenant_id"].(string); ok && v != "" {
		idx++
		conditions = append(conditions, fmt.Sprintf("%s = %s", quoteIdentifier(driver, "tenant_id"), getParamValue(paramPlaceholder, idx-1)))
		args = append(args, v)
	}

	if v, ok := params["name"].(string); ok && v != "" {
		idx++
		ph := getParamValue(paramPlaceholder, idx-1)
		switch driver {
		case POSTGRES_DRIVER:
			conditions = append(conditions, fmt.Sprintf("(entity->>'name' ILIKE '%s' || %s || '%s')", "%", ph, "%"))
		case SQLITE_DRIVER:
			conditions = append(conditions, fmt.Sprintf("(json_extract(entity, '$.name') LIKE '%s' || %s || '%s')", "%", ph, "%"))
		default:
			return "", nil, getUnsupportedDriverError(driver)
		}
		args = append(args, v)
	}

	if v, ok := params["tags"].(string); ok && v != "" {
		tagStrs := strings.Split(v, ",")
		for i := range tagStrs {
			tagStrs[i] = strings.TrimSpace(tagStrs[i])
		}
		if len(tagStrs) > 0 {
			switch driver {
			case POSTGRES_DRIVER:
				var tagConditions []string
				for _, tag := range tagStrs {
					idx++
					ph := getParamValue(paramPlaceholder, idx-1)
					tagConditions = append(tagConditions, fmt.Sprintf("(COALESCE(entity->'tags', '[]'::jsonb) @> %s::jsonb)", ph))
					singleTagJSON, _ := json.Marshal([]string{tag})
					args = append(args, string(singleTagJSON))
				}
				conditions = append(conditions, "("+strings.Join(tagConditions, " OR ")+")")
			case SQLITE_DRIVER:
				tagsJSON, err := json.Marshal(tagStrs)
				if err != nil {
					return "", nil, err
				}
				idx++
				ph := getParamValue(paramPlaceholder, idx-1)
				conditions = append(conditions, fmt.Sprintf("(SELECT COUNT(*) FROM json_each(COALESCE(json_extract(entity, '$.tags'), '[]')) AS je WHERE je.value IN (SELECT value FROM json_each(%s))) > 0", ph))
				args = append(args, string(tagsJSON))
			default:
				return "", nil, getUnsupportedDriverError(driver)
			}
		}
	}

	if len(conditions) == 0 {
		return "", args, nil
	}
	return " WHERE " + strings.Join(conditions, " AND "), args, nil
}

func createCollectionCountStatement(driver, tableName string, params map[string]any) (string, []any, error) {
	quotedTable := quoteIdentifier(driver, tableName)
	where, args, err := createCollectionFilterWhereAndArgs(driver, params)
	if err != nil {
		return "", nil, err
	}
	return fmt.Sprintf("SELECT COUNT(*) FROM %s%s;", quotedTable, where), args, nil
}

func createCollectionListStatement(driver, tableName string, limit, offset int, params map[string]any) (string, []any, error) {
	quotedTable := quoteIdentifier(driver, tableName)
	where, args, err := createCollectionFilterWhereAndArgs(driver, params)
	if err != nil {
		return "", nil, err
	}
	paramPlaceholder := getParam(driver)
	idx := len(args)
	orderLimitOffset := " ORDER BY id DESC"
	if limit > 0 {
		idx++
		orderLimitOffset += fmt.Sprintf(" LIMIT %s", getParamValue(paramPlaceholder, idx-1))
		args = append(args, limit)
	}
	if offset > 0 {
		idx++
		orderLimitOffset += fmt.Sprintf(" OFFSET %s", getParamValue(paramPlaceholder, idx-1))
		args = append(args, offset)
	}
	return fmt.Sprintf("SELECT id, created_at, updated_at, tenant_id, entity FROM %s%s%s;", quotedTable, where, orderLimitOffset), args, nil
}

// createEvaluationFilterWhereAndArgs builds WHERE clause and args for evaluations list/count.
// Entity column stores EvaluationJobEntity JSON (config.name, config.tags under entity->'config'). Filters: tenant_id, status (columns), name, tags (entity JSON).
func createEvaluationFilterWhereAndArgs(driver string, params map[string]any) (where string, args []any, err error) {
	var conditions []string
	args = make([]any, 0)
	idx := 0
	paramPlaceholder := getParam(driver)

	if v, ok := params["tenant_id"].(string); ok && v != "" {
		idx++
		conditions = append(conditions, fmt.Sprintf("%s = %s", quoteIdentifier(driver, "tenant_id"), getParamValue(paramPlaceholder, idx-1)))
		args = append(args, v)
	}

	if v, ok := params["status"].(string); ok && v != "" {
		idx++
		conditions = append(conditions, fmt.Sprintf("%s = %s", quoteIdentifier(driver, "status"), getParamValue(paramPlaceholder, idx-1)))
		args = append(args, v)
	}

	if v, ok := params["name"].(string); ok && v != "" {
		idx++
		ph := getParamValue(paramPlaceholder, idx-1)
		switch driver {
		case POSTGRES_DRIVER:
			conditions = append(conditions, fmt.Sprintf("(entity->'config'->>'name' ILIKE '%s' || %s || '%s')", "%", ph, "%"))
		case SQLITE_DRIVER:
			conditions = append(conditions, fmt.Sprintf("(json_extract(entity, '$.config.name') LIKE '%s' || %s || '%s')", "%", ph, "%"))
		default:
			return "", nil, getUnsupportedDriverError(driver)
		}
		args = append(args, v)
	}

	if v, ok := params["tags"].(string); ok && v != "" {
		tagStrs := strings.Split(v, ",")
		for i := range tagStrs {
			tagStrs[i] = strings.TrimSpace(tagStrs[i])
		}
		if len(tagStrs) > 0 {
			switch driver {
			case POSTGRES_DRIVER:
				var tagConditions []string
				for _, tag := range tagStrs {
					idx++
					ph := getParamValue(paramPlaceholder, idx-1)
					tagConditions = append(tagConditions, fmt.Sprintf("(COALESCE(entity->'config'->'tags', '[]'::jsonb) @> %s::jsonb)", ph))
					singleTagJSON, _ := json.Marshal([]string{tag})
					args = append(args, string(singleTagJSON))
				}
				conditions = append(conditions, "("+strings.Join(tagConditions, " OR ")+")")
			case SQLITE_DRIVER:
				tagsJSON, err := json.Marshal(tagStrs)
				if err != nil {
					return "", nil, err
				}
				idx++
				ph := getParamValue(paramPlaceholder, idx-1)
				conditions = append(conditions, fmt.Sprintf("(SELECT COUNT(*) FROM json_each(COALESCE(json_extract(entity, '$.config.tags'), '[]')) AS je WHERE je.value IN (SELECT value FROM json_each(%s))) > 0", ph))
				args = append(args, string(tagsJSON))
			default:
				return "", nil, getUnsupportedDriverError(driver)
			}
		}
	}

	if len(conditions) == 0 {
		return "", args, nil
	}
	return " WHERE " + strings.Join(conditions, " AND "), args, nil
}

func createEvaluationCountStatement(driver, tableName string, params map[string]any) (string, []any, error) {
	quotedTable := quoteIdentifier(driver, tableName)
	where, args, err := createEvaluationFilterWhereAndArgs(driver, params)
	if err != nil {
		return "", nil, err
	}
	return fmt.Sprintf("SELECT COUNT(*) FROM %s%s;", quotedTable, where), args, nil
}

func createEvaluationListStatement(driver, tableName string, limit, offset int, params map[string]any) (string, []any, error) {
	quotedTable := quoteIdentifier(driver, tableName)
	where, args, err := createEvaluationFilterWhereAndArgs(driver, params)
	if err != nil {
		return "", nil, err
	}
	paramPlaceholder := getParam(driver)
	idx := len(args)
	orderLimitOffset := " ORDER BY id DESC"
	if limit > 0 {
		idx++
		orderLimitOffset += fmt.Sprintf(" LIMIT %s", getParamValue(paramPlaceholder, idx-1))
		args = append(args, limit)
	}
	if offset > 0 {
		idx++
		orderLimitOffset += fmt.Sprintf(" OFFSET %s", getParamValue(paramPlaceholder, idx-1))
		args = append(args, offset)
	}
	return fmt.Sprintf("SELECT id, created_at, updated_at, tenant_id, status, experiment_id, entity FROM %s%s%s;", quotedTable, where, orderLimitOffset), args, nil
}
