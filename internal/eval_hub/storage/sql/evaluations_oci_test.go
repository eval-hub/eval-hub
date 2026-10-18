package sql_test

import (
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/common"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/go-playground/validator/v10"
)

func ociStatus(state api.OCIProcessingState) *api.OCIProcessingStatus {
	return &api.OCIProcessingStatus{State: state, Message: &api.MessageInfo{Message: "OCI processing", MessageCode: "oci_test", MessageOrigin: api.MessageOriginServer}}
}

func ociArtifact(name, hex string) *api.OCIArtifactReference {
	digest := "sha256:" + strings.Repeat(hex, 64)
	return &api.OCIArtifactReference{OCIDigest: digest, OCIReference: "quay.io/team/" + name + "@" + digest}
}

func ociTestJob() *api.EvaluationJobResource {
	job := makeGitJob(common.GUID(), "main")
	job.Status.State = api.OverallStateCompleted
	job.Status.Message = &api.MessageInfo{Message: "Evaluation complete", MessageCode: "JOB_COMPLETED"}
	job.Status.Benchmarks = []api.BenchmarkStatus{{ID: "bench-1", ProviderID: "prov", Status: api.StateCompleted, Phase: api.JobPhaseCompleted}}
	job.Exports = &api.EvaluationExports{OCI: &api.EvaluationExportsOCI{Coordinates: api.OCICoordinates{OCIHost: "quay.io", OCIRepository: "team/results"}}}
	job.Results = &api.EvaluationJobResults{
		Benchmarks: []api.BenchmarkResult{{ID: "bench-1", ProviderID: "prov", Metrics: map[string]any{"accuracy": 0.7}, Artifacts: map[string]any{"samples": "samples.json"}, MLFlowRunID: "run-1"}},
		Test:       &api.EvaluationTest{Score: 0.7, Pass: true}, MLFlowExperimentURL: "https://mlflow.example/experiments/1",
		OCI: &api.EvaluationOCIResults{Signing: &api.OCISigningResult{Type: "key-pair"}},
	}
	return job
}

func createOCITestJob(t *testing.T, job *api.EvaluationJobResource) (abstractions.Storage, string) {
	t.Helper()
	databaseName := getDBName()
	store, err := getTestStorage(t, "sqlite", databaseName)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateEvaluationJob(job); err != nil {
		t.Fatal(err)
	}
	return store.WithTenant(job.Resource.Tenant), databaseName
}

func readOCITestJob(t *testing.T, store abstractions.Storage, id string) *api.EvaluationJobResource {
	t.Helper()
	job, err := store.GetEvaluationJob(id)
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func TestUpdateEvaluationJobOCIPreservesResultsAndTenant(t *testing.T) {
	job := ociTestJob()
	scoped, _ := createOCITestJob(t, job)
	before := readOCITestJob(t, scoped, job.Resource.ID)
	card, bundle, replacement := ociArtifact("card", "a"), ociArtifact("bundle", "b"), ociArtifact("bundle", "c")
	wantOCI := *before.Results.OCI
	for _, tc := range []struct {
		name         string
		state        api.OCIProcessingState
		card, bundle *api.OCIArtifactReference
	}{
		{"running without outputs", api.OCIProcessingRunning, nil, nil},
		{"complete both outputs atomically", api.OCIProcessingCompleted, card, bundle},
		{"replace bundle only", api.OCIProcessingCompleted, card, replacement},
		{"replace card only", api.OCIProcessingCompleted, ociArtifact("card", "d"), nil},
		{"status only preserves both", api.OCIProcessingRunning, nil, nil},
		{"failed preserves unavailable bundle", api.OCIProcessingFailed, card, nil},
		{"failed with both published outputs", api.OCIProcessingFailed, card, bundle},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status := ociStatus(tc.state)
			if err := scoped.UpdateEvaluationJobOCI(job.Resource.ID, status, tc.card, tc.bundle); err != nil {
				t.Fatal(err)
			}
			wantOCI.Status = status
			if tc.card != nil {
				wantOCI.EvaluationCard = tc.card
			}
			if tc.bundle != nil {
				wantOCI.EvaluationBundle = tc.bundle
			}
			got := readOCITestJob(t, scoped, job.Resource.ID)
			if !reflect.DeepEqual(got.Results.OCI, &wantOCI) {
				t.Fatalf("OCI = %#v, want %#v", got.Results.OCI, &wantOCI)
			}
			// Compare the whole persisted job except the intended OCI changes and update time.
			got.Results.OCI = before.Results.OCI
			got.Resource.UpdatedAt = before.Resource.UpdatedAt
			if !reflect.DeepEqual(got, before) {
				t.Fatalf("unrelated job fields changed: got %#v, want %#v", got, before)
			}
		})
	}
	before = readOCITestJob(t, scoped, job.Resource.ID)
	if err := scoped.WithTenant("other-tenant").UpdateEvaluationJobOCI(job.Resource.ID, ociStatus(api.OCIProcessingRunning), card, replacement); err == nil {
		t.Fatal("cross-tenant update succeeded")
	}
	after := readOCITestJob(t, scoped, job.Resource.ID)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("cross-tenant attempt changed owner job")
	}
	_, missingErr := scoped.GetEvaluationJob("unknown-job")
	updateErr := scoped.UpdateEvaluationJobOCI("unknown-job", ociStatus(api.OCIProcessingCompleted), card, bundle)
	if updateErr == nil || missingErr == nil || updateErr.Error() != missingErr.Error() {
		t.Fatalf("unknown job error = %v, want %v", updateErr, missingErr)
	}
}

