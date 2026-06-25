package metrics_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/metrics"
	"github.com/eval-hub/eval-hub/pkg/api"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestInitCreatesEvaluationJobInstruments(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	otel.SetMeterProvider(provider)

	if err := metrics.Init(); err != nil {
		t.Fatalf("metrics.Init: %v", err)
	}

	ctx := context.Background()
	metrics.RecordEvaluationJobCreated(ctx, "kubernetes")
	metrics.RecordEvaluationJobCancelled(ctx)
	metrics.RecordEvaluationJobRuntimeStartFailed(ctx, "local")
	metrics.RecordEvaluationJobTerminalState(ctx, api.OverallStateRunning, api.OverallStateCompleted)
	metrics.RecordBenchmarkRuntimeError(ctx, "kubernetes")
	metrics.RecordHTTPServerRequest(ctx, http.MethodGet, "/api/v1/health", http.StatusOK)
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/health", nil)
	metrics.IncHTTPServerActiveRequests(ctx, req)
	metrics.DecHTTPServerActiveRequests(ctx, req)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	names := make(map[string]struct{})
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			names[m.Name] = struct{}{}
		}
	}

	for _, want := range []string{
		"evalhub.evaluation_jobs",
		"evalhub.evaluation_job_completions",
		"evalhub.benchmark_runtime_errors",
		"http.server.request.count",
		"http.server.active_requests",
	} {
		if _, ok := names[want]; !ok {
			t.Errorf("missing metric %q", want)
		}
	}
}
