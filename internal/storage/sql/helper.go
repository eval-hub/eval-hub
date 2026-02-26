package sql

import (
	"encoding/json"
	"fmt"
	"maps"
	"strings"

	"github.com/eval-hub/eval-hub/pkg/api"
)

// TODO - do we want to pull out all the SQL statements like this or leave them in the functions?

// SQLite: use ? placeholders
const SQLITE_INSERT_EVALUATION_STATEMENT = `INSERT INTO evaluations (id, resource, status, experiment_id, entity) VALUES (?, ?, ?, ?, ?);`

// PostgreSQL: use $1, $2 placeholders and RETURNING id clause
const POSTGRES_INSERT_EVALUATION_STATEMENT = `INSERT INTO evaluations (id, resource, status, experiment_id, entity) VALUES ($1, $2, $3, $4, $5) RETURNING id;`

// SQLite: use ? placeholders
const SQLITE_INSERT_COLLECTION_STATEMENT = `INSERT INTO collections (id, resource, entity) VALUES (?, ?, ?);`

// PostgreSQL: use $1, $2 placeholders and RETURNING id clause
const POSTGRES_INSERT_COLLECTION_STATEMENT = `INSERT INTO collections (id, resource, entity) VALUES ($1, $2, $3) RETURNING id;`

// SQLite: use ? placeholders
const SQLITE_INSERT_PROVIDER_STATEMENT = `INSERT INTO providers (id, resource, entity) VALUES (?, ?, ?);`

// PostgreSQL: use $1, $2 placeholders and RETURNING id clause
const POSTGRES_INSERT_PROVIDER_STATEMENT = `INSERT INTO providers (id, resource, entity) VALUES ($1, $2, $3) RETURNING id;`

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
func quoteIdentifier(driver string, identifier string) string {
	switch driver {
	case POSTGRES_DRIVER:
		//escaped := strings.ReplaceAll(identifier, `"`, `""`)
		//return fmt.Sprintf(`"%s"`, escaped)
		return identifier
	default:
		return identifier
	}
}

