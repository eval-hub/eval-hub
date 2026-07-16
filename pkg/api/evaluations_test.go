package api

import (
	"bytes"
	"io"
	"log/slog"
	"strings"
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

// TestBenchmarkStatusEventNilStampRuntimeMessageOrigins verifies StampRuntimeMessageOrigins
// is a no-op on a nil receiver and does not panic.
func TestBenchmarkStatusEventNilStampRuntimeMessageOrigins(t *testing.T) {
	var event *BenchmarkStatusEvent
	event.StampRuntimeMessageOrigins()
}

func TestTruncateEndpointHTTPErrorDetail(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "model endpoint with requests detail",
			in:   "Model endpoint returned HTTP 404: 404 Client Error: Not Found for url: http://localhost:8080/v1/completions",
			want: "Model endpoint returned HTTP 404",
		},
		{
			name: "mlflow endpoint with 502 detail",
			in:   "MLflow endpoint returned HTTP 502: 502 Server Error: Bad Gateway for url: http://localhost:8080/api/2.0/mlflow/runs/create",
			want: "MLflow endpoint returned HTTP 502",
		},
		{
			name: "no detail after status code",
			in:   "Model endpoint returned HTTP 404",
			want: "Model endpoint returned HTTP 404",
		},
		{
			name: "unrelated message with colon",
			in:   "Connection failed: timeout talking to sidecar",
			want: "Connection failed: timeout talking to sidecar",
		},
		{
			name: "empty",
			in:   "",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &BenchmarkStatusEvent{
				ErrorMessage: &MessageInfo{Message: tt.in, MessageCode: "E"},
			}
			event.TruncateEndpointHTTPErrorDetail(logger)
			if event.ErrorMessage.Message != tt.want {
				t.Fatalf("TruncateEndpointHTTPErrorDetail() message = %q, want %q", event.ErrorMessage.Message, tt.want)
			}
		})
	}

	t.Run("nil receiver and nil error message are no-ops", func(t *testing.T) {
		var event *BenchmarkStatusEvent
		event.TruncateEndpointHTTPErrorDetail(logger)

		empty := &BenchmarkStatusEvent{}
		empty.TruncateEndpointHTTPErrorDetail(logger)
	})

	t.Run("logs full message before truncating", func(t *testing.T) {
		var buf bytes.Buffer
		log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
		full := "Model endpoint returned HTTP 500: upstream boom"
		event := &BenchmarkStatusEvent{
			ErrorMessage: &MessageInfo{Message: full, MessageCode: "E"},
		}
		event.TruncateEndpointHTTPErrorDetail(log)
		if event.ErrorMessage.Message != "Model endpoint returned HTTP 500" {
			t.Fatalf("message = %q", event.ErrorMessage.Message)
		}
		if !strings.Contains(buf.String(), full) {
			t.Fatalf("log = %q, want full original message", buf.String())
		}
	})
}
