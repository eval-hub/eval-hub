package sqlite

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/storage/sql/shared"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestCreateDeleteSystemEntitiesStatement(t *testing.T) {
	f := NewStatementsFactory(slog.Default())
	stmt, args := f.CreateDeleteSystemEntitiesStatement(shared.TABLE_COLLECTIONS)
	if !strings.Contains(stmt, "DELETE FROM collections") {
		t.Fatalf("unexpected statement: %s", stmt)
	}
	if !strings.Contains(stmt, "owner = ?") {
		t.Fatalf("expected owner placeholder ?, got: %s", stmt)
	}
	if len(args) != 1 || args[0] != abstractions.OwnerSystem {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestCreateProviderAddEntityStatementIncludesTimestamps(t *testing.T) {
	f := NewStatementsFactory(slog.Default())
	created := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	updated := time.Date(2024, 2, 3, 4, 5, 6, 0, time.UTC)
	provider := &api.ProviderResource{
		Resource: api.Resource{
			ID:        "p1",
			Tenant:    "t1",
			Owner:     "system",
			CreatedAt: created,
			UpdatedAt: updated,
		},
	}
	stmt, args := f.CreateProviderAddEntityStatement(provider, `{"name":"n"}`)
	if !strings.Contains(stmt, "created_at") || !strings.Contains(stmt, "updated_at") {
		t.Fatalf("expected created_at/updated_at columns in: %s", stmt)
	}
	if len(args) != 6 || args[3] != created || args[4] != updated {
		t.Fatalf("unexpected args: %#v", args)
	}
}
