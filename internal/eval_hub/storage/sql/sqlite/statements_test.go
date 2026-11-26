package sqlite

import (
	"database/sql"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/storage/sql/shared"
	"github.com/eval-hub/eval-hub/internal/eval_hub/workloads"
	"github.com/eval-hub/eval-hub/pkg/api"
	_ "modernc.org/sqlite"
)

func TestCreateDeleteSystemEntitiesStatement(t *testing.T) {
	f := NewStatementsFactory(slog.Default())
	stmt, args := f.CreateDeleteSystemEntitiesStatement(shared.TableCollections)
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

func TestExpiredGenerationCleanupUsesExpiryIndex(t *testing.T) {
	db, err := sql.Open("sqlite", "file:generation-expiry-plan?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close test database: %v", err)
		}
	})
	db.SetMaxOpenConns(1)
	factory := NewStatementsFactory(slog.Default())
	if _, err := db.Exec(factory.GetTablesSchema()); err != nil {
		t.Fatal(err)
	}
	if err := EnsureEvaluationExpiryColumn(db); err != nil {
		t.Fatal(err)
	}
	if err := EnsureEvaluationCleanupProtectionColumn(db); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	insert, err := tx.Prepare(`INSERT INTO evaluations (id, tenant_id, owner, status, experiment_id, workload_type, expires_at, entity) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 20_000 {
		workloadType := string(workloads.Evaluation)
		if i%2 == 0 {
			workloadType = string(workloads.CollectionGeneration)
		}
		if _, err := insert.Exec(fmt.Sprintf("job-%05d", i), "tenant", "owner", string(api.OverallStateCompleted), "", workloadType, "2026-01-01T00:00:00.000000000Z", "{}"); err != nil {
			t.Fatalf("insert fixture row %d: %v", i, err)
		}
	}
	if err := insert.Close(); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("ANALYZE"); err != nil {
		t.Fatal(err)
	}
	deleteQuery, args := factory.CreateDeleteExpiredGenerationJobsStatement(time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), 100)
	rows, err := db.Query("EXPLAIN QUERY PLAN "+deleteQuery, args...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := rows.Close(); err != nil {
			t.Errorf("close query plan rows: %v", err)
		}
	})
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(plan, " | ")
	if !strings.Contains(joined, "idx_evaluations_generation_expiry") || strings.Contains(joined, "USE TEMP B-TREE") {
		t.Fatalf("expiry cleanup query plan does not use the ordered partial index: %s", joined)
	}
}

func TestEnsureEvaluationCleanupProtectionColumnMigratesExistingDatabase(t *testing.T) {
	db, err := sql.Open("sqlite", "file:generation-cleanup-migration?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec("CREATE TABLE evaluations (id TEXT PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	if err := EnsureEvaluationCleanupProtectionColumn(db); err != nil {
		t.Fatalf("EnsureEvaluationCleanupProtectionColumn(): %v", err)
	}
	if err := EnsureEvaluationCleanupProtectionColumn(db); err != nil {
		t.Fatalf("EnsureEvaluationCleanupProtectionColumn() on an already migrated database: %v", err)
	}
	var columns int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('evaluations') WHERE name = 'cleanup_protected_until'`).Scan(&columns); err != nil {
		t.Fatal(err)
	}
	if columns != 1 {
		t.Fatalf("cleanup protection columns = %d, want 1", columns)
	}
}

func TestEnsureEvaluationCleanupProtectionColumnReturnsSchemaError(t *testing.T) {
	db, err := sql.Open("sqlite", "file:generation-cleanup-missing-table?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := EnsureEvaluationCleanupProtectionColumn(db); err == nil {
		t.Fatal("EnsureEvaluationCleanupProtectionColumn() on a closed database returned nil")
	}
}

func TestEnsureEvaluationCleanupProtectionColumnReportsMigrationFailure(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "generation-cleanup-readonly.db")
	setup, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := setup.Exec("CREATE TABLE evaluations (id TEXT PRIMARY KEY)"); err != nil {
		_ = setup.Close()
		t.Fatal(err)
	}
	if err := setup.Close(); err != nil {
		t.Fatal(err)
	}

	readOnly, err := sql.Open("sqlite", "file:"+databasePath+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = readOnly.Close() })
	if err := EnsureEvaluationCleanupProtectionColumn(readOnly); err == nil {
		t.Fatal("EnsureEvaluationCleanupProtectionColumn() on a read-only database returned nil")
	}
}

