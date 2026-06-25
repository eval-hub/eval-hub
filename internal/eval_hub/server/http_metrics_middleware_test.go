package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/metrics"
	"github.com/eval-hub/eval-hub/internal/logging"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/metric"
)

func TestHTTPMetricsMiddleware(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(t.Context()) })
	otel.SetMeterProvider(provider)

	if err := metrics.Init(); err != nil {
		t.Fatalf("metrics.Init: %v", err)
	}

	handler := HTTPMetricsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), true, logging.FallbackLogger())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	req.Pattern = "/api/v1/health"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
}
