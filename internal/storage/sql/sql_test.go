package sql_test

import (
	"testing"

	"github.com/eval-hub/eval-hub/internal/storage/sql/shared"
)

func TestSQLStorage(t *testing.T) {
	t.Run("Check database name is extracted correctly", func(t *testing.T) {
		data := [][]string{
			{"file::eval_hub:?mode=memory&cache=shared", "eval_hub"},
			{"postgres://user@localhost:5432/eval_hub", "eval_hub"},
		}
		for _, d := range data {
			databaseConfig := shared.SQLDatabaseConfig{
				URL: d[0],
			}
			databaseName := databaseConfig.GetDatabaseName()
			if databaseName != d[1] {
				t.Errorf("Expected database name %s, got '%s' from URL %s", d[1], databaseName, d[0])
			}
		}
	})
}
