package otel_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_hub/metrics"
	"github.com/eval-hub/eval-hub/internal/eval_hub/server"
	"github.com/eval-hub/eval-hub/internal/logging"
	"github.com/eval-hub/eval-hub/internal/otel"
	oteltest "github.com/eval-hub/eval-hub/internal/otel/oteltest"
	gootel "go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

func TestSetupOTELExportsHTTPMetricsViaOTLPGRPC(t *testing.T) {
	t.Run("records counters directly", func(t *testing.T) {
		collector, shutdownOTEL := setupOTELWithCollector(t)
		defer collector.Shutdown()
		defer shutdownOTEL(context.Background())

		ctx := context.Background()
		metrics.RecordHTTPRequest(ctx, http.MethodGet, "/api/v1/health", "200")
		metrics.IncHTTPInFlight(ctx)
		metrics.DecHTTPInFlight(ctx)

		waitForExportedMetrics(t, ctx, collector)

		assertExportedHTTPMetrics(t, collector, "/api/v1/health")
	})

	t.Run("records counters through HTTP middleware", func(t *testing.T) {
		collector, shutdownOTEL := setupOTELWithCollector(t)
		defer collector.Shutdown()
		defer shutdownOTEL(context.Background())

		handler := server.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}), true, logging.FallbackLogger())

		req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}

		waitForExportedMetrics(t, req.Context(), collector)

		assertExportedHTTPMetrics(t, collector, "/api/v1/health")
	})
}

func setupOTELWithCollector(t *testing.T) (*oteltest.GRPCCollector, func(context.Context) error) {
	t.Helper()

	collector, err := oteltest.NewGRPCCollector()
	if err != nil {
		t.Fatalf("NewGRPCCollector: %v", err)
	}

	cfg := &config.OTELConfig{
		Enabled:              true,
		EnableMetrics:        true,
		ExporterType:         otel.ExporterTypeOTLPGRPC,
		ExporterEndpoint:     collector.Endpoint(),
		ExporterInsecure:     true,
		MetricExportInterval: 20 * time.Millisecond,
	}

	shutdown, err := otel.SetupOTEL(context.Background(), cfg, slog.Default(), false)
	if err != nil {
		collector.Shutdown()
		t.Fatalf("SetupOTEL: %v", err)
	}
	if err := metrics.Init(); err != nil {
		collector.Shutdown()
		_ = shutdown(context.Background())
		t.Fatalf("metrics.Init: %v", err)
	}
	return collector, shutdown
}

func waitForExportedMetrics(t *testing.T, ctx context.Context, collector *oteltest.GRPCCollector) {
	t.Helper()

	mp, ok := gootel.GetMeterProvider().(*sdkmetric.MeterProvider)
	if !ok {
		t.Fatal("global MeterProvider is not *sdkmetric.MeterProvider")
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := mp.ForceFlush(ctx); err != nil {
			t.Fatalf("ForceFlush: %v", err)
		}
		if len(collector.ResourceMetrics()) > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for OTLP metric export")
}

func assertExportedHTTPMetrics(t *testing.T, collector *oteltest.GRPCCollector, endpoint string) {
	t.Helper()

	exported := collector.ResourceMetrics()
	names := oteltest.MetricNames(exported)

	for _, want := range []string{"http_requests_total", "http_requests_in_flight"} {
		if _, ok := names[want]; !ok {
			t.Errorf("missing exported metric %q", want)
		}
	}

	if !oteltest.HasIntSumDataPoint(exported, "http_requests_total", "method", http.MethodGet) {
		t.Error("http_requests_total missing method=GET attribute")
	}
	if !oteltest.HasIntSumDataPoint(exported, "http_requests_total", "endpoint", endpoint) {
		t.Errorf("http_requests_total missing endpoint=%q attribute", endpoint)
	}
	if !oteltest.HasIntSumDataPoint(exported, "http_requests_total", "status", "200") {
		t.Error("http_requests_total missing status=200 attribute")
	}
}
