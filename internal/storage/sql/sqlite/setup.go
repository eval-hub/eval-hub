package sqlite

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/eval-hub/eval-hub/internal/storage/sql/shared"
)

func Setup(pool *sql.DB, config *shared.SQLDatabaseConfig) (shared.SQLStatementsFactory, error) {
	// SQLite only supports one writer at a time; serializing access through
	// a single connection eliminates lock contention and deadlocks.
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
	return NewStatementsFactory(), nil
}
