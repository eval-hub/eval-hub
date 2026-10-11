package api

import (
	"reflect"
	"testing"
)

func TestMergeOCIResultsInitializesMissingResults(t *testing.T) {
	for _, tc := range []struct {
		name    string
		results *EvaluationJobResults
	}{
		{name: "missing results"},
		{name: "missing OCI", results: &EvaluationJobResults{MLFlowExperimentURL: "https://mlflow.example/experiments/1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status := &OCIProcessingStatus{State: OCIProcessingRunning}
			got := MergeOCIResults(tc.results, status, nil)
			if got == nil || got.OCI == nil || got.OCI.Status != status {
				t.Fatalf("OCI status was not initialized: %#v", got)
			}
			if got.OCI.EvaluationCard != nil {
				t.Fatal("status-only update created a card reference")
			}
			if tc.results != nil && (got != tc.results || got.MLFlowExperimentURL != "https://mlflow.example/experiments/1") {
				t.Fatal("existing results were replaced")
			}
		})
	}
}

func TestMergeOCIResultsPreservesOutputsAcrossWorkflowStates(t *testing.T) {
	oldCard := &OCIArtifactReference{OCIDigest: "sha256:old", OCIReference: "registry.example/card@sha256:old"}
	newCard := &OCIArtifactReference{OCIDigest: "sha256:new", OCIReference: "registry.example/card@sha256:new"}
	results := &EvaluationJobResults{
		OCI: &EvaluationOCIResults{
			EvaluationCard:   oldCard,
			EvaluationBundle: &OCIArtifactReference{OCIDigest: "sha256:bundle", OCIReference: "registry.example/bundle@sha256:bundle"},
			Signing:          &OCISigningResult{Type: "key-pair"},
		},
		Benchmarks:          []BenchmarkResult{{ID: "bench", Metrics: map[string]any{"accuracy": 0.7}}},
		Test:                &EvaluationTest{Score: 0.7, Pass: true},
		MLFlowExperimentURL: "https://mlflow.example/experiments/1",
	}
	want := *results
	wantOCI := *results.OCI
	want.OCI = &wantOCI
	for _, tc := range []struct {
		state OCIProcessingState
		card  *OCIArtifactReference
	}{
		{state: OCIProcessingRunning},
		{state: OCIProcessingCompleted, card: newCard},
		{state: OCIProcessingFailed},
	} {
		t.Run(string(tc.state), func(t *testing.T) {
			status := &OCIProcessingStatus{State: tc.state, Message: &MessageInfo{Message: "Processing", MessageCode: "oci_test"}}
			wantOCI.Status = status
			if tc.card != nil {
				wantOCI.EvaluationCard = tc.card
			}
			got := MergeOCIResults(results, status, tc.card)
			if got != results || got.OCI != results.OCI || !reflect.DeepEqual(got, &want) {
				t.Fatalf("merge changed unrelated results or lost outputs: got %#v, want %#v", got, &want)
			}
		})
	}
}
