package api

import (
	"reflect"
	"strings"
	"testing"
)

func mergeTestArtifact(name, hex string) *OCIArtifactReference {
	digest := "sha256:" + strings.Repeat(hex, 64)
	return &OCIArtifactReference{OCIDigest: digest, OCIReference: "registry.example/" + name + "@" + digest}
}

func TestUpdateOCIResultsInitializesMissingResults(t *testing.T) {
	for _, missingResults := range []bool{true, false} {
		for _, outputs := range []string{"none", "card", "bundle", "both"} {
			t.Run(fmtMergeCase(missingResults, outputs), func(t *testing.T) {
				var results *EvaluationJobResults
				if !missingResults {
					results = &EvaluationJobResults{MLFlowExperimentURL: "https://mlflow.example/experiments/1"}
				}
				var card, bundle *OCIArtifactReference
				if outputs == "card" || outputs == "both" {
					card = mergeTestArtifact("card", "a")
				}
				if outputs == "bundle" || outputs == "both" {
					bundle = mergeTestArtifact("bundle", "b")
				}
				status := &OCIProcessingStatus{State: OCIProcessingRunning}
				got := UpdateOCIResults(results, status, card, bundle)
				if got == nil || got.OCI == nil || got.OCI.Status != status || got.OCI.EvaluationCard != card || got.OCI.EvaluationBundle != bundle || got.OCI.Signing != nil {
					t.Fatalf("incorrect initialized results: %#v", got)
				}
				if results != nil && (got != results || got.MLFlowExperimentURL != "https://mlflow.example/experiments/1") {
					t.Fatal("existing results were replaced")
				}
			})
		}
	}
}

func fmtMergeCase(missingResults bool, outputs string) string {
	if missingResults {
		return "missing results/" + outputs
	}
	return "missing OCI/" + outputs
}

func TestUpdateOCIResultsPreservesOutputsAcrossWorkflowStates(t *testing.T) {
	for _, state := range []OCIProcessingState{OCIProcessingRunning, OCIProcessingCompleted, OCIProcessingFailed} {
		for _, outputs := range []string{"none", "card", "bundle", "both"} {
			t.Run(string(state)+"/"+outputs, func(t *testing.T) {
				results := &EvaluationJobResults{
					OCI:        &EvaluationOCIResults{EvaluationCard: mergeTestArtifact("old-card", "a"), EvaluationBundle: mergeTestArtifact("old-bundle", "b"), Signing: &OCISigningResult{Type: "key-pair"}},
					Benchmarks: []BenchmarkResult{{ID: "bench", Metrics: map[string]any{"accuracy": 0.7}, Artifacts: map[string]any{"samples": "samples.json"}, MLFlowRunID: "run-1"}},
					Test:       &EvaluationTest{Score: 0.7, Pass: true}, MLFlowExperimentURL: "https://mlflow.example/experiments/1",
				}
				want := *results
				wantOCI := *results.OCI
				want.OCI = &wantOCI
				originalOCI := results.OCI
				var card, bundle *OCIArtifactReference
				if outputs == "card" || outputs == "both" {
					card = mergeTestArtifact("new-card", "c")
					wantOCI.EvaluationCard = card
				}
				if outputs == "bundle" || outputs == "both" {
					bundle = mergeTestArtifact("new-bundle", "d")
					wantOCI.EvaluationBundle = bundle
				}
				status := &OCIProcessingStatus{State: state, Message: &MessageInfo{Message: "Processing", MessageCode: "oci_test"}}
				wantOCI.Status = status
				got := UpdateOCIResults(results, status, card, bundle)
				if got != results || got.OCI != originalOCI || !reflect.DeepEqual(got, &want) {
					t.Fatalf("merge changed unrelated results or lost outputs: got %#v, want %#v", got, &want)
				}
			})
		}
	}
}

// TestEligibleForOCIProcessing covers supported outcomes and rejects missing export or status prerequisites.
func TestEligibleForOCIProcessing(t *testing.T) {
	for _, state := range []OverallState{"", OverallStatePending, OverallStateRunning, OverallStateCompleted, OverallStateFailed, OverallStateCancelled, OverallStatePartiallyFailed} {
		t.Run(string(state), func(t *testing.T) {
			job := &EvaluationJobResource{
				EvaluationJobConfig: EvaluationJobConfig{Exports: &EvaluationExports{OCI: &EvaluationExportsOCI{}}},
				Status:              &EvaluationJobStatus{EvaluationJobState: EvaluationJobState{State: state}},
			}
			want := state == OverallStateCompleted || state == OverallStatePartiallyFailed
			if got := EligibleForOCIProcessing(job); got != want {
				t.Fatalf("eligibility = %v, want %v", got, want)
			}
		})
	}
	for name, job := range map[string]*EvaluationJobResource{
		"nil job": nil,
		"missing status": {EvaluationJobConfig: EvaluationJobConfig{
			Exports: &EvaluationExports{OCI: &EvaluationExportsOCI{}},
		}},
		"missing exports": {Status: &EvaluationJobStatus{EvaluationJobState: EvaluationJobState{State: OverallStateCompleted}}},
		"missing OCI": {
			EvaluationJobConfig: EvaluationJobConfig{Exports: &EvaluationExports{}},
			Status:              &EvaluationJobStatus{EvaluationJobState: EvaluationJobState{State: OverallStateCompleted}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if EligibleForOCIProcessing(job) {
				t.Fatal("job with missing prerequisites is eligible")
			}
		})
	}
}