func TestSetCollectionGenerationCleanupProtectionIsTenantScopedAndPreservesExpiry(t *testing.T) {
	factory := NewStatementsFactory(slog.Default())
	protectedUntil := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	statement, args := factory.CreateSetCollectionGenerationCleanupProtectionStatement("tenant-a", "job-1", protectedUntil)
	if !strings.Contains(statement, "cleanup_protected_until = ?") || !strings.Contains(statement, "tenant_id = ?") || strings.Contains(statement, "owner =") || strings.Contains(statement, "expires_at =") {
		t.Fatalf("unexpected cleanup protection statement: %s", statement)
	}
	if got := args[0]; got != protectedUntil.Format(sqliteExpiryTimestampFormat) {
		t.Fatalf("protected-until argument = %#v", got)
	}
	if !slices.Equal(args[1:], []any{"job-1", "tenant-a"}) {
		t.Fatalf("cleanup protection args = %#v", args)
	}
}

func TestGenerationStatementsKeepExpiryOnSubsequentUpdates(t *testing.T) {
	db, err := sql.Open("sqlite", "file:generation-expiry-statement?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	factory := NewStatementsFactory(slog.Default())
	if _, err := db.Exec(factory.GetTablesSchema()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO evaluations (id, tenant_id, owner, status, experiment_id, workload_type, entity)
		VALUES ('job', 'tenant', 'owner', 'running', '', 'collection-generation', '{}')`); err != nil {
		t.Fatal(err)
	}
	firstExpiry := time.Now().UTC().Add(time.Hour)
	laterExpiry := firstExpiry.Add(time.Hour)
	for _, expires := range []*time.Time{nil, &firstExpiry, &laterExpiry} {
		statement, args := factory.CreateUpdateEvaluationEntityStatement("tenant", "job", "{}", api.OverallStateFailed, expires)
		if _, err := db.Exec(statement, args...); err != nil {
			t.Fatal(err)
		}
	}
	var preserved string
	if err := db.QueryRow("SELECT CAST(expires_at AS TEXT) FROM evaluations WHERE id = 'job'").Scan(&preserved); err != nil {
		t.Fatal(err)
	}
	if want := firstExpiry.Format(sqliteExpiryTimestampFormat); preserved != want {
		t.Fatalf("expiry = %q, want %q", preserved, want)
	}
	query := shared.EntityQuery{Resource: api.Resource{ID: "job", Tenant: "tenant"}, WorkloadType: workloads.CollectionGeneration}
	statement, args, scan := factory.CreateEvaluationGetEntityForUpdateStatement(&query)
	if err := db.QueryRow(statement, args...).Scan(scan...); err != nil {
		t.Fatalf("unexpired job is unavailable: %v", err)
	}
	statement, args = factory.CreateDeleteCleanedCollectionGenerationJobStatement("different-tenant", "job")
	deletion, err := db.Exec(statement, args...)
	if err != nil {
		t.Fatal(err)
	}
	if affected, err := deletion.RowsAffected(); err != nil || affected != 0 {
		t.Fatalf("cleanup removed another tenant's job: rows=%d err=%v", affected, err)
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
	// AND semantics: both values must appear in the generated condition
	if !strings.Contains(cond, "AND") {
		t.Errorf("multi-value condition should contain AND, got %q", cond)
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
		t.Errorf("GetTablesSchema should return CREATE TABLE statement")
	}
}

func TestGetAllowedFilterColumns_Evaluations(t *testing.T) {
	f := NewStatementsFactory(slog.Default())
	cols := f.GetAllowedFilterColumns(shared.TableEvaluations)
	if !slices.Contains(cols, "collection_id") {
		t.Errorf("GetAllowedFilterColumns missing collection_id: %v", cols)
	}

	cond, args := f.CreateEntityFilterCondition("collection_id", "collection-1", 1, shared.TableEvaluations)
	if !strings.Contains(cond, "$.config.collection.id") {
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

func TestGetAllowedFilterColumns_Providers(t *testing.T) {
	f := NewStatementsFactory(slog.Default())
	cols := f.GetAllowedFilterColumns(shared.TableProviders)
	if len(cols) == 0 {
		t.Error("expected non-empty filter columns for providers")
	}
}

func TestCreateCountEntitiesStatement(t *testing.T) {
	f := NewStatementsFactory(slog.Default())
	stmt, _ := f.CreateCountEntitiesStatement("t1", shared.TableCollections, map[string]any{})
	if !strings.Contains(stmt, "SELECT COUNT(*)") {
		t.Errorf("expected COUNT(*), got: %s", stmt)
	}
	evaluationStmt, _ := f.CreateCountEntitiesStatement("t1", shared.TableEvaluations, map[string]any{})
	if !strings.Contains(evaluationStmt, "workload_type = 'evaluation'") {
		t.Errorf("evaluation-job count should exclude post-processing jobs, got: %s", evaluationStmt)
	}
}

func TestCreateListEntitiesStatement(t *testing.T) {
	f := NewStatementsFactory(slog.Default())
	stmt, _ := f.CreateListEntitiesStatement("t1", shared.TableCollections, 10, 0, map[string]any{}, "")
	if !strings.Contains(stmt, "ORDER BY id DESC") {
		t.Errorf("default statement should preserve id ordering, got: %s", stmt)
	}
	evaluationStmt, _ := f.CreateListEntitiesStatement("t1", shared.TableEvaluations, 10, 0, map[string]any{}, "")
	if !strings.Contains(evaluationStmt, "workload_type = 'evaluation'") {
		t.Errorf("evaluation-job list should exclude post-processing jobs, got: %s", evaluationStmt)
	}

	curatedStmt, _ := f.CreateListEntitiesStatement("t1", shared.TableCollections, 10, 0, map[string]any{}, "curation_order")
	if !strings.Contains(curatedStmt, "CASE WHEN json_extract(entity, '$.curation_order') > 0 THEN 0 ELSE 1 END") {
		t.Errorf("expected JSON curation order, got: %s", curatedStmt)
	}
}

func TestCreateCollectionGetEntityForUpdateStatement(t *testing.T) {
	f := NewStatementsFactory(slog.Default())
	query := &shared.EntityQuery{Resource: api.Resource{ID: "collection-1", Tenant: "tenant-1"}}
	stmt, args, scanArgs := f.CreateCollectionGetEntityForUpdateStatement(query)
	wantStmt, wantArgs, wantScanArgs := f.CreateCollectionGetEntityStatement(query)
	if stmt != wantStmt || len(args) != len(wantArgs) || len(scanArgs) != len(wantScanArgs) {
		t.Errorf("collection FOR UPDATE statement = (%q, %v, %v), want (%q, %v, %v)", stmt, args, scanArgs, wantStmt, wantArgs, wantScanArgs)
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
	stmt, args := f.CreateUpdateEntityStatement("t1", shared.TableCollections, "coll-1", `{"name":"test"}`, nil)
	if !strings.Contains(stmt, "UPDATE collections") {
		t.Errorf("expected UPDATE collections, got: %s", stmt)
	}
	if len(args) == 0 {
		t.Error("expected args for update statement")
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

func TestCreateEvaluationAddEntityStatement(t *testing.T) {
	f := NewStatementsFactory(slog.Default())
	state := api.OverallStatePending
	eval := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{Resource: api.Resource{ID: "e1", Tenant: "t1", Owner: "u1"}},
		Status:   &api.EvaluationJobStatus{EvaluationJobState: api.EvaluationJobState{State: state}},
	}
	stmt, args := f.CreateEvaluationAddEntityStatement(eval, `{}`, workloads.Evaluation)
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

func TestCreateEvaluationGetEntityStatement(t *testing.T) {
	f := NewStatementsFactory(slog.Default())
	query := &shared.EntityQuery{Resource: api.Resource{ID: "e1", Tenant: "t1"}}
	stmt, _, scanArgs := f.CreateEvaluationGetEntityStatement(query)
	if !strings.Contains(stmt, "SELECT") || !strings.Contains(stmt, "evaluations") {
		t.Errorf("unexpected statement: %s", stmt)
	}
	if len(scanArgs) == 0 {
		t.Error("expected scan args")
	}
}
