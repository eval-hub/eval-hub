package sql_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/common"
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
		return &api.OCIProcessingStatus{State: state, Message: &api.MessageInfo{Message: "Card processing", MessageCode: "OCI_CARD_TEST", MessageOrigin: api.MessageOriginServer}}
	}
	artifact := &api.OCIArtifactReference{OCIDigest: "sha256:" + strings.Repeat("a", 64), OCIReference: "quay.io/team/results:evaluation-card-test@sha256:" + strings.Repeat("a", 64)}
	if err = store.WithTenant("other-tenant").UpdateEvaluationJobOCI(job.Resource.ID, status(api.OCIProcessingPending), nil); err == nil {
		t.Fatal("cross-tenant update succeeded")
	}
	for _, state := range []api.OCIProcessingState{api.OCIProcessingPending, api.OCIProcessingGeneratingEvaluationCard, api.OCIProcessingCompleted, api.OCIProcessingFailed} {
		var card *api.OCIArtifactReference
		if state == api.OCIProcessingGeneratingEvaluationCard {
			card = artifact
		}
		if err = scoped.UpdateEvaluationJobOCI(job.Resource.ID, status(state), card); err != nil {
			t.Fatal(err)
		}
		got, err := scoped.GetEvaluationJob(job.Resource.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Results.OCI.Status.State != state {
			t.Fatalf("state = %s", got.Results.OCI.Status.State)
		}
		if state != api.OCIProcessingPending && !reflect.DeepEqual(got.Results.OCI.EvaluationCard, artifact) {
			t.Fatal("published reference was erased")
		}
		if got.Results.OCI.EvaluationBundle != nil || got.Results.OCI.Signing != nil {
			t.Fatal("invented downstream results")
		}
		statusAfter, _ := json.Marshal(got.Status)
		got.Results.OCI = nil
		resultsAfter, _ := json.Marshal(got.Results)
		if string(statusBefore) != string(statusAfter) || string(resultsBefore) != string(resultsAfter) {
			t.Fatal("unrelated status or results changed")
		}
	}
	if err = scoped.UpdateEvaluationJobOCI(job.Resource.ID, nil, nil); err == nil {
		t.Fatal("nil status accepted")
	}
	if err = scoped.UpdateEvaluationJobOCI(job.Resource.ID, status(api.OCIProcessingCompleted), &api.OCIArtifactReference{}); err == nil {
		t.Fatal("empty artifact accepted")
	}
}

func TestUpdateEvaluationJobOCIRejectsIneligibleJob(t *testing.T) {
	store, err := getTestStorage(t, "sqlite", getDBName())
	if err != nil {
		t.Fatal(err)
	}
	job := makeGitJob(common.GUID(), "main")
	if err = store.CreateEvaluationJob(job); err != nil {
		t.Fatal(err)
	}
	err = store.WithTenant(job.Resource.Tenant).UpdateEvaluationJobOCI(job.Resource.ID, &api.OCIProcessingStatus{State: api.OCIProcessingPending, Message: &api.MessageInfo{Message: "Pending", MessageCode: "OCI_PENDING"}}, nil)
	if err == nil {
		t.Fatal("reported OCI processing for an ineligible evaluation")
	}
}
