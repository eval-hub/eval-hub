package sql_test

import (
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/storage"
	"github.com/eval-hub/eval-hub/internal/eval_hub/storage/sql/shared"
	"github.com/eval-hub/eval-hub/internal/logging"
)

var (
	dbIndex = atomic.Int32{}
)

func TestNewStorageConnMaxLifetime(t *testing.T) {
	logger := logging.FallbackLogger()

	t.Run("accepts conn_max_lifetime as duration string", func(t *testing.T) {
		config := map[string]any{
			"driver":            "sqlite",
			"url":               getDBInMemoryURL(getDBName()),
			"conn_max_lifetime": "30m",
		}
		s, err := storage.NewStorage(&config, nil, nil, false, logger)
		if err != nil {
			t.Fatalf("NewStorage failed with duration string: %v", err)
		}
		s.Close()
	})

	t.Run("accepts config without conn_max_lifetime", func(t *testing.T) {
		config := map[string]any{
			"driver": "sqlite",
			"url":    getDBInMemoryURL(getDBName()),
		}
		s, err := storage.NewStorage(&config, nil, nil, false, logger)
		if err != nil {
			t.Fatalf("NewStorage failed without conn_max_lifetime: %v", err)
		}
		s.Close()
	})
}

func TestSQLStorage(t *testing.T) {
	t.Run("Check database name is extracted correctly", func(t *testing.T) {
		data := [][]string{
			{"file::eval_hub:?mode=memory&cache=shared", "eval_hub", ""},
			{"postgres://user@localhost:5432/eval_hub", "eval_hub", "user"},
		}
		for _, d := range data {
			databaseConfig := shared.SQLDatabaseConfig{
				URL: d[0],
			}
			databaseName := databaseConfig.GetDatabaseName()
			if databaseName != d[1] {
				t.Errorf("Expected database name %s, got '%s' from URL %s", d[1], databaseName, d[0])
			}
			user := databaseConfig.GetUser()
			if user != d[2] {
				t.Errorf("Expected user %s, got '%s' from URL %s", d[2], user, d[0])
			}
		}
	})
}

func getTestStorage(t *testing.T, driver string, databaseName string) (abstractions.Storage, error) {
	logger := logging.FallbackLogger()
	switch driver {
	case "sqlite":
		databaseConfig := map[string]any{
			"driver":        "sqlite",
			"url":           getDBInMemoryURL(databaseName),
			"database_name": databaseName,
		}
		return storage.NewStorage(&databaseConfig, nil, nil, false, logger)
	case "postgres", "pgx":
		url, err := getPostgresURL(databaseName)
		if err != nil {
			t.Skipf("Failed to get Postgres URL: %v", err)
		}
		databaseConfig := map[string]any{
			"driver":        "pgx",
			"url":           url,
			"database_name": databaseName,
		}
		return storage.NewStorage(&databaseConfig, nil, nil, false, logger)
	default:
		return nil, fmt.Errorf("unsupported driver: %s", driver)
	}
}

func getDBName() string {
	n := dbIndex.Add(1)
	return fmt.Sprintf("eval_hub_tenant_test_%d", n)
}

func getDBInMemoryURL(dbName string) string {
	// we want each test to use a unique in-memory database
	return fmt.Sprintf("file:%s?mode=memory&cache=shared", dbName)
}