// createGetEntityStatement returns a driver-specific SELECT statement
// to retrieve an entity by ID
func createGetEntityStatement(driver, tableName string) (string, error) {
	quotedTable := quoteIdentifier(driver, tableName)

	switch driver + tableName {
	case POSTGRES_DRIVER + TABLE_EVALUATIONS:
		return fmt.Sprintf(`SELECT id, resource, status, experiment_id, entity FROM %s WHERE id = $1;`, quotedTable), nil
	case SQLITE_DRIVER + TABLE_EVALUATIONS:
		// SQLite: use ? placeholder
		return fmt.Sprintf(`SELECT id, resource, status, experiment_id, entity FROM %s WHERE id = ?;`, quotedTable), nil
	case POSTGRES_DRIVER + TABLE_COLLECTIONS:
		return fmt.Sprintf(`SELECT id, resource, entity FROM %s WHERE id = $1;`, quotedTable), nil
	case SQLITE_DRIVER + TABLE_COLLECTIONS:
		// SQLite: use ? placeholder
		return fmt.Sprintf(`SELECT id, resource, entity FROM %s WHERE id = ?;`, quotedTable), nil
	case POSTGRES_DRIVER + TABLE_PROVIDERS:
		return fmt.Sprintf(`SELECT id, resource, entity FROM %s WHERE id = $1;`, quotedTable), nil
	case SQLITE_DRIVER + TABLE_PROVIDERS:
		return fmt.Sprintf(`SELECT id, resource, entity FROM %s WHERE id = ?;`, quotedTable), nil
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

	filterStatement, args, err := createFilterStatement(driver, filter, "", 0, 0)
	if err != nil {
		return "", nil, err
	}

	var query string

	switch driver {
	case POSTGRES_DRIVER:
		query = fmt.Sprintf(`SELECT COUNT(*) FROM %s%s;`, quotedTable, filterStatement)
	case SQLITE_DRIVER:
		query = fmt.Sprintf(`SELECT COUNT(*) FROM %s%s;`, quotedTable, filterStatement)
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

func getParamKey(driver string, key string) string {
	switch driver {
	case SQLITE_DRIVER:
		switch key {
		case "tenant", "owner", "created_at", "updated_at":
			return fmt.Sprintf("resource ->> '$.%s'", key)
		default:
			return key
		}
	case POSTGRES_DRIVER:
		switch key {
		case "tenant", "owner", "created_at", "updated_at":
			return fmt.Sprintf("resource ->> '{%s}'", key)
		default:
			return key
		}
	default:
		return key
	}
}

func setParamValue(driver string, parent string, key string, value string) string {
	switch driver {
	case SQLITE_DRIVER:
		// Use strftime for ISO 8601 - CURRENT_TIMESTAMP/datetime('now') return 'YYYY-MM-DD HH:MM:SS'
		// but API expects '2006-01-02T15:04:05Z07:00'
		if value == "CURRENT_TIMESTAMP" {
			value = "strftime('%Y-%m-%dT%H:%M:%SZ', 'now', 'utc')"
		}
		return fmt.Sprintf("json_set( %s, '$.%s', %s )", parent, key, value)
	case POSTGRES_DRIVER:
		if value == "CURRENT_TIMESTAMP" {
			value = `to_jsonb(to_char(now() AT TIME ZONE 'utc', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'))`
		}
		return fmt.Sprintf("jsonb_set( %s, '{%s}', %s )", parent, key, value)
	default:
		return value
	}
}

func getParamValue(param string, index int) string {
	if param == "$" {
		return fmt.Sprintf("$%d", index)
	} else {
		return param
	}
}

func createFilterStatement(driver string, filter map[string]any, orderBy string, limit int, offset int) (string, []any, error) {
	param := getParam(driver)

	var sb strings.Builder
	var args []any

	// start at 1 because we use $1, $2, $3, etc. placeholders
	index := 1

	if len(filter) > 0 {
		sb.WriteString(" WHERE ")
		for key := range maps.Keys(filter) {
			if index > 1 {
				sb.WriteString(" AND ")
			}
			sb.WriteString(fmt.Sprintf("%s = %s", getParamKey(driver, key), getParamValue(param, index)))
			args = append(args, filter[key])
			index++
		}
	}

	// ORDER BY id DESC LIMIT $2 OFFSET $3
	if orderBy != "" {
		// note that we use the value here and not ?
		sb.WriteString(fmt.Sprintf(" ORDER BY %s", orderBy))
		// no arg because we substitute directly for this value
	}
	if limit > 0 {
		sb.WriteString(fmt.Sprintf(" LIMIT %s", getParamValue(param, index)))
		index++
		args = append(args, limit)
	}
	if offset > 0 {
		sb.WriteString(fmt.Sprintf(" OFFSET %s", getParamValue(param, index)))
		index++
		args = append(args, offset)
	}

	fmt.Printf("Filter statement:\n%s\nargs:\n%s\n\n", sb.String(), pretty(args))

	return sb.String(), args, nil
}

// createListEntitiesStatement returns a driver-specific SELECT statement
// to list entities with pagination (LIMIT and OFFSET), optionally filtered by status
func createListEntitiesStatement(driver, tableName string, limit, offset int, filter map[string]any) (string, []any, error) {
	quotedTable := quoteIdentifier(driver, tableName)

	filterStatement, args, err := createFilterStatement(driver, filter, "id DESC", limit, offset)
	if err != nil {
		return "", nil, err
	}

	var query string

	switch driver {
	case POSTGRES_DRIVER:
		switch tableName {
		case TABLE_EVALUATIONS:
			query = fmt.Sprintf(`SELECT id, resource, status, experiment_id, entity FROM %s %s;`, quotedTable, filterStatement)
		default:
			query = fmt.Sprintf(`SELECT id, resource, entity FROM %s %s;`, quotedTable, filterStatement)
		}
	case SQLITE_DRIVER:
		switch tableName {
		case TABLE_EVALUATIONS:
			query = fmt.Sprintf(`SELECT id, resource, status, experiment_id, entity FROM %s %s;`, quotedTable, filterStatement)
		default:
			query = fmt.Sprintf(`SELECT id, resource, entity FROM %s %s;`, quotedTable, filterStatement)
		}
	default:
		return "", nil, getUnsupportedDriverError(driver)
	}

	print("List entities query:\n%s\nargs:\n%s\n\n", query, pretty(args))

	return query, args, nil
}

// CreateUpdateEvaluationStatement returns a driver-specific UPDATE statement for the evaluations table,
// setting only the non-empty fields (status, entity) and updated_at, filtered by id.
// If status is empty, the query does not set status; if entityJSON is empty, the query does not set entity.
// Returns the query, args in SET order then id, and an optional error.
func CreateUpdateEvaluationStatement(driver, tableName, id string, status api.OverallState, entityJSON string) (query string, args []any, err error) {
	quotedTable, quotedID, setParts, argsList := tableAndArgsList(driver, tableName, status, entityJSON, id)
	statement, args, err := createUpdateStatement(driver, quotedTable, quotedID, setParts, argsList)
	if err != nil {
		return statement, args, err
	}
	print("Update evaluation job query:\n%s\nargs:\n%s\n\n", statement, pretty(args))
	return statement, args, nil
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
	quotedUpdatedAt := quoteIdentifier(driver, "resource") // quoteIdentifier(driver, "updated_at")
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
	// UPDATE events SET json_set(data, '$.date', '2024-12-05') WHERE id = 1;
	setParts = append(setParts, fmt.Sprintf("%s = %s", quotedUpdatedAt, quoteIdentifier(driver, setParamValue(driver, "resource", "updated_at", "CURRENT_TIMESTAMP"))))
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

// just for debugging

func pretty(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err == nil {
		return string(b)
	}
	return fmt.Sprintf("%v", v)
}

func print(format string, a ...any) {
	fmt.Printf(format, a...)
}
