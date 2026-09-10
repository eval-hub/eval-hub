package metrics_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/metrics"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func setupOTEL(t *testing.T) (*sdkmetric.ManualReader, context.Context) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	otel.SetMeterProvider(provider)
	if err := metrics.Init(); err != nil {
		t.Fatalf("metrics.Init: %v", err)
	}
	return reader, context.Background()
}

func collectOTELNames(t *testing.T, reader *sdkmetric.ManualReader, ctx context.Context) map[string]struct{} {
	t.Helper()
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
	return names
}

func getPromMetricValue(name string) float64 {
	mfs, _ := prometheus.DefaultGatherer.Gather()
	for _, mf := range mfs {
		if mf.GetName() == name {
			for _, m := range mf.GetMetric() {
				if m.GetCounter() != nil {
					return m.GetCounter().GetValue()
				}
				if m.GetGauge() != nil {
					return m.GetGauge().GetValue()
				}
				if m.GetHistogram() != nil {
					return float64(m.GetHistogram().GetSampleCount())
				}
			}
		}
	}
	return -1
}

func getPromMetricWithLabels(name string, labels map[string]string) *dto.Metric {
	mfs, _ := prometheus.DefaultGatherer.Gather()
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			if matchLabels(m.GetLabel(), labels) {
				return m
			}
		}
	}
	return nil
}

func matchLabels(pairs []*dto.LabelPair, want map[string]string) bool {
	if len(pairs) != len(want) {
		return false
	}
	have := make(map[string]string, len(pairs))
	for _, p := range pairs {
		have[p.GetName()] = p.GetValue()
	}
	for k, v := range want {
		if have[k] != v {
			return false
		}
	}
	return true
}

