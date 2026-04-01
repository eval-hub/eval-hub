package shared_test

import (
	"strings"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/storage/sql/shared"
)

func TestSQLDatabaseConfig_GetDriverName(t *testing.T) {
	t.Parallel()
	c := shared.SQLDatabaseConfig{Driver: "pgx"}
	if got := c.GetDriverName(); got != "pgx" {
		t.Errorf("GetDriverName() = %q, want %q", got, "pgx")
	}
	var empty shared.SQLDatabaseConfig
	if got := empty.GetDriverName(); got != "" {
		t.Errorf("GetDriverName() on zero value = %q, want empty", got)
	}
}

func TestSQLDatabaseConfig_GetConnectionURL(t *testing.T) {
	t.Parallel()
	t.Run("strips password from userinfo", func(t *testing.T) {
		t.Parallel()
		c := shared.SQLDatabaseConfig{URL: "postgres://alice:secret@localhost:5432/mydb"}
		got, err := c.GetConnectionURL()
		if err != nil {
			t.Fatalf("GetConnectionURL: %v", err)
		}
		if strings.Contains(got, "secret") {
			t.Errorf("password leaked in %q", got)
		}
		want := "postgres://alice@localhost:5432/mydb"
		if got != want {
			t.Errorf("GetConnectionURL() = %q, want %q", got, want)
		}
	})
	t.Run("user without password unchanged", func(t *testing.T) {
		t.Parallel()
		c := shared.SQLDatabaseConfig{URL: "postgres://bob@localhost:5432/db"}
		got, err := c.GetConnectionURL()
		if err != nil {
			t.Fatalf("GetConnectionURL: %v", err)
		}
		if got != "postgres://bob@localhost:5432/db" {
			t.Errorf("GetConnectionURL() = %q", got)
		}
	})
	t.Run("preserves query", func(t *testing.T) {
		t.Parallel()
		c := shared.SQLDatabaseConfig{URL: "postgres://u:p@h:5432/d?sslmode=disable"}
		got, err := c.GetConnectionURL()
		if err != nil {
			t.Fatalf("GetConnectionURL: %v", err)
		}
		if !strings.Contains(got, "sslmode=disable") || strings.Contains(got, "p@") {
			t.Errorf("GetConnectionURL() = %q", got)
		}
	})
	t.Run("invalid URL", func(t *testing.T) {
		t.Parallel()
		c := shared.SQLDatabaseConfig{URL: "postgres://%zz"}
		_, err := c.GetConnectionURL()
		if err == nil {
			t.Fatal("expected error for invalid URL escape")
		}
		if !strings.Contains(err.Error(), "parse connection URL") {
			t.Errorf("error = %v, want wrap of parse failure", err)
		}
	})
}

func TestSQLDatabaseConfig_GetDatabaseName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "sqlite file memory style",
			url:  "file::eval_hub:?mode=memory&cache=shared",
			want: "eval_hub",
		},
		{
			name: "postgres with path",
			url:  "postgres://user@localhost:5432/eval_hub",
			want: "eval_hub",
		},
		{
			name: "postgres single segment",
			url:  "postgres://localhost:5432/dbname",
			want: "dbname",
		},
		{
			name: "invalid URL",
			url:  "postgres://%zz",
			want: "",
		},
		{
			name: "empty URL",
			url:  "",
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := shared.SQLDatabaseConfig{URL: tc.url}
			if got := c.GetDatabaseName(); got != tc.want {
				t.Errorf("GetDatabaseName() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSQLDatabaseConfig_GetUser(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "username only",
			url:  "postgres://user@localhost:5432/eval_hub",
			want: "user",
		},
		{
			name: "username and password",
			url:  "postgres://alice:secret@localhost:5432/db",
			want: "alice",
		},
		{
			name: "no userinfo",
			url:  "postgres://localhost:5432/db",
			want: "",
		},
		{
			name: "file URL",
			url:  "file::eval_hub:?mode=memory",
			want: "",
		},
		{
			name: "invalid URL",
			url:  "postgres://%zz",
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := shared.SQLDatabaseConfig{URL: tc.url}
			if got := c.GetUser(); got != tc.want {
				t.Errorf("GetUser() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDatabaseConfig_structTags(t *testing.T) {
	t.Parallel()
	// Smoke: types used by mapstructure unmarshaling remain constructible.
	lifetime := 30 * time.Minute
	idle := 2
	open := 10
	cfg := shared.DatabaseConfig{
		SQL: map[string]shared.SQLDatabaseConfig{
			"default": {
				Enabled:         true,
				Driver:          "pgx",
				URL:             "postgres://u@localhost:5432/db",
				ConnMaxLifetime: &lifetime,
				MaxIdleConns:    &idle,
				MaxOpenConns:    &open,
				Fallback:        true,
			},
		},
	}
	inner := cfg.SQL["default"]
	if !inner.Enabled || inner.Driver != "pgx" || inner.URL == "" {
		t.Fatalf("unexpected SQL config: %+v", inner)
	}
	if inner.ConnMaxLifetime == nil || *inner.ConnMaxLifetime != lifetime {
		t.Error("ConnMaxLifetime not set")
	}
	if inner.MaxIdleConns == nil || *inner.MaxIdleConns != idle {
		t.Error("MaxIdleConns not set")
	}
	if inner.MaxOpenConns == nil || *inner.MaxOpenConns != open {
		t.Error("MaxOpenConns not set")
	}
	if !inner.Fallback {
		t.Error("Fallback not set")
	}
}
