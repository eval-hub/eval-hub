package sql

import (
	"fmt"
	"strings"

	"github.com/eval-hub/eval-hub/pkg/api"
)

// TODO - do we want to pull out all the SQL statements like this or leave them in the functions?

// SQLite: use ? placeholders
const SQLITE_INSERT_EVALUATION_STATEMENT = `INSERT INTO evaluations (id, tenant_id, status, entity) VALUES (?, ?, ?, ?);`

// PostgreSQL: use $1, $2 placeholders and RETURNING id clause
const POSTGRES_INSERT_EVALUATION_STATEMENT = `INSERT INTO evaluations (id, tenant_id, status, entity) VALUES ($1, $2, $3, $4) RETURNING id;`

// TODO: Add collection insert statement
const INSERT_COLLECTION_STATEMENT = `INSERT INTO collections (entity) VALUES (?);`

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

	switch driver {
	case POSTGRES_DRIVER:
		return fmt.Sprintf(`SELECT id, created_at, updated_at, status, entity FROM %s WHERE id = $1;`, quotedTable), nil
	case SQLITE_DRIVER:
		// SQLite: use ? placeholder
		return fmt.Sprintf(`SELECT id, created_at, updated_at, status, entity FROM %s WHERE id = ?;`, quotedTable), nil
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
// to count total entities in the table, optionally filtered by status
func createCountEntitiesStatement(driver, tableName string, statusFilter string) (string, []any, error) {
	quotedTable := quoteIdentifier(driver, tableName)

	var query string
	var args []any

	switch driver {
	case POSTGRES_DRIVER:
		if statusFilter != "" {
			query = fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE status = $1;`, quotedTable)
			args = []any{statusFilter}
		} else {
			query = fmt.Sprintf(`SELECT COUNT(*) FROM %s;`, quotedTable)
		}
	case SQLITE_DRIVER:
		if statusFilter != "" {
			query = fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE status = ?;`, quotedTable)
			args = []any{statusFilter}
		} else {
			query = fmt.Sprintf(`SELECT COUNT(*) FROM %s;`, quotedTable)
		}
	default:
		return "", nil, getUnsupportedDriverError(driver)
	}

	return query, args, nil
}

// createListEntitiesStatement returns a driver-specific SELECT statement
// to list entities with pagination (LIMIT and OFFSET), optionally filtered by status
func createListEntitiesStatement(driver, tableName string, limit, offset int, statusFilter string) (string, []any, error) {
	quotedTable := quoteIdentifier(driver, tableName)

	var query string
	var args []any

	switch driver {
	case POSTGRES_DRIVER:
		if statusFilter != "" {
			query = fmt.Sprintf(`SELECT id, created_at, updated_at, status, entity FROM %s WHERE status = $1 ORDER BY id DESC LIMIT $2 OFFSET $3;`, quotedTable)
			args = []any{statusFilter, limit, offset}
		} else {
			query = fmt.Sprintf(`SELECT id, created_at, updated_at, status, entity FROM %s ORDER BY id DESC LIMIT $1 OFFSET $2;`, quotedTable)
			args = []any{limit, offset}
		}
	case SQLITE_DRIVER:
		if statusFilter != "" {
			query = fmt.Sprintf(`SELECT id, created_at, updated_at, status, entity FROM %s WHERE status = ? ORDER BY id DESC LIMIT ? OFFSET ?;`, quotedTable)
			args = []any{statusFilter, limit, offset}
		} else {
			query = fmt.Sprintf(`SELECT id, created_at, updated_at, status, entity FROM %s ORDER BY id DESC LIMIT ? OFFSET ?;`, quotedTable)
			args = []any{limit, offset}
		}
	default:
		return "", nil, getUnsupportedDriverError(driver)
	}

	return query, args, nil
}

// createUpdateStatusStatement returns a driver-specific UPDATE statement
// to update the status of an entity by ID
func createUpdateStatusStatement(driver, tableName string) (string, error) {
	quotedTable := quoteIdentifier(driver, tableName)

	switch driver {
	case POSTGRES_DRIVER:
		// PostgreSQL: use $1, $2 placeholders
		return fmt.Sprintf(`UPDATE %s SET status = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2;`, quotedTable), nil
	case SQLITE_DRIVER:
		// SQLite: use ? placeholders
		return fmt.Sprintf(`UPDATE %s SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?;`, quotedTable), nil
	default:
		return "", getUnsupportedDriverError(driver)
	}
}
