package postgres

import (
	"database/sql"
	"log/slog"

	"github.com/eval-hub/eval-hub/internal/storage/sql/shared"
)

func Setup(logger *slog.Logger, pool *sql.DB, config *shared.SQLDatabaseConfig) (shared.SQLStatementsFactory, error) {
	return NewStatementsFactory(logger), nil
}
