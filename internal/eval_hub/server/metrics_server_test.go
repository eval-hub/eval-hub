package server_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_hub/server"
	"github.com/eval-hub/eval-hub/internal/logging"
)

func TestMetricsServerDefaults(t *testing.T) {
	t.Parallel()
	t.Run("uses default port when not configured", func(t *testing.T) {
		t.Parallel()
		logger, _, _ := logging.NewLogger()
		promConfig := &config.PrometheusConfig{Enabled: true}
		srv := server.NewMetricsServer(logger, promConfig)
		if srv.GetPort() != config.DefaultMetricsPort {
			t.Errorf("expected default port %d, got %d", config.DefaultMetricsPort, srv.GetPort())
		}
	})

	t.Run("uses configured port", func(t *testing.T) {
		t.Parallel()
		logger, _, _ := logging.NewLogger()
		promConfig := &config.PrometheusConfig{Enabled: true, Port: 9191}
		srv := server.NewMetricsServer(logger, promConfig)
		if srv.GetPort() != 9191 {
			t.Errorf("expected port 9191, got %d", srv.GetPort())
		}
	})
}

func TestMetricsServerHandler(t *testing.T) {
	t.Parallel()
	logger, _, _ := logging.NewLogger()
	promConfig := &config.PrometheusConfig{Enabled: true}
	srv := server.NewMetricsServer(logger, promConfig)
	handler := srv.Handler()

	t.Run("serves /metrics with Prometheus format", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
		body := w.Body.String()
		if !strings.Contains(body, "go_goroutines") {
			t.Error("response should contain go runtime metrics")
		}
		if !strings.Contains(body, "# HELP") {
			t.Error("response should contain Prometheus exposition format")
		}
	})

	t.Run("returns 404 for non-metrics paths", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404 for /api/v1/health, got %d", w.Code)
		}
	})
}
