package sqlite

import (
	"database/sql"
	"fmt"
	"log/slog"
	"strings"

	"github.com/eval-hub/eval-hub/internal/eval_hub/storage/sql/shared"
)

func Setup(logger *slog.Logger, pool *sql.DB, config *shared.SQLDatabaseConfig) (shared.SQLStatementsFactory, error) {
	// SQLite only supports one writer at a time; a single connection
	// serializes all access and eliminates lock contention.
	pool.SetMaxOpenConns(1)
	if _, err := pool.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		return nil, fmt.Errorf("failed to set busy_timeout: %w", err)
	}
	// Enable WAL mode for file-based databases (in-memory databases don't
	// support WAL and always return journal_mode="memory").
	if !strings.Contains(config.URL, "mode=memory") {
		var mode string
		if err := pool.QueryRow("PRAGMA journal_mode = WAL").Scan(&mode); err != nil {
			return nil, fmt.Errorf("failed to set journal_mode: %w", err)
		}
		if mode != "wal" {
			return nil, fmt.Errorf("failed to enable WAL mode: database returned journal_mode=%q", mode)
		}
	}
	return NewStatementsFactory(logger), nil
}

// EnsureEvaluationWorkloadTypeColumn upgrades databases created before the
// workload_type column was added. SQLite does not support ADD COLUMN IF NOT EXISTS.
func EnsureEvaluationWorkloadTypeColumn(pool *sql.DB) error {
	var columns int
	const columnQuery = `SELECT COUNT(*) FROM pragma_table_info('evaluations') WHERE name = 'workload_type'`
	if err := pool.QueryRow(columnQuery).Scan(&columns); err != nil {
		return fmt.Errorf("check evaluations workload_type column: %w", err)
	}
	if columns != 0 {
		return nil
	}
	const migration = `ALTER TABLE evaluations ADD COLUMN workload_type TEXT NOT NULL DEFAULT 'evaluation'`
	if _, err := pool.Exec(migration); err != nil {
		// Another instance may have added the column after the check.
		if checkErr := pool.QueryRow(columnQuery).Scan(&columns); checkErr == nil && columns != 0 {
			return nil
		}
		return fmt.Errorf("add evaluations workload_type column: %w", err)
	}
	return nil
}
