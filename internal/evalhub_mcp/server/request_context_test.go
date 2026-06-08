package server

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/evalhub_mcp/config"
	"github.com/google/uuid"
)

func TestWithRequestContextUsesExistingHeader(t *testing.T) {
	t.Parallel()

	var gotID string
	inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotID = RequestIDFromContext(r.Context())
	})
	handler := withRequestContext(discardLogger, inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(TRANSACTION_ID_HEADER, "  req-123  ")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if gotID != "req-123" {
		t.Fatalf("RequestIDFromContext() = %q, want %q", gotID, "req-123")
	}
	if rec.Header().Get(TRANSACTION_ID_HEADER) != "req-123" {
		t.Fatalf("response header = %q, want %q", rec.Header().Get(TRANSACTION_ID_HEADER), "req-123")
	}
	if req.Header.Get(TRANSACTION_ID_HEADER) != "  req-123  " {
		t.Fatalf("request header was mutated: %q", req.Header.Get(TRANSACTION_ID_HEADER))
	}
}

func TestWithRequestContextGeneratesRequestID(t *testing.T) {
	t.Parallel()

	var gotID string
	inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotID = RequestIDFromContext(r.Context())
	})
	handler := withRequestContext(discardLogger, inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if gotID == "" {
		t.Fatal("expected generated request ID")
	}
	if _, err := uuid.Parse(gotID); err != nil {
		t.Fatalf("generated request ID %q is not a UUID: %v", gotID, err)
	}
	if rec.Header().Get(TRANSACTION_ID_HEADER) != gotID {
		t.Fatalf("response header = %q, want %q", rec.Header().Get(TRANSACTION_ID_HEADER), gotID)
	}
}

func TestRequestLoggerFromContext(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	base := slog.New(slog.NewTextHandler(&buf, nil))

	inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		requestLogger(r.Context(), base).Info("test message")
	})
	handler := withRequestContext(base, inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(TRANSACTION_ID_HEADER, "req-log")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !strings.Contains(buf.String(), "request_id=req-log") {
		t.Fatalf("log = %q, want request_id field", buf.String())
	}
}

func TestWrapRequestPropagatesRequestID(t *testing.T) {
	t.Parallel()

	var gotID string
	inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotID = RequestIDFromContext(r.Context())
	})
	handler := wrapRequest(&config.Config{AuthType: config.AuthTypeNone}, discardLogger, inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(TRANSACTION_ID_HEADER, "wrapped-req")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if gotID != "wrapped-req" {
		t.Fatalf("RequestIDFromContext() = %q, want %q", gotID, "wrapped-req")
	}
}
