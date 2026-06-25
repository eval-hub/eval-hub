package metrics_test

import (
	"context"
	"crypto/tls"
	"net/http"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/metrics"
	"github.com/eval-hub/eval-hub/pkg/api"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func setupMetricsTest(t *testing.T) (*metric.ManualReader, context.Context) {
	t.Helper()

	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	otel.SetMeterProvider(provider)

	if err := metrics.Init(); err != nil {
		t.Fatalf("metrics.Init: %v", err)
	}
	return reader, context.Background()
}

func collectIntSumValue(t *testing.T, reader *metric.ManualReader, ctx context.Context, name string, attrs ...attribute.KeyValue) int64 {
	t.Helper()

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %q is not an int64 sum", name)
			}
			for _, dp := range sum.DataPoints {
				if len(attrs) == 0 {
					return dp.Value
				}
				if attributesMatch(dp.Attributes, attrs) {
					return dp.Value
				}
			}
		}
	}
	t.Fatalf("metric %q not found", name)
	return 0
}

func attributesMatch(set attribute.Set, want []attribute.KeyValue) bool {
	for _, w := range want {
		v, ok := set.Value(w.Key)
		if !ok || v != w.Value {
			return false
		}
	}
	return true
}

func TestRecordEvaluationJobTerminalStateNoOps(t *testing.T) {
	reader, ctx := setupMetricsTest(t)

	metrics.RecordEvaluationJobTerminalState(ctx, api.OverallStateRunning, api.OverallStateCompleted)
	if got := collectIntSumValue(t, reader, ctx, "evalhub.evaluation_job_completions"); got != 1 {
		t.Fatalf("completions = %d, want 1", got)
	}

	metrics.RecordEvaluationJobTerminalState(ctx, api.OverallStateRunning, api.OverallStatePending)
	metrics.RecordEvaluationJobTerminalState(ctx, api.OverallStateCompleted, api.OverallStateCompleted)

	if got := collectIntSumValue(t, reader, ctx, "evalhub.evaluation_job_completions"); got != 1 {
		t.Fatalf("completions after no-ops = %d, want 1", got)
	}
}

func TestRecordHTTPServerRequestEmptyRoute(t *testing.T) {
	reader, ctx := setupMetricsTest(t)

	metrics.RecordHTTPServerRequest(ctx, http.MethodPost, "", http.StatusNotFound)

	got := collectIntSumValue(t, reader, ctx, "http.server.request.count",
		attribute.String("http.request.method", http.MethodPost),
		attribute.String("http.route", "not_found"),
		attribute.Int("http.response.status_code", http.StatusNotFound),
	)
	if got != 1 {
		t.Fatalf("request count = %d, want 1", got)
	}
}

func TestHTTPServerActiveRequestsUsesHTTPSScheme(t *testing.T) {
	reader, ctx := setupMetricsTest(t)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example/health", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	req.TLS = &tls.ConnectionState{}

	metrics.IncHTTPServerActiveRequests(ctx, req)
	metrics.DecHTTPServerActiveRequests(ctx, req)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	found := false
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "http.server.active_requests" {
				continue
			}
			found = true
		}
	}
	if !found {
		t.Fatal("expected http.server.active_requests metric")
	}
}
