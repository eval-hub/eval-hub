package postgres

import (
	"database/sql"

	"github.com/eval-hub/eval-hub/internal/storage/sql/shared"
)

func Setup(pool *sql.DB, config *shared.SQLDatabaseConfig) (shared.SQLStatementsFactory, error) {
	return NewStatementsFactory(), nil
}
