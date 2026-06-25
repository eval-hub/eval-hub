package metrics_test

import (
	"context"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/metrics"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func setupTestMeterProvider(t *testing.T) *metric.ManualReader {
	t.Helper()
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
	})
	if err := metrics.Init(); err != nil {
		t.Fatalf("metrics.Init: %v", err)
	}
	return reader
}

func TestInitCreatesHTTPMetrics(t *testing.T) {
	reader := setupTestMeterProvider(t)

	metrics.RecordHTTPRequest(context.Background(), "GET", "/api/v1/health", "200")
	metrics.IncHTTPInFlight(context.Background())
	metrics.DecHTTPInFlight(context.Background())

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}

	names := metricNames(rm)
	for _, want := range []string{"http_requests_total", "http_requests_in_flight"} {
		if !names[want] {
			t.Errorf("missing metric %q", want)
		}
	}
}

func metricNames(rm metricdata.ResourceMetrics) map[string]bool {
	found := make(map[string]bool)
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			found[m.Name] = true
		}
	}
	return found
}
