package handlers

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/eval-hub/eval-hub/internal/config"
)

func TestNew(t *testing.T) {
	logger := slog.Default()

	t.Run("returns error when EVALHUB_URL is not set", func(t *testing.T) {
		os.Unsetenv("EVALHUB_URL")
		cfg := &config.Config{
			Sidecar: &config.SidecarConfig{EvalHub: &config.EvalHubClientConfig{}},
		}
		_, err := New(cfg, logger)
		if err == nil {
			t.Fatal("expected error when EVALHUB_URL is not set")
		}
		if err.Error() != "EVALHUB_URL environment variable is not set" {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("returns Handlers when EVALHUB_URL set and config valid", func(t *testing.T) {
		t.Setenv("EVALHUB_URL", "http://localhost:8080")
		cfg := &config.Config{
			Sidecar: &config.SidecarConfig{EvalHub: &config.EvalHubClientConfig{}},
		}
		h, err := New(cfg, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if h == nil {
			t.Fatal("expected non-nil Handlers")
		}
		if h.evalHubBaseURL != "http://localhost:8080" {
			t.Errorf("evalHubBaseURL = %q, want http://localhost:8080", h.evalHubBaseURL)
		}
	})
}

func TestHandlers_HandleProxyCall(t *testing.T) {
	t.Setenv("EVALHUB_URL", "http://localhost:8080")
	logger := slog.Default()
	cfg := &config.Config{
		Sidecar: &config.SidecarConfig{EvalHub: &config.EvalHubClientConfig{}},
	}
	h, err := New(cfg, logger)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	t.Run("unknown path returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/unknown", nil)
		rw := httptest.NewRecorder()
		h.HandleProxyCall(rw, req)
		if rw.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rw.Code)
		}
		if body := rw.Body.String(); body != "unknown proxy call: /unknown\n" {
			t.Errorf("body = %q", body)
		}
	})

	t.Run("eval-hub path with nil EvalHub returns 400", func(t *testing.T) {
		cfgNoEvalHub := &config.Config{Sidecar: &config.SidecarConfig{}}
		h2, err := New(cfgNoEvalHub, logger)
		if err != nil {
			t.Fatalf("New() error: %v", err)
		}
		req := httptest.NewRequest(http.MethodGet, "/api/v1/evaluations/jobs", nil)
		rw := httptest.NewRecorder()
		h2.HandleProxyCall(rw, req)
		if rw.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rw.Code)
		}
	})

	t.Run("eval-hub path with prefix matches", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/evaluations/jobs/123", nil)
		rw := httptest.NewRecorder()
		h.HandleProxyCall(rw, req)
		// Should not be "unknown proxy call" (that would mean path didn't match)
		if body := rw.Body.String(); body == "unknown proxy call: /api/v1/evaluations/jobs/123\n" {
			t.Errorf("eval-hub path should match prefix; got unknown proxy call")
		}
	})

	t.Run("mlflow path with nil MLFlow returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/2.0/mlflow/experiments/list", nil)
		rw := httptest.NewRecorder()
		h.HandleProxyCall(rw, req)
		// Our config has no MLFlow, so we get "mlflow proxy is not configured"
		if rw.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rw.Code)
		}
	})
}
