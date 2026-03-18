package handlers

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eval-hub/eval-hub/internal/config"
)

func TestNew(t *testing.T) {
	logger := slog.Default()

	t.Run("returns error when eval_hub.base_url is not set", func(t *testing.T) {
		cfg := &config.Config{
			Sidecar: &config.SidecarConfig{EvalHub: &config.EvalHubClientConfig{}},
		}
		_, err := New(cfg, logger)
		if err == nil {
			t.Fatal("expected error when eval_hub.base_url is not set")
		}
		if err.Error() != "eval_hub.base_url is not set in sidecar config" {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("returns Handlers when eval_hub.base_url and mlflow set", func(t *testing.T) {
		cfg := &config.Config{
			Sidecar: &config.SidecarConfig{
				EvalHub: &config.EvalHubClientConfig{BaseURL: "http://localhost:8080"},
			},
			MLFlow: &config.MLFlowConfig{TrackingURI: "http://localhost:5000"},
		}
		h, err := New(cfg, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if h == nil {
			t.Fatal("expected non-nil Handlers")
		}
		if h.evalHubProxy == nil {
			t.Error("expected non-nil evalHubProxy")
		}
		if h.mlflowProxy == nil {
			t.Error("expected non-nil mlflowProxy")
		}
	})
}

func TestHandlers_HandleProxyCall(t *testing.T) {
	logger := slog.Default()
	cfg := &config.Config{
		Sidecar: &config.SidecarConfig{
			EvalHub: &config.EvalHubClientConfig{BaseURL: "http://localhost:8080"},
		},
		MLFlow: &config.MLFlowConfig{TrackingURI: "http://localhost:5000"},
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
		h2 := &Handlers{
			logger: logger,
			serviceConfig: &config.Config{
				Sidecar: &config.SidecarConfig{EvalHub: nil},
				MLFlow:  &config.MLFlowConfig{TrackingURI: "http://localhost:5000"},
			},
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
		if body := rw.Body.String(); body == "unknown proxy call: /api/v1/evaluations/jobs/123\n" {
			t.Errorf("eval-hub path should match prefix; got unknown proxy call")
		}
	})

	t.Run("mlflow path with nil MLFlow returns 400", func(t *testing.T) {
		cfgNoMLFlow := &config.Config{
			Sidecar: &config.SidecarConfig{
				EvalHub: &config.EvalHubClientConfig{BaseURL: "http://localhost:8080"},
			},
		}
		hNoMLFlow, err := New(cfgNoMLFlow, logger)
		if err != nil {
			t.Fatalf("New() error: %v", err)
		}
		req := httptest.NewRequest(http.MethodGet, "/api/2.0/mlflow/experiments/list", nil)
		rw := httptest.NewRecorder()
		hNoMLFlow.HandleProxyCall(rw, req)
		if rw.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400 (mlflow proxy not configured)", rw.Code)
		}
	})
}
