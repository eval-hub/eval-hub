package sql_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/common"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestUpdateEvaluationJobOCIPreservesResultsAndTenant(t *testing.T) {
	store, err := getTestStorage(t, "sqlite", getDBName())
	if err != nil {
		t.Fatal(err)
	}
	job := makeGitJob(common.GUID(), "main")
	job.Status.State = api.OverallStateCompleted
	job.Status.Message = &api.MessageInfo{Message: "Evaluation complete", MessageCode: "JOB_COMPLETED"}
	job.Exports = &api.EvaluationExports{OCI: &api.EvaluationExportsOCI{Coordinates: api.OCICoordinates{OCIHost: "quay.io", OCIRepository: "team/results"}}}
	job.Results = &api.EvaluationJobResults{Benchmarks: []api.BenchmarkResult{{ID: "bench-1", Metrics: map[string]any{"accuracy": 0.7}}}, Test: &api.EvaluationTest{Score: 0.7, Pass: true}}
	if err = store.CreateEvaluationJob(job); err != nil {
		t.Fatal(err)
	}
	scoped := store.WithTenant(job.Resource.Tenant)
	before, err := scoped.GetEvaluationJob(job.Resource.ID)
	if err != nil {
		t.Fatal(err)
	}
	statusBefore, _ := json.Marshal(before.Status)
	resultsBefore, _ := json.Marshal(before.Results)
	status := func(state api.OCIProcessingState) *api.OCIProcessingStatus {
		return &api.OCIProcessingStatus{State: state, Message: &api.MessageInfo{Message: "Card processing", MessageCode: "oci_card_test", MessageOrigin: api.MessageOriginServer}}
	}
	artifact := &api.OCIArtifactReference{OCIDigest: "sha256:" + strings.Repeat("a", 64), OCIReference: "quay.io/team/results:evaluation-card-test@sha256:" + strings.Repeat("a", 64)}
	err = store.WithTenant("other-tenant").UpdateEvaluationJobOCI(job.Resource.ID, status(api.OCIProcessingRunning), nil)
	_ = assertOCIServiceError(t, err, messages.ResourceNotFound, http.StatusNotFound)
	for _, state := range []api.OCIProcessingState{api.OCIProcessingRunning, api.OCIProcessingCompleted} {
		var card *api.OCIArtifactReference
		if state == api.OCIProcessingCompleted {
			card = artifact
		}
		if err = scoped.UpdateEvaluationJobOCI(job.Resource.ID, status(state), card); err != nil {
			t.Fatal(err)
		}
		beforeReplay, err := scoped.GetEvaluationJob(job.Resource.ID)
		if err != nil {
			t.Fatal(err)
		}
		// Replaying an identical status and artifact must succeed without changing results.
		if err = scoped.UpdateEvaluationJobOCI(job.Resource.ID, status(state), card); err != nil {
			t.Fatalf("repeat %s update: %v", state, err)
		}
		got, err := scoped.GetEvaluationJob(job.Resource.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Results.OCI.Status.State != state {
			t.Fatalf("state = %s", got.Results.OCI.Status.State)
		}
		if state != api.OCIProcessingRunning && !reflect.DeepEqual(got.Results.OCI.EvaluationCard, artifact) {
			t.Fatal("published reference was erased")
		}
		if got.Results.OCI.EvaluationBundle != nil || got.Results.OCI.Signing != nil {
			t.Fatal("invented downstream results")
		}
		if !got.Resource.UpdatedAt.Equal(beforeReplay.Resource.UpdatedAt) {
			t.Fatalf("repeat %s update changed UpdatedAt: before=%v after=%v", state, beforeReplay.Resource.UpdatedAt, got.Resource.UpdatedAt)
		}
		statusAfter, _ := json.Marshal(got.Status)
		got.Results.OCI = nil
		resultsAfter, _ := json.Marshal(got.Results)
		if string(statusBefore) != string(statusAfter) || string(resultsBefore) != string(resultsAfter) {
			t.Fatal("unrelated status or results changed")
		}
	}
	err = scoped.UpdateEvaluationJobOCI(job.Resource.ID, nil, nil)
	_ = assertOCIServiceError(t, err, messages.InternalServerError, http.StatusInternalServerError)
}

