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
			ErrorMessage:   &MessageInfo{Message: "err", MessageCode: "E", MessageOrigin: MessageOriginAdapter},
			WarningMessage: &MessageInfo{Message: "warn", MessageCode: "W", MessageOrigin: MessageOriginSDK},
		}
		event.StampRuntimeMessageOrigins()

		if event.ErrorMessage.MessageOrigin != MessageOriginAdapter {
			t.Fatalf("expected adapter error origin to be preserved, got %q", event.ErrorMessage.MessageOrigin)
		}
		if event.WarningMessage.MessageOrigin != MessageOriginSDK {
			t.Fatalf("expected sdk warning origin to be preserved, got %q", event.WarningMessage.MessageOrigin)
		}
	})
}

// TestBenchmarkStatusEventNilStampRuntimeMessageOrigins verifies StampRuntimeMessageOrigins
// is a no-op on a nil receiver and does not panic.
func TestBenchmarkStatusEventNilStampRuntimeMessageOrigins(t *testing.T) {
	var event *BenchmarkStatusEvent
	event.StampRuntimeMessageOrigins()
}

func TestRewriteSidecarURLsInMessage(t *testing.T) {
	const sidecar = "http://localhost:8080"
	targets := SidecarURLTargets{
		EvalHub:       "https://evalhub.demo.svc.cluster.local:8443",
		MLFlow:        "https://mlflow.example.com",
		OCI:           "https://quay.io",
		OCIRepository: "org/repo",
		Model:         "https://api.openai.com/v1",
	}

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "model path rewrites to model host",
			in:   "Model endpoint returned HTTP 404: 404 Client Error: Not Found for url: http://localhost:8080/v1/completions",
			want: "Model endpoint returned HTTP 404: 404 Client Error: Not Found for url: https://api.openai.com/v1/completions",
		},
		{
			name: "mlflow path rewrites to mlflow host",
			in:   "MLflow endpoint returned HTTP 502: Bad Gateway for url: http://localhost:8080/api/2.0/mlflow/runs/create",
			want: "MLflow endpoint returned HTTP 502: Bad Gateway for url: https://mlflow.example.com/api/2.0/mlflow/runs/create",
		},
		{
			name: "eval-hub path rewrites to eval-hub host",
			in:   "callback failed for url: http://localhost:8080/api/v1/evaluations/jobs/j1/events",
			want: "callback failed for url: https://evalhub.demo.svc.cluster.local:8443/api/v1/evaluations/jobs/j1/events",
		},
		{
			name: "oci path rewrites to oci host",
			in:   "OCI push failed for url: http://localhost:8080/v2/org/repo/blobs/uploads/",
			want: "OCI push failed for url: https://quay.io/v2/org/repo/blobs/uploads/",
		},
		{
			name: "preserves query string",
			in:   "error for url: http://localhost:8080/v1/completions?stream=true",
			want: "error for url: https://api.openai.com/v1/completions?stream=true",
		},
		{
			name: "unrelated message unchanged",
			in:   "Connection failed: timeout talking to sidecar",
			want: "Connection failed: timeout talking to sidecar",
		},
		{
			name: "prefix-sharing host unchanged",
			in:   "error for url: http://localhost:8080.evil.com/v1/completions",
			want: "error for url: http://localhost:8080.evil.com/v1/completions",
		},
		{
			name: "prefix-sharing port unchanged",
			in:   "error for url: http://localhost:80809/v1/completions",
			want: "error for url: http://localhost:80809/v1/completions",
		},
		{
			name: "empty",
			in:   "",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RewriteSidecarURLsInMessage(tt.in, sidecar, targets)
			if got != tt.want {
				t.Fatalf("RewriteSidecarURLsInMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRewriteSidecarURLsInMessageFallbackStripHost(t *testing.T) {
	const sidecar = "http://localhost:8080"
	empty := SidecarURLTargets{}

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "model path strips host when model target missing",
			in:   "Model endpoint returned HTTP 404: Not Found for url: http://localhost:8080/v1/completions",
			want: "Model endpoint returned HTTP 404: Not Found for url: /v1/completions",
		},
		{
			name: "mlflow path strips host when mlflow target missing",
			in:   "error for url: http://localhost:8080/api/2.0/mlflow/runs/create",
			want: "error for url: /api/2.0/mlflow/runs/create",
		},
		{
			name: "bare sidecar base becomes slash",
			in:   "failed contacting http://localhost:8080",
			want: "failed contacting /",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RewriteSidecarURLsInMessage(tt.in, sidecar, empty)
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBenchmarkStatusEventRewriteSidecarURLsInMessages(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	targets := SidecarURLTargets{Model: "https://model.example"}
	event := &BenchmarkStatusEvent{
		ErrorMessage: &MessageInfo{
			Message:     "err for url: http://localhost:8080/v1/chat",
			MessageCode: "E",
		},
		WarningMessage: &MessageInfo{
			Message:     "warn for url: http://localhost:8080/v1/chat",
			MessageCode: "W",
		},
	}
	event.RewriteSidecarURLsInMessages("http://localhost:8080", targets, logger)

	want := "err for url: https://model.example/v1/chat"
	if event.ErrorMessage.Message != want {
		t.Fatalf("error message = %q, want %q", event.ErrorMessage.Message, want)
	}
	wantWarn := "warn for url: https://model.example/v1/chat"
	if event.WarningMessage.Message != wantWarn {
		t.Fatalf("warning message = %q, want %q", event.WarningMessage.Message, wantWarn)
	}

	t.Run("nil receiver and nil messages are no-ops", func(t *testing.T) {
		var nilEvent *BenchmarkStatusEvent
		nilEvent.RewriteSidecarURLsInMessages("http://localhost:8080", targets, logger)

		empty := &BenchmarkStatusEvent{}
		empty.RewriteSidecarURLsInMessages("http://localhost:8080", targets, logger)
	})

	t.Run("logs original message before rewriting", func(t *testing.T) {
		var buf bytes.Buffer
		log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
		originalMessage := "Model endpoint returned HTTP 500: boom for url: http://localhost:8080/v1/x"
		ev := &BenchmarkStatusEvent{
			ErrorMessage: &MessageInfo{Message: originalMessage, MessageCode: "E"},
		}
		ev.RewriteSidecarURLsInMessages("http://localhost:8080", targets, log)
		if !strings.Contains(buf.String(), originalMessage) {
			t.Fatalf("log = %q, want original message", buf.String())
		}
		if strings.Contains(ev.ErrorMessage.Message, "localhost:8080") {
			t.Fatalf("persisted message still has sidecar host: %q", ev.ErrorMessage.Message)
		}
	})
}
