package api

import (
	"encoding/json"
	"testing"
)

func TestIsBenchmarkTerminalState(t *testing.T) {
	tests := []struct {
		state    State
		expected bool
	}{
		{StateCompleted, true},
		{StateFailed, true},
		{StateCancelled, true},
		{StatePending, false},
		{StateRunning, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			got := IsBenchmarkTerminalState(tt.state)
			if got != tt.expected {
				t.Errorf("IsBenchmarkTerminalState(%q) = %v, want %v", tt.state, got, tt.expected)
			}
		})
	}
}

func TestPassCriteria_MarshalJSON_NilThreshold(t *testing.T) {
	t.Parallel()
	pc := PassCriteria{Threshold: nil}
	data, err := json.Marshal(pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != "null" {
		t.Errorf("expected null, got %s", string(data))
	}
}

func TestPassCriteria_MarshalJSON_WithThreshold(t *testing.T) {
	t.Parallel()
	v := float32(0.75)
	pc := PassCriteria{Threshold: &v}
	data, err := json.Marshal(pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("output is not valid JSON object: %v", err)
	}
	if parsed["threshold"] != float64(0.75) {
		t.Errorf("expected threshold 0.75, got %v", parsed["threshold"])
	}
}

func TestPassCriteria_MarshalJSON_ZeroThreshold(t *testing.T) {
	t.Parallel()
	v := float32(0)
	pc := PassCriteria{Threshold: &v}
	data, err := json.Marshal(pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("output is not valid JSON object: %v", err)
	}
	if parsed["threshold"] != float64(0) {
		t.Errorf("expected threshold 0, got %v", parsed["threshold"])
	}
}

func TestBenchmarkResource_EmptyPassCriteria_OmittedFromJSON(t *testing.T) {
	t.Parallel()
	br := BenchmarkResource{
		ID:           "test",
		Name:         "test",
		Category:     "test",
		PassCriteria: &PassCriteria{Threshold: nil},
	}
	data, err := json.Marshal(br)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if pc, ok := parsed["pass_criteria"]; ok && pc != nil {
		t.Errorf("expected pass_criteria to be null or absent, got %v", pc)
	}
}
