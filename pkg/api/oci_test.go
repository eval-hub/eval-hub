package api

import (
	"reflect"
	"testing"
)

func TestIsOCIExportEnabled(t *testing.T) {
	for _, tc := range []struct {
		name string
		job  *EvaluationJobResource
		want bool
	}{
		{name: "nil job"},
		{name: "missing exports", job: &EvaluationJobResource{}},
		{name: "missing OCI exports", job: &EvaluationJobResource{EvaluationJobConfig: EvaluationJobConfig{Exports: &EvaluationExports{}}}},
		{name: "enabled without status", job: &EvaluationJobResource{EvaluationJobConfig: EvaluationJobConfig{Exports: &EvaluationExports{OCI: &EvaluationExportsOCI{}}}}, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsOCIExportEnabled(tc.job); got != tc.want {
				t.Fatalf("IsOCIExportEnabled() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIsOCIProcessingTerminalState(t *testing.T) {
	for _, tc := range []struct {
		state OCIProcessingState
		want  bool
	}{
		{OCIProcessingRunning, false},
		{OCIProcessingCompleted, true},
		{OCIProcessingFailed, true},
		{"", false},
		{"unknown", false},
	} {
		t.Run(string(tc.state), func(t *testing.T) {
			if got := IsOCIProcessingTerminalState(tc.state); got != tc.want {
				t.Fatalf("IsOCIProcessingTerminalState(%q) = %v, want %v", tc.state, got, tc.want)
			}
		})
	}
}

func TestUpdateOCIResultsInitializesMissingResults(t *testing.T) {
	for _, tc := range []struct {
		name    string
		results *EvaluationJobResults
	}{
		{name: "missing results"},
		{name: "missing OCI", results: &EvaluationJobResults{MLFlowExperimentURL: "https://mlflow.example/experiments/1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status := &OCIProcessingStatus{State: OCIProcessingRunning}
			got := UpdateOCIResults(tc.results, status, nil)
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

func TestUpdateOCIResultsPreservesNilUpdates(t *testing.T) {
	status := &OCIProcessingStatus{State: OCIProcessingRunning}
	card := &OCIArtifactReference{OCIDigest: "sha256:card", OCIReference: "registry.example/card@sha256:card"}
	for _, tc := range []struct {
		name    string
		results *EvaluationJobResults
	}{
		{name: "nil results"},
		{name: "missing OCI", results: &EvaluationJobResults{MLFlowExperimentURL: "https://mlflow.example/experiments/1"}},
		{name: "existing OCI", results: &EvaluationJobResults{OCI: &EvaluationOCIResults{Status: status, EvaluationCard: card}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := UpdateOCIResults(tc.results, nil, nil)
			if got != tc.results {
				t.Fatal("nil update replaced results")
			}
			if tc.results != nil && tc.results.OCI == nil && got.OCI != nil {
				t.Fatal("nil update allocated OCI results")
			}
		})
	}

	results := &EvaluationJobResults{OCI: &EvaluationOCIResults{Status: status}}
	got := UpdateOCIResults(results, nil, card)
	if got != results || got.OCI.Status != status || got.OCI.EvaluationCard != card {
		t.Fatal("card-only update did not preserve the existing status")
	}
}

func TestUpdateOCIResultsPreservesOutputsAcrossWorkflowStates(t *testing.T) {
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
			got := UpdateOCIResults(results, status, tc.card)
			if got != results || got.OCI != results.OCI || !reflect.DeepEqual(got, &want) {
				t.Fatalf("merge changed unrelated results or lost outputs: got %#v, want %#v", got, &want)
			}
		})
	}
}
