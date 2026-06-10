package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/constants"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/evalhubclient"
)

func TestScopedEvalHubClientPropagatesRequestID(t *testing.T) {
	t.Parallel()

	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get(evalhubclient.TransactionIDHeader)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(api.HealthResponse{Status: "ok"})
	}))
	t.Cleanup(srv.Close)

	base := evalhubclient.NewClient(srv.URL)
	ctx := context.WithValue(context.Background(), requestContextKey{}, "mcp-req-42")
	ctx = context.WithValue(ctx, requestLoggerContextKey{}, slog.New(slog.DiscardHandler).With(constants.LOG_REQUEST_ID, "mcp-req-42"))

	client := scopedEvalHubClient(ctx, base, slog.New(slog.DiscardHandler))
	if _, err := client.GetHealth(); err != nil {
		t.Fatalf("GetHealth: %v", err)
	}
	if gotHeader != "mcp-req-42" {
		t.Fatalf("header = %q, want %q", gotHeader, "mcp-req-42")
	}
}

func TestScopedEvalHubClientUsesRequestLogger(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	}))
	t.Cleanup(srv.Close)

	var buf bytes.Buffer
	baseLogger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ctx := context.WithValue(context.Background(), requestContextKey{}, "req-log")
	ctx = context.WithValue(ctx, requestLoggerContextKey{}, baseLogger.With(constants.LOG_REQUEST_ID, "req-log"))

	client := scopedEvalHubClient(ctx, evalhubclient.NewClient(srv.URL), baseLogger)
	if _, err := client.GetHealth(); err != nil {
		t.Fatalf("GetHealth: %v", err)
	}
	if !strings.Contains(buf.String(), "request_id=req-log") {
		t.Fatalf("log = %q, want request_id field", buf.String())
	}
}
