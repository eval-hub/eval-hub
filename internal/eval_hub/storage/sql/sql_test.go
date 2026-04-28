package sql_test

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/storage"
	"github.com/eval-hub/eval-hub/internal/eval_hub/storage/sql/shared"
	"github.com/eval-hub/eval-hub/internal/logging"
	"github.com/go-viper/mapstructure/v2"
)

var (
	dbIndex = atomic.Int32{}
)

func TestTxRetryConfig(t *testing.T) {
	t.Run("defaults when unset", func(t *testing.T) {
		cfg := shared.SQLDatabaseConfig{}
		if cfg.GetTxRetryMax() != shared.DefaultTxRetryMax {
			t.Errorf("want %d, got %d", shared.DefaultTxRetryMax, cfg.GetTxRetryMax())
		}
		if cfg.GetTxRetryInterval() != shared.DefaultTxRetryInterval {
			t.Errorf("want %v, got %v", shared.DefaultTxRetryInterval, cfg.GetTxRetryInterval())
		}
	})

	t.Run("custom values", func(t *testing.T) {
		cfg := shared.SQLDatabaseConfig{
			TxRetryMax:      5,
			TxRetryInterval: 200 * time.Millisecond,
		}
		if cfg.GetTxRetryMax() != 5 {
			t.Errorf("want 5, got %d", cfg.GetTxRetryMax())
		}
		if cfg.GetTxRetryInterval() != 200*time.Millisecond {
			t.Errorf("want 200ms, got %v", cfg.GetTxRetryInterval())
		}
	})

	t.Run("mapstructure decodes duration strings", func(t *testing.T) {
		config := map[string]any{
			"driver":            "sqlite",
			"url":               "file::test:?mode=memory&cache=shared",
			"tx_retry_max":      7,
			"tx_retry_interval": "5s",
		}
		var sqlConfig shared.SQLDatabaseConfig
		decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
			DecodeHook: mapstructure.StringToTimeDurationHookFunc(),
			Result:     &sqlConfig,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if err = decoder.Decode(config); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sqlConfig.GetTxRetryMax() != 7 {
			t.Errorf("want 7, got %d", sqlConfig.GetTxRetryMax())
		}
		if sqlConfig.GetTxRetryInterval() != 5*time.Second {
			t.Errorf("want 5s, got %v", sqlConfig.GetTxRetryInterval())
		}
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
