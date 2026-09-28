package postgres

import (
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/storage/sql/shared"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestCreateDeleteSystemEntitiesStatement(t *testing.T) {
	f := NewStatementsFactory(slog.Default())
	stmt, args := f.CreateDeleteSystemEntitiesStatement(shared.TableProviders)
	if !strings.Contains(stmt, "DELETE FROM providers") {
		t.Fatalf("unexpected statement: %s", stmt)
	}
	if !strings.Contains(stmt, "owner = $1") {
		t.Fatalf("expected owner placeholder $1, got: %s", stmt)
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
	if len(args) != 6 {
		t.Fatalf("expected 6 args, got %d: %#v", len(args), args)
	}
	if args[3] != created || args[4] != updated {
		t.Fatalf("timestamp args mismatch: %#v", args)
	}
}

func TestCreateCollectionAddEntityStatementIncludesTimestamps(t *testing.T) {
	f := NewStatementsFactory(slog.Default())
	created := time.Date(2024, 3, 4, 5, 6, 7, 0, time.UTC)
	updated := time.Date(2024, 4, 5, 6, 7, 8, 0, time.UTC)
	collection := &api.CollectionResource{
		Resource: api.Resource{
			ID:        "c1",
			Tenant:    "t1",
			Owner:     "system",
			CreatedAt: created,
			UpdatedAt: updated,
		},
	}
	stmt, args := f.CreateCollectionAddEntityStatement(collection, `{"name":"n"}`)
	if !strings.Contains(stmt, "created_at") || !strings.Contains(stmt, "updated_at") {
		t.Fatalf("expected created_at/updated_at columns in: %s", stmt)
	}
	if len(args) != 6 {
		t.Fatalf("expected 6 args, got %d: %#v", len(args), args)
	}
	if args[3] != created || args[4] != updated {
		t.Fatalf("timestamp args mismatch: %#v", args)
	}
}

func TestGetAllowedFilterColumns_IncludesCollectionFilters(t *testing.T) {
	f := NewStatementsFactory(slog.Default())
	cols := f.GetAllowedFilterColumns(shared.TableCollections)
	required := []string{"domains", "tasks", "modalities", "industries", "evaluation_targets"}
	colSet := make(map[string]struct{}, len(cols))
	for _, c := range cols {
		colSet[c] = struct{}{}
	}
	for _, r := range required {
		if _, ok := colSet[r]; !ok {
			t.Errorf("GetAllowedFilterColumns missing %q for collections", r)
		}
	}
}

func TestCreateEntityFilterCondition_ArrayFields(t *testing.T) {
	f := NewStatementsFactory(slog.Default())
	for _, key := range []string{"domains", "tasks", "modalities", "industries", "evaluation_targets"} {
		cond, args := f.CreateEntityFilterCondition(key, "rag", 1, shared.TableCollections)
		if cond == "" {
			t.Errorf("empty condition for key %q", key)
		}
		if len(args) != 1 || args[0] != "rag" {
			t.Errorf("expected 1 arg 'rag' for key %q, got %v", key, args)
		}
	}
}

func TestCreateEntityFilterCondition_MultiValueArrayField(t *testing.T) {
	f := NewStatementsFactory(slog.Default())
	cond, args := f.CreateEntityFilterCondition("domains", []string{"rag", "grounding"}, 1, shared.TableCollections)
	if cond == "" {
		t.Error("expected non-empty condition for multi-value domains filter")
	}
	if len(args) != 2 {
		t.Errorf("expected 2 args for 2 values, got %v", args)
	}
	if args[0] != "rag" || args[1] != "grounding" {
		t.Errorf("expected args [rag grounding], got %v", args)
	}
	if !strings.Contains(cond, "AND") {
		t.Errorf("multi-value condition should contain AND, got %q", cond)
	}
}

func TestGetAllowedFilterColumns_Providers(t *testing.T) {
	f := NewStatementsFactory(slog.Default())
	cols := f.GetAllowedFilterColumns(shared.TableProviders)
	if len(cols) == 0 {
		t.Error("expected non-empty filter columns for providers")
	}
}

func TestGetLogger(t *testing.T) {
	logger := slog.Default()
	f := NewStatementsFactory(logger)
	if f.GetLogger() == nil {
		t.Error("GetLogger should return non-nil logger")
	}
}

func TestGetTablesSchema(t *testing.T) {
	f := NewStatementsFactory(slog.Default())
	schema := f.GetTablesSchema()
	if !strings.Contains(schema, "CREATE TABLE") {
		t.Errorf("GetTablesSchema should return CREATE TABLE statement, got: %s", schema[:min(100, len(schema))])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestGetAllowedFilterColumns_Evaluations(t *testing.T) {
	f := NewStatementsFactory(slog.Default())
	cols := f.GetAllowedFilterColumns(shared.TableEvaluations)
	if !slices.Contains(cols, "collection_id") {
		t.Errorf("GetAllowedFilterColumns missing collection_id: %v", cols)
	}

	cond, args := f.CreateEntityFilterCondition("collection_id", "collection-1", 1, shared.TableEvaluations)
	if !strings.Contains(cond, "entity->'config'->'collection'->>'id'") {
		t.Errorf("unexpected collection_id condition: %q", cond)
	}
	if len(args) != 1 || args[0] != "collection-1" {
		t.Errorf("unexpected collection_id args: %v", args)
	}

	cond, args = f.CreateEntityFilterCondition("collection_id", "collection-1", 1, shared.TableCollections)
	if cond != "" || len(args) != 0 {
		t.Errorf("collection_id condition for collections = %q, %v; want empty", cond, args)
	}
}

func TestCreateCountEntitiesStatement(t *testing.T) {
	f := NewStatementsFactory(slog.Default())
	stmt, _ := f.CreateCountEntitiesStatement("t1", shared.TableCollections, map[string]any{})
	if !strings.Contains(stmt, "SELECT COUNT(*)") {
		t.Errorf("expected COUNT(*), got: %s", stmt)
	}
}

func TestCreateListEntitiesStatement(t *testing.T) {
	f := NewStatementsFactory(slog.Default())
	stmt, _ := f.CreateListEntitiesStatement("t1", shared.TableCollections, 10, 0, map[string]any{})
	if !strings.Contains(stmt, "SELECT") {
		t.Errorf("expected SELECT, got: %s", stmt)
	}
}

func TestCreateCollectionGetEntityForUpdateStatement(t *testing.T) {
	f := NewStatementsFactory(slog.Default())
	query := &shared.EntityQuery{Resource: api.Resource{ID: "collection-1", Tenant: "tenant-1"}}
	stmt, _, _ := f.CreateCollectionGetEntityForUpdateStatement(query)
	if !strings.HasSuffix(stmt, "FOR UPDATE;") {
		t.Errorf("expected FOR UPDATE statement, got %q", stmt)
	}
}

func TestCreateDeleteEntityStatement(t *testing.T) {
	f := NewStatementsFactory(slog.Default())
	stmt, args := f.CreateDeleteEntityStatement("t1", shared.TableCollections, "coll-1")
	if !strings.Contains(stmt, "DELETE FROM collections") {
		t.Errorf("expected DELETE FROM collections, got: %s", stmt)
	}
	if len(args) == 0 {
		t.Error("expected args for delete statement")
	}
}

func TestCreateUpdateEntityStatement(t *testing.T) {
	f := NewStatementsFactory(slog.Default())
	entity := `{"name":"test"}`
	stmt, args := f.CreateUpdateEntityStatement("t1", shared.TableCollections, "coll-1", entity, nil)
	if !strings.Contains(stmt, "UPDATE collections") {
		t.Errorf("expected UPDATE collections, got: %s", stmt)
	}
	if len(args) == 0 {
		t.Error("expected args for update statement")
	}
}

func TestCreateEvaluationAddEntityStatement(t *testing.T) {
	f := NewStatementsFactory(slog.Default())
	state := api.OverallStatePending
	eval := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{Resource: api.Resource{ID: "e1", Tenant: "t1", Owner: "u1"}},
		Status:   &api.EvaluationJobStatus{EvaluationJobState: api.EvaluationJobState{State: state}},
	}
	stmt, args := f.CreateEvaluationAddEntityStatement(eval, `{}`)
	if !strings.Contains(stmt, "INSERT INTO evaluations") {
		t.Errorf("expected INSERT INTO evaluations, got: %s", stmt)
	}
	if len(args) == 0 {
		t.Error("expected args")
	}
}

func TestCreateProviderGetEntityStatement(t *testing.T) {
	f := NewStatementsFactory(slog.Default())
	query := &shared.EntityQuery{Resource: api.Resource{ID: "p1", Tenant: "t1"}}
	stmt, _, scanArgs := f.CreateProviderGetEntityStatement(query)
	if !strings.Contains(stmt, "SELECT") || !strings.Contains(stmt, "providers") {
		t.Errorf("unexpected statement: %s", stmt)
	}
	if len(scanArgs) == 0 {
		t.Error("expected scan args")
	}
}

func TestCreateCollectionGetEntityStatement(t *testing.T) {
	f := NewStatementsFactory(slog.Default())
	query := &shared.EntityQuery{Resource: api.Resource{ID: "c1", Tenant: "t1"}}
	stmt, _, scanArgs := f.CreateCollectionGetEntityStatement(query)
	if !strings.Contains(stmt, "SELECT") || !strings.Contains(stmt, "collections") {
		t.Errorf("unexpected statement: %s", stmt)
	}
	if len(scanArgs) == 0 {
		t.Error("expected scan args")
	}
}

func TestCreateCollectionAddEntityStatement(t *testing.T) {
	f := NewStatementsFactory(slog.Default())
	coll := &api.CollectionResource{
		Resource: api.Resource{ID: "c1", Tenant: "t1", Owner: "u1", CreatedAt: time.Now(), UpdatedAt: time.Now()},
	}
	stmt, args := f.CreateCollectionAddEntityStatement(coll, `{}`)
	if !strings.Contains(stmt, "INSERT INTO collections") {
		t.Errorf("expected INSERT INTO collections, got: %s", stmt)
	}
	if len(args) == 0 {
		t.Error("expected args")
	}
}
