package api

import "testing"

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