func TestUpdateEvaluationJobOCIIndependentOutputs(t *testing.T) {
	for _, output := range []string{"none", "card", "bundle"} {
		t.Run(output, func(t *testing.T) {
			job := ociTestJob()
			job.Results = nil
			scoped, _ := createOCITestJob(t, job)
			card, bundle := ociArtifact("card", "a"), ociArtifact("bundle", "b")
			var suppliedCard, suppliedBundle *api.OCIArtifactReference
			if output == "card" {
				suppliedCard = card
			}
			if output == "bundle" {
				suppliedBundle = bundle
			}
			status := ociStatus(api.OCIProcessingFailed)
			if err := scoped.UpdateEvaluationJobOCI(job.Resource.ID, status, suppliedCard, suppliedBundle); err != nil {
				t.Fatal(err)
			}
			got := readOCITestJob(t, scoped, job.Resource.ID)
			want := &api.EvaluationOCIResults{Status: status, EvaluationCard: suppliedCard, EvaluationBundle: suppliedBundle}
			if !reflect.DeepEqual(got.Results.OCI, want) {
				t.Fatalf("OCI = %#v, want %#v", got.Results.OCI, want)
			}
			data, err := json.Marshal(got.Results.OCI)
			if err != nil {
				t.Fatal(err)
			}
			for field, absent := range map[string]bool{"evaluation_card": suppliedCard == nil, "evaluation_bundle": suppliedBundle == nil, "signing": true} {
				if strings.Contains(string(data), `"`+field+`"`) == absent {
					t.Fatalf("unexpected optional field %s in %s", field, data)
				}
			}
			// Adding a bundle with identical status/card must still write.
			if err := scoped.UpdateEvaluationJobOCI(job.Resource.ID, status, suppliedCard, bundle); err != nil {
				t.Fatal(err)
			}
			got = readOCITestJob(t, scoped, job.Resource.ID)
			if !reflect.DeepEqual(got.Results.OCI.EvaluationBundle, bundle) {
				t.Fatal("bundle-only change was lost")
			}
		})
	}
}

func TestUpdateEvaluationJobOCIRejectsInvalidInputsAtomically(t *testing.T) {
	job := ociTestJob()
	job.Results.OCI.Status = ociStatus(api.OCIProcessingRunning)
	job.Results.OCI.EvaluationCard = ociArtifact("old-card", "a")
	job.Results.OCI.EvaluationBundle = ociArtifact("old-bundle", "b")
	scoped, _ := createOCITestJob(t, job)
	before := readOCITestJob(t, scoped, job.Resource.ID)
	valid := ociArtifact("new", "c")
	for _, output := range []string{"card", "bundle"} {
		for _, invalid := range []struct {
			name string
			ref  *api.OCIArtifactReference
			tag  string
		}{
			{"empty", &api.OCIArtifactReference{}, "required"},
			{"malformed digest", &api.OCIArtifactReference{OCIDigest: "sha256:abc", OCIReference: valid.OCIReference}, "sha256_digest"},
			{"missing reference", &api.OCIArtifactReference{OCIDigest: valid.OCIDigest}, "required"},
			{"whitespace reference", &api.OCIArtifactReference{OCIDigest: valid.OCIDigest, OCIReference: " \t\n"}, "notblank"},
		} {
			for _, companion := range []bool{false, true} {
				name := output + "/" + invalid.name
				if companion {
					name += "/valid companion"
				}
				t.Run(name, func(t *testing.T) {
					var card, bundle *api.OCIArtifactReference
					if companion {
						card, bundle = valid, valid
					}
					if output == "card" {
						card = invalid.ref
					} else {
						bundle = invalid.ref
					}
					err := scoped.UpdateEvaluationJobOCI(job.Resource.ID, ociStatus(api.OCIProcessingCompleted), card, bundle)
					var validationErrors validator.ValidationErrors
					if !errors.As(err, &validationErrors) || len(validationErrors) == 0 || validationErrors[0].Tag() != invalid.tag || !strings.Contains(err.Error(), "evaluation "+output+" reference") {
						t.Fatalf("expected contextual %s validation error, got %v", invalid.tag, err)
					}
					if !reflect.DeepEqual(before, readOCITestJob(t, scoped, job.Resource.ID)) {
						t.Fatal("invalid input changed persisted job")
					}
				})
			}
		}
	}
	for _, tc := range []struct {
		name   string
		status *api.OCIProcessingStatus
	}{
		{"nil", nil}, {"missing message", &api.OCIProcessingStatus{State: api.OCIProcessingCompleted}},
		{"empty message", &api.OCIProcessingStatus{State: api.OCIProcessingCompleted, Message: &api.MessageInfo{MessageCode: "oci_test"}}},
		{"empty code", &api.OCIProcessingStatus{State: api.OCIProcessingCompleted, Message: &api.MessageInfo{Message: "Processing"}}},
		{"empty state", ociStatus("")}, {"pending", ociStatus("pending")},
		{"card phase", ociStatus("generating_evaluation_card")}, {"bundle phase", ociStatus("generating_evaluation_bundle")}, {"signing", ociStatus("signing")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := scoped.UpdateEvaluationJobOCI(job.Resource.ID, tc.status, valid, valid); err == nil {
				t.Fatal("invalid status accepted")
			}
			if !reflect.DeepEqual(before, readOCITestJob(t, scoped, job.Resource.ID)) {
				t.Fatal("invalid status changed persisted job")
			}
		})
	}
}