func TestUpdateEvaluationJobOCITerminalOutcomes(t *testing.T) {
	store, err := getTestStorage(t, "sqlite", getDBName())
	if err != nil {
		t.Fatal(err)
	}
	status := func(state api.OCIProcessingState) *api.OCIProcessingStatus {
		return &api.OCIProcessingStatus{State: state, Message: &api.MessageInfo{Message: "Card processing", MessageCode: "oci_card_test"}}
	}
	for _, terminal := range []api.OCIProcessingState{api.OCIProcessingCompleted, api.OCIProcessingFailed} {
		t.Run(string(terminal), func(t *testing.T) {
			job := makeGitJob(common.GUID(), "main")
			job.Status.State = api.OverallStateCompleted
			job.Exports = &api.EvaluationExports{OCI: &api.EvaluationExportsOCI{}}
			if err := store.CreateEvaluationJob(job); err != nil {
				t.Fatal(err)
			}
			scoped := store.WithTenant(job.Resource.Tenant)
			if err := scoped.UpdateEvaluationJobOCI(job.Resource.ID, status(api.OCIProcessingRunning), nil); err != nil {
				t.Fatal(err)
			}
			var card *api.OCIArtifactReference
			if terminal == api.OCIProcessingCompleted {
				card = &api.OCIArtifactReference{OCIDigest: "sha256:" + strings.Repeat("a", 64), OCIReference: "quay.io/team/results@sha256:" + strings.Repeat("a", 64)}
			}
			if err := scoped.UpdateEvaluationJobOCI(job.Resource.ID, status(terminal), card); err != nil {
				t.Fatal(err)
			}
			before, err := scoped.GetEvaluationJob(job.Resource.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, next := range []api.OCIProcessingState{api.OCIProcessingRunning, api.OCIProcessingCompleted, api.OCIProcessingFailed} {
				err := scoped.UpdateEvaluationJobOCI(job.Resource.ID, status(next), nil)
				if next == terminal {
					if err != nil {
						t.Fatalf("replay %s: %v", terminal, err)
					}
				} else {
					_ = assertOCIServiceError(t, err, messages.JobCanNotBeUpdated, http.StatusConflict)
				}
				after, err := scoped.GetEvaluationJob(job.Resource.ID)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(before, after) {
					t.Fatalf("%s -> %s changed persisted job", terminal, next)
				}
			}
		})
	}
}

func TestUpdateEvaluationJobOCIAcceptsCompletedAndPartiallyFailed(t *testing.T) {
	store, err := getTestStorage(t, "sqlite", getDBName())
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []api.OverallState{api.OverallStateCompleted, api.OverallStatePartiallyFailed} {
		t.Run(string(state), func(t *testing.T) {
			job := makeGitJob(common.GUID(), "main")
			job.Status.State = state
			job.Exports = &api.EvaluationExports{OCI: &api.EvaluationExportsOCI{}}
			if err := store.CreateEvaluationJob(job); err != nil {
				t.Fatal(err)
			}
			scoped := store.WithTenant(job.Resource.Tenant)
			status := &api.OCIProcessingStatus{State: api.OCIProcessingRunning, Message: &api.MessageInfo{Message: "Running", MessageCode: "OCI_RUNNING"}}
			if err := scoped.UpdateEvaluationJobOCI(job.Resource.ID, status, nil); err != nil {
				t.Fatal(err)
			}
			got, err := scoped.GetEvaluationJob(job.Resource.ID)
			if err != nil {
				t.Fatal(err)
			}
			want := &api.OCIProcessingStatus{State: api.OCIProcessingRunning, Message: &api.MessageInfo{Message: "Running", MessageCode: "OCI_RUNNING", MessageOrigin: api.MessageOriginServer}}
			if got.Status.State != state || got.Results == nil || got.Results.OCI == nil || !reflect.DeepEqual(got.Results.OCI.Status, want) {
				t.Fatal("OCI progress was not saved independently of evaluation state")
			}
		})
	}
}

func TestUpdateEvaluationJobOCINormalizesMessageOrigin(t *testing.T) {
	store, err := getTestStorage(t, "sqlite", getDBName())
	if err != nil {
		t.Fatal(err)
	}
	job := makeGitJob(common.GUID(), "main")
	job.Status.State = api.OverallStateCompleted
	job.Exports = &api.EvaluationExports{OCI: &api.EvaluationExportsOCI{}}
	if err := store.CreateEvaluationJob(job); err != nil {
		t.Fatal(err)
	}

	scoped := store.WithTenant(job.Resource.Tenant)
	status := &api.OCIProcessingStatus{
		State: api.OCIProcessingRunning,
		Message: &api.MessageInfo{
			Message:       "Generating evaluation card",
			MessageCode:   "OCI_RUNNING",
			MessageOrigin: api.MessageOriginRuntime,
		},
	}
	if err := scoped.UpdateEvaluationJobOCI(job.Resource.ID, status, nil); err != nil {
		t.Fatal(err)
	}
	if status.Message.MessageOrigin != api.MessageOriginRuntime {
		t.Fatalf("accepted update changed caller origin to %q", status.Message.MessageOrigin)
	}

	got, err := scoped.GetEvaluationJob(job.Resource.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Results.OCI.Status.Message.MessageOrigin != api.MessageOriginServer {
		t.Fatalf("expected server origin, got %q", got.Results.OCI.Status.Message.MessageOrigin)
	}

	// A duplicate update that omits the origin normalizes to the persisted value and is
	// therefore treated as an idempotent no-op.
	duplicate := &api.OCIProcessingStatus{
		State: api.OCIProcessingRunning,
		Message: &api.MessageInfo{
			Message:     "Generating evaluation card",
			MessageCode: "OCI_RUNNING",
		},
	}
	if err := scoped.UpdateEvaluationJobOCI(job.Resource.ID, duplicate, nil); err != nil {
		t.Fatal(err)
	}
	if duplicate.Message.MessageOrigin != "" {
		t.Fatalf("duplicate update changed caller origin to %q", duplicate.Message.MessageOrigin)
	}
}

func TestUpdateEvaluationJobOCIRejectsIneligibleJob(t *testing.T) {
	databaseName := getDBName()
	store, err := getTestStorage(t, "sqlite", databaseName)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name          string
		missingStatus bool
		completed     bool
		state         api.OverallState
		exports       *api.EvaluationExports
	}{
		{name: "missing status", missingStatus: true, exports: &api.EvaluationExports{OCI: &api.EvaluationExportsOCI{}}},
		{name: "running job", exports: &api.EvaluationExports{OCI: &api.EvaluationExportsOCI{}}},
		{name: "failed job", state: api.OverallStateFailed, exports: &api.EvaluationExports{OCI: &api.EvaluationExportsOCI{}}},
		{name: "cancelled job", state: api.OverallStateCancelled, exports: &api.EvaluationExports{OCI: &api.EvaluationExportsOCI{}}},
		{name: "missing exports", completed: true},
		{name: "missing OCI exports", completed: true, exports: &api.EvaluationExports{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			job := makeGitJob(common.GUID(), "main")
			if tc.completed {
				job.Status.State = api.OverallStateCompleted
			}
			if tc.state != "" {
				job.Status.State = tc.state
			}
			job.Exports = tc.exports
			if err := store.CreateEvaluationJob(job); err != nil {
				t.Fatal(err)
			}
			if tc.missingStatus {
				// Creation requires a status; simulate a stored job missing that field.
				db, err := sql.Open("sqlite", getDBInMemoryURL(databaseName))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = db.Close() })
				if _, err := db.Exec(`UPDATE evaluations SET entity = json_set(entity, '$.status', NULL) WHERE id = ?`, job.Resource.ID); err != nil {
					t.Fatal(err)
				}
			}
			scoped := store.WithTenant(job.Resource.Tenant)
			before, err := scoped.GetEvaluationJob(job.Resource.ID)
			if err != nil {
				t.Fatal(err)
			}
			status := &api.OCIProcessingStatus{State: api.OCIProcessingRunning, Message: &api.MessageInfo{Message: "Running", MessageCode: "OCI_RUNNING"}}
			err = scoped.UpdateEvaluationJobOCI(job.Resource.ID, status, nil)
			_ = assertOCIServiceError(t, err, messages.JobCanNotBeUpdated, http.StatusConflict)
			if status.Message.MessageOrigin != "" {
				t.Fatalf("rejected update changed caller origin to %q", status.Message.MessageOrigin)
			}
			after, err := scoped.GetEvaluationJob(job.Resource.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("rejected update changed persisted job")
			}
		})
	}
}

func assertOCIServiceError(t *testing.T, err error, code *messages.MessageCode, httpStatus int) abstractions.ServiceError {
	t.Helper()
	serviceErr, ok := err.(abstractions.ServiceError)
	if !ok {
		t.Fatalf("expected service error, got %T: %v", err, err)
	}
	if serviceErr.MessageCode() != code || serviceErr.MessageCode().GetStatusCode() != httpStatus {
		t.Fatalf("expected %s (HTTP %d), got %s (HTTP %d): %v", code.GetCode(), httpStatus, serviceErr.MessageCode().GetCode(), serviceErr.MessageCode().GetStatusCode(), err)
	}
	return serviceErr
}
