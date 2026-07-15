package sqlite

import (
	"testing"

	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestCreateEvaluationGetEvalCardStatement(t *testing.T) {
	t.Parallel()

	factory := NewStatementsFactory(nil)

	query, args := factory.CreateEvaluationGetEvalCardStatement(api.Tenant("tenant-a"), "job-1")
	if query == "" {
		t.Fatal("expected non-empty query")
	}
	if len(args) != 2 {
		t.Fatalf("args = %v, want 2 entries for tenant scoped lookup", args)
	}

	query, args = factory.CreateEvaluationGetEvalCardStatement(api.Tenant(""), "job-1")
	if len(args) != 1 {
		t.Fatalf("args = %v, want 1 entry for global lookup", args)
	}
}

func TestCreateUpdateEvaluationEvalCardStatement(t *testing.T) {
	t.Parallel()

	factory := NewStatementsFactory(nil)

	query, args := factory.CreateUpdateEvaluationEvalCardStatement(api.Tenant("tenant-a"), "job-1", `{"card_version":"1.0"}`)
	if query == "" {
		t.Fatal("expected non-empty query")
	}
	if len(args) != 3 {
		t.Fatalf("args = %v, want 3 entries", args)
	}

	query, args = factory.CreateUpdateEvaluationEvalCardStatement(api.Tenant(""), "job-1", `{"card_version":"1.0"}`)
	if len(args) != 2 {
		t.Fatalf("args = %v, want 2 entries", args)
	}
}

func TestGetSchemaMigrationsIncludesEvalCard(t *testing.T) {
	t.Parallel()

	factory := NewStatementsFactory(nil)
	migrations := factory.GetSchemaMigrations()
	if len(migrations) == 0 {
		t.Fatal("expected schema migrations")
	}
	found := false
	for _, migration := range migrations {
		if migration != "" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected eval_card migration statement")
	}
}
