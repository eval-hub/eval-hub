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

func TestWithMessageOrigin(t *testing.T) {
	if got := WithMessageOrigin(nil, MessageOriginServer); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}

	msg := &MessageInfo{Message: "m", MessageCode: "c"}
	if got := WithMessageOrigin(msg, MessageOriginRuntime); got.MessageOrigin != MessageOriginRuntime {
		t.Fatalf("expected runtime origin, got %q", got.MessageOrigin)
	}
}

func TestBenchmarkStatusEventStampRuntimeMessageOrigins(t *testing.T) {
	event := &BenchmarkStatusEvent{
		ErrorMessage:   &MessageInfo{Message: "err", MessageCode: "E"},
		WarningMessage: &MessageInfo{Message: "warn", MessageCode: "W"},
	}
	event.StampRuntimeMessageOrigins()

	if event.ErrorMessage.MessageOrigin != MessageOriginRuntime {
		t.Fatalf("expected runtime error origin, got %q", event.ErrorMessage.MessageOrigin)
	}
	if event.WarningMessage.MessageOrigin != MessageOriginRuntime {
		t.Fatalf("expected runtime warning origin, got %q", event.WarningMessage.MessageOrigin)
	}
}