func TestInitCreatesEvaluationJobInstruments(t *testing.T) {
	reader, ctx := setupOTEL(t)

	metrics.RecordEvaluationJobCreated(ctx, "kubernetes")
	metrics.RecordEvaluationJobCancelled(ctx)
	metrics.RecordEvaluationJobRuntimeStartFailed(ctx, "local")
	metrics.RecordEvaluationJobTerminalState(ctx, api.OverallStateRunning, api.OverallStateCompleted)
	metrics.RecordBenchmarkRuntimeError(ctx, "kubernetes")
	metrics.RecordHTTPServerRequest(ctx, http.MethodGet, "/api/v1/health", http.StatusOK)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/health", nil)
	metrics.IncHTTPServerActiveRequests(ctx, req)
	metrics.DecHTTPServerActiveRequests(ctx, req)

	names := collectOTELNames(t, reader, ctx)

	for _, want := range []string{
		"evalhub.evaluation_job_actions",
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

func TestInitCreatesEvaluationDomainOTELInstruments(t *testing.T) {
	reader, ctx := setupOTEL(t)

	metrics.RecordEvaluationJobStateTransition(ctx, "prov", "coll", "pending")
	metrics.ObserveEvaluationJobDuration(ctx, "prov", "coll", 10.0)
	metrics.IncActiveJobs(ctx)
	metrics.DecActiveJobs(ctx)
	metrics.IncQueueDepth(ctx)
	metrics.DecQueueDepth(ctx)
	metrics.RecordEvaluationError(ctx, "test_err", "prov")
	metrics.ObserveBenchmarkDuration(ctx, "mmlu", "prov", 5.0)
	metrics.ObserveAPIRequestDuration(ctx, "/health", "GET", "", "", 0.01)

	names := collectOTELNames(t, reader, ctx)

	for _, want := range []string{
		"evalhub.eval.job_state_transitions",
		"evalhub.eval.job_duration",
		"evalhub.eval.active_jobs",
		"evalhub.eval.queue_depth",
		"evalhub.eval.errors",
		"evalhub.eval.benchmark_duration",
		"evalhub.eval.api_request_duration",
	} {
		if _, ok := names[want]; !ok {
			t.Errorf("missing OTEL evaluation-domain metric %q", want)
		}
	}
}

func TestRecordEvaluationJobStateTransition(t *testing.T) {
	reader, ctx := setupOTEL(t)

	metrics.RecordEvaluationJobStateTransition(ctx, "llm-judge", "safety", "pending")
	metrics.RecordEvaluationJobStateTransition(ctx, "llm-judge", "safety", "running")
	metrics.RecordEvaluationJobStateTransition(ctx, "llm-judge", "safety", "completed")

	m := getPromMetricWithLabels("evalhub_evaluation_jobs_total", map[string]string{
		"provider": "llm-judge", "collection": "safety", "status": "completed",
	})
	if m == nil {
		t.Fatal("evalhub_evaluation_jobs_total{status=completed} not found")
	}
	if got := m.GetCounter().GetValue(); got != 1 {
		t.Errorf("expected counter=1, got %v", got)
	}

	names := collectOTELNames(t, reader, ctx)
	if _, ok := names["evalhub.eval.job_state_transitions"]; !ok {
		t.Error("OTEL instrument evalhub.eval.job_state_transitions not recorded")
	}
}

func TestObserveEvaluationJobDuration(t *testing.T) {
	_, ctx := setupOTEL(t)

	metrics.ObserveEvaluationJobDuration(ctx, "prov-a", "coll-x", 25.5)

	m := getPromMetricWithLabels("evalhub_evaluation_job_duration_seconds", map[string]string{
		"provider": "prov-a", "collection": "coll-x",
	})
	if m == nil {
		t.Fatal("evalhub_evaluation_job_duration_seconds not found")
	}
	if got := m.GetHistogram().GetSampleCount(); got != 1 {
		t.Errorf("expected sample_count=1, got %v", got)
	}
}

func TestActiveJobsAndQueueDepthGauges(t *testing.T) {
	_, ctx := setupOTEL(t)

	metrics.IncActiveJobs(ctx)
	metrics.IncActiveJobs(ctx)
	metrics.IncQueueDepth(ctx)
	metrics.IncQueueDepth(ctx)
	metrics.DecQueueDepth(ctx)

	activeVal := getPromMetricValue("evalhub_evaluation_jobs_active")
	if activeVal < 1 {
		t.Errorf("expected evalhub_evaluation_jobs_active > 0, got %v", activeVal)
	}

	queueVal := getPromMetricValue("evalhub_evaluation_queue_depth")
	if queueVal < 0 {
		t.Errorf("expected evalhub_evaluation_queue_depth >= 0, got %v", queueVal)
	}
}

func TestRecordEvaluationError(t *testing.T) {
	_, ctx := setupOTEL(t)

	metrics.RecordEvaluationError(ctx, "k8s_create_failed", "llm-judge")

	m := getPromMetricWithLabels("evalhub_evaluation_errors_total", map[string]string{
		"error_type": "k8s_create_failed", "provider": "llm-judge",
	})
	if m == nil {
		t.Fatal("evalhub_evaluation_errors_total not found")
	}
	if got := m.GetCounter().GetValue(); got < 1 {
		t.Errorf("expected counter >= 1, got %v", got)
	}
}

func TestObserveBenchmarkDuration(t *testing.T) {
	_, ctx := setupOTEL(t)

	metrics.ObserveBenchmarkDuration(ctx, "mmlu", "llm-judge", 45.2)

	m := getPromMetricWithLabels("evalhub_benchmark_duration_seconds", map[string]string{
		"benchmark_name": "mmlu", "provider": "llm-judge",
	})
	if m == nil {
		t.Fatal("evalhub_benchmark_duration_seconds not found")
	}
	if got := m.GetHistogram().GetSampleCount(); got != 1 {
		t.Errorf("expected sample_count=1, got %v", got)
	}
}

func TestObserveAPIRequestDuration(t *testing.T) {
	_, ctx := setupOTEL(t)

	metrics.ObserveAPIRequestDuration(ctx, "/api/v1/evaluations/jobs", "POST", "safety", "llm-judge", 0.123)

	m := getPromMetricWithLabels("evalhub_api_request_duration_seconds", map[string]string{
		"endpoint": "/api/v1/evaluations/jobs", "method": "POST",
		"collection": "safety", "provider": "llm-judge",
	})
	if m == nil {
		t.Fatal("evalhub_api_request_duration_seconds not found")
	}
	if got := m.GetHistogram().GetSampleCount(); got < 1 {
		t.Errorf("expected sample_count >= 1, got %v", got)
	}
}
