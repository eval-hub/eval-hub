package api

import "testing"

func TestIsResultsExportEligible(t *testing.T) {
	for _, tc := range []struct {
		name string
		job  *EvaluationJobResource
		want bool
	}{
		{name: "nil job"},
		{name: "missing status", job: &EvaluationJobResource{}},
		{name: "completed without exports", job: &EvaluationJobResource{Status: &EvaluationJobStatus{EvaluationJobState: EvaluationJobState{State: OverallStateCompleted}}}, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsResultsExportEligible(tc.job); got != tc.want {
				t.Fatalf("IsResultsExportEligible() = %v, want %v", got, tc.want)
			}
		})
	}
	for _, tc := range []struct {
		state OverallState
		want  bool
	}{
		{OverallStateCompleted, true},
		{OverallStatePartiallyFailed, true},
		{OverallStateFailed, false},
		{OverallStateCancelled, false},
		{OverallStatePending, false},
		{OverallStateRunning, false},
		{"", false},
		{"unknown", false},
	} {
		t.Run(string(tc.state), func(t *testing.T) {
			job := &EvaluationJobResource{
				Status: &EvaluationJobStatus{EvaluationJobState: EvaluationJobState{State: tc.state}},
			}
			if got := IsResultsExportEligible(job); got != tc.want {
				t.Fatalf("IsResultsExportEligible() = %v, want %v", got, tc.want)
			}
		})
	}
}