func TestUpdateEvaluationJobOCIRejectsIneligibleJob(t *testing.T) {
	for _, tc := range []struct {
		name    string
		state   api.OverallState
		exports *api.EvaluationExports
	}{
		{"running", api.OverallStateRunning, ociTestJob().Exports},
		{"partially failed", api.OverallStatePartiallyFailed, ociTestJob().Exports},
		{"failed", api.OverallStateFailed, ociTestJob().Exports},
		{"cancelled", api.OverallStateCancelled, ociTestJob().Exports},
		{"no exports", api.OverallStateCompleted, nil},
		{"no OCI export", api.OverallStateCompleted, &api.EvaluationExports{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			job := ociTestJob()
			job.Status.State, job.Exports = tc.state, tc.exports
			scoped, _ := createOCITestJob(t, job)
			before := readOCITestJob(t, scoped, job.Resource.ID)
			if err := scoped.UpdateEvaluationJobOCI(job.Resource.ID, ociStatus(api.OCIProcessingCompleted), ociArtifact("card", "a"), ociArtifact("bundle", "b")); err == nil {
				t.Fatal("ineligible job accepted")
			}
			if !reflect.DeepEqual(before, readOCITestJob(t, scoped, job.Resource.ID)) {
				t.Fatal("ineligible update changed persisted job")
			}
		})
	}
}

func TestUpdateEvaluationJobOCINoopAndDatabaseFailure(t *testing.T) {
	job := ociTestJob()
	status := ociStatus(api.OCIProcessingCompleted)
	card, bundle := ociArtifact("card", "a"), ociArtifact("bundle", "b")
	job.Results.OCI.Status, job.Results.OCI.EvaluationCard, job.Results.OCI.EvaluationBundle = status, card, bundle
	scoped, databaseName := createOCITestJob(t, job)
	before := readOCITestJob(t, scoped, job.Resource.ID)
	triggerDB, err := sql.Open("sqlite", getDBInMemoryURL(databaseName))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = triggerDB.Close() })
	// This database contains only this job; rejecting every UPDATE observes no-ops
	// and exercises a real database write failure without production injection hooks.
	_, err = triggerDB.Exec(`CREATE TRIGGER reject_oci_update BEFORE UPDATE ON evaluations
 BEGIN SELECT RAISE(FAIL, 'reject OCI update'); END;`)
	if err != nil {
		t.Fatal(err)
	}
	for _, outputs := range []struct{ card, bundle *api.OCIArtifactReference }{{card, bundle}, {nil, nil}, {card, nil}, {nil, bundle}} {
		if err := scoped.UpdateEvaluationJobOCI(job.Resource.ID, status, outputs.card, outputs.bundle); err != nil {
			t.Fatalf("no-op attempted UPDATE: %v", err)
		}
	}
	for _, change := range []struct {
		name         string
		status       *api.OCIProcessingStatus
		card, bundle *api.OCIArtifactReference
	}{
		{"bundle only", status, card, ociArtifact("new-bundle", "c")},
		{"all outputs", ociStatus(api.OCIProcessingFailed), ociArtifact("new-card", "d"), ociArtifact("new-bundle", "e")},
	} {
		t.Run(change.name, func(t *testing.T) {
			err := scoped.UpdateEvaluationJobOCI(job.Resource.ID, change.status, change.card, change.bundle)
			if err == nil || !strings.Contains(err.Error(), "reject OCI update") {
				t.Fatalf("expected database write failure, got %v", err)
			}
			if !reflect.DeepEqual(before, readOCITestJob(t, scoped, job.Resource.ID)) {
				t.Fatal("failed database write changed persisted job")
			}
		})
	}
}
