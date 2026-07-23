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
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	gootel "go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

func TestSetupOTELExportsMetricsViaOTLPGRPC(t *testing.T) {
	t.Run("exports otelhttp server duration", func(t *testing.T) {
		collector, shutdownOTEL := setupOTELWithCollector(t)
		defer collector.Shutdown()
		defer func() { _ = shutdownOTEL(context.Background()) }()

		handler := otelhttp.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}), "/api/v1/health")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}

		waitForExportedMetrics(t, req.Context(), collector)

		names := oteltest.MetricNames(collector.ResourceMetrics())
		if _, ok := names["http.server.request.duration"]; !ok {
			t.Error("missing exported metric http.server.request.duration")
		}
	})

	t.Run("exports semconv HTTP request metrics via middleware", func(t *testing.T) {
		collector, shutdownOTEL := setupOTELWithCollector(t)
		defer collector.Shutdown()
		defer func() { _ = shutdownOTEL(context.Background()) }()

		handler := server.HTTPMetricsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}), true, logging.FallbackLogger())

		req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
		req.Pattern = "/api/v1/health"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		waitForExportedMetrics(t, req.Context(), collector)

		exported := collector.ResourceMetrics()
		names := oteltest.MetricNames(exported)
		for _, want := range []string{"http.server.request.count", "http.server.active_requests"} {
			if _, ok := names[want]; !ok {
				t.Errorf("missing exported metric %q", want)
			}
		}
		if !oteltest.HasIntSumDataPoint(exported, "http.server.request.count", "http.route", "/api/v1/health") {
			t.Error("http.server.request.count missing http.route=/api/v1/health attribute")
		}
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
