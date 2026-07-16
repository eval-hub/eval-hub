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

func TestDefaultMessageOrigin(t *testing.T) {
	if got := DefaultMessageOrigin(nil, MessageOriginServer); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}

	unset := &MessageInfo{Message: "m", MessageCode: "c"}
	if got := DefaultMessageOrigin(unset, MessageOriginRuntime); got.MessageOrigin != MessageOriginRuntime {
		t.Fatalf("expected runtime origin, got %q", got.MessageOrigin)
	}

	set := &MessageInfo{Message: "m", MessageCode: "c", MessageOrigin: MessageOriginServer}
	if got := DefaultMessageOrigin(set, MessageOriginRuntime); got.MessageOrigin != MessageOriginServer {
		t.Fatalf("expected existing server origin to be preserved, got %q", got.MessageOrigin)
	}
}

func TestBenchmarkStatusEventStampRuntimeMessageOrigins(t *testing.T) {
	t.Run("defaults missing origins to runtime", func(t *testing.T) {
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
	})

	t.Run("preserves origins already set on the event", func(t *testing.T) {
		event := &BenchmarkStatusEvent{
			ErrorMessage:   &MessageInfo{Message: "err", MessageCode: "E", MessageOrigin: MessageOriginServer},
			WarningMessage: &MessageInfo{Message: "warn", MessageCode: "W", MessageOrigin: MessageOriginServer},
		}
		event.StampRuntimeMessageOrigins()

		if event.ErrorMessage.MessageOrigin != MessageOriginServer {
			t.Fatalf("expected server error origin to be preserved, got %q", event.ErrorMessage.MessageOrigin)
		}
		if event.WarningMessage.MessageOrigin != MessageOriginServer {
			t.Fatalf("expected server warning origin to be preserved, got %q", event.WarningMessage.MessageOrigin)
		}
	})
}

// TestBenchmarkStatusEventNilStampRuntimeMessageOrigins verifies StampRuntimeMessageOrigins
// is a no-op on a nil receiver and does not panic.
func TestBenchmarkStatusEventNilStampRuntimeMessageOrigins(t *testing.T) {
	var event *BenchmarkStatusEvent
	event.StampRuntimeMessageOrigins()
}
