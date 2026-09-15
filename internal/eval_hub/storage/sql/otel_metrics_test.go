package sql

import (
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/storage/sql/shared"
	semconv "go.opentelemetry.io/otel/semconv/v1.39.0"
)

func TestDBSystemName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		driver string
		want   string
	}{
		{driver: SQLiteDriver, want: semconv.DBSystemNameSQLite.Value.AsString()},
		{driver: PostgresDriver, want: semconv.DBSystemNamePostgreSQL.Value.AsString()},
		{driver: "unknown", want: semconv.DBSystemNameOtherSQL.Value.AsString()},
	}
	for _, tt := range tests {
		got := dbSystemName(tt.driver)
		if got.Value.AsString() != tt.want {
			t.Errorf("dbSystemName(%q) = %q, want %q", tt.driver, got.Value.AsString(), tt.want)
		}
	}
}

func TestEffectiveMaxIdleConns(t *testing.T) {
	t.Parallel()

	t.Run("unset uses database/sql default", func(t *testing.T) {
		got := effectiveMaxIdleConns(&shared.SQLDatabaseConfig{})
		if got != defaultMaxIdleConns {
			t.Errorf("effectiveMaxIdleConns(unset) = %d, want %d", got, defaultMaxIdleConns)
		}
	})

	t.Run("configured value is respected", func(t *testing.T) {
		configured := 17
		got := effectiveMaxIdleConns(&shared.SQLDatabaseConfig{MaxIdleConns: &configured})
		if got != configured {
			t.Errorf("effectiveMaxIdleConns(configured) = %d, want %d", got, configured)
		}
	})
}
