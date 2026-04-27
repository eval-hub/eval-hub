package sql_test

import (
	gosql "database/sql"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/storage/sql"
	"github.com/eval-hub/eval-hub/internal/eval_hub/storage/sql/shared"
)

func TestGetIsolationLevel(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("driver sqlite returns default when debug env unset", func(t *testing.T) {
		isolationLevel := ""
		cfg := &shared.SQLDatabaseConfig{Driver: sql.SQLITE_DRIVER}
		level, err := sql.GetIsolationLevel(isolationLevel, cfg, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if level != gosql.LevelDefault {
			t.Fatalf("want %v, got %v", gosql.LevelDefault, level)
		}
	})

	t.Run("driver postgres returns serializable when debug env unset", func(t *testing.T) {
		isolationLevel := ""
		cfg := &shared.SQLDatabaseConfig{Driver: sql.POSTGRES_DRIVER}
		level, err := sql.GetIsolationLevel(isolationLevel, cfg, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if level != gosql.LevelSerializable {
			t.Fatalf("want %v, got %v", gosql.LevelSerializable, level)
		}
	})

	t.Run("unknown driver returns default when debug env unset", func(t *testing.T) {
		isolationLevel := ""
		cfg := &shared.SQLDatabaseConfig{Driver: "mysql"}
		level, err := sql.GetIsolationLevel(isolationLevel, cfg, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if level != gosql.LevelDefault {
			t.Fatalf("want %v, got %v", gosql.LevelDefault, level)
		}
	})

	t.Run("debug env overrides driver with valid level", func(t *testing.T) {
		isolationLevel := " Read Committed "
		cfg := &shared.SQLDatabaseConfig{Driver: sql.SQLITE_DRIVER}
		level, err := sql.GetIsolationLevel(isolationLevel, cfg, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if level != gosql.LevelReadCommitted {
			t.Fatalf("want %v, got %v", gosql.LevelReadCommitted, level)
		}
	})

	t.Run("debug env case-insensitive match", func(t *testing.T) {
		isolationLevel := "serializable"
		cfg := &shared.SQLDatabaseConfig{Driver: sql.SQLITE_DRIVER}
		level, err := sql.GetIsolationLevel(isolationLevel, cfg, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if level != gosql.LevelSerializable {
			t.Fatalf("want %v, got %v", gosql.LevelSerializable, level)
		}
	})

	t.Run("debug env invalid level returns error", func(t *testing.T) {
		isolationLevel := "not-a-real-level"
		cfg := &shared.SQLDatabaseConfig{Driver: sql.SQLITE_DRIVER}
		_, err := sql.GetIsolationLevel(isolationLevel, cfg, logger)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "invalid isolation level") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
