package otel

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/pkg/api"
	"go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"
)

type captureLogExporter struct {
	mu      sync.Mutex
	records []sdklog.Record
}

func (e *captureLogExporter) Export(_ context.Context, records []sdklog.Record) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.records = append(e.records, records...)
	return nil
}

func (e *captureLogExporter) Shutdown(context.Context) error { return nil }

func (e *captureLogExporter) ForceFlush(context.Context) error { return nil }

func (e *captureLogExporter) len() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.records)
}

func TestParseBenchmarkIDFromSectionHeader(t *testing.T) {
	header := "=== pod=pod-1 container=adapter benchmark_id=bench-1 ==="
	if got := parseBenchmarkIDFromSectionHeader(header); got != "bench-1" {
		t.Fatalf("got %q, want bench-1", got)
	}
}

func TestEmitContainerLogs(t *testing.T) {
	exporter := &captureLogExporter{}
	provider := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(exporter)))
	global.SetLoggerProvider(provider)
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	job := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{ID: "job-1"},
		},
		Status: &api.EvaluationJobStatus{
			EvaluationJobState: api.EvaluationJobState{State: api.OverallStateCompleted},
		},
	}

	emitContainerLogs(context.Background(), job, "=== pod=pod-1 container=adapter benchmark_id=bench-1 ===\nline one\nline two")
	if exporter.len() != 2 {
		t.Fatalf("expected 2 log records, got %d", exporter.len())
	}
}

func TestBridgeSlogToOTELPreservesBaseOutput(t *testing.T) {
	exporter := &captureLogExporter{}
	provider := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(exporter)))
	global.SetLoggerProvider(provider)
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	base := slog.New(slog.NewTextHandler(discardWriter{}, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger := BridgeSlogToOTEL(base)
	logger.Info("hello otel bridge")

	if exporter.len() != 1 {
		t.Fatalf("expected 1 OTEL log record, got %d", exporter.len())
	}
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestExportJobContainerLogsAsyncNoopWithoutProvider(t *testing.T) {
	global.SetLoggerProvider(nil)
	ExportJobContainerLogsAsync(context.Background(), nil, &api.EvaluationJobResource{}, nil, slog.Default())
}

func TestExportJobContainerLogsAsyncInvokesRuntime(t *testing.T) {
	exporter := &captureLogExporter{}
	provider := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(exporter)))
	global.SetLoggerProvider(provider)
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	exported := make(chan struct{})
	runtime := &stubLogsRuntime{
		logs:     "line from container\n",
		exported: exported,
	}
	job := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-async"}},
		Status:   &api.EvaluationJobStatus{EvaluationJobState: api.EvaluationJobState{State: api.OverallStateCompleted}},
	}
	benchmarks := []api.EvaluationBenchmarkConfig{{Ref: api.Ref{ID: "bench-1"}, ProviderID: "provider-1"}}

	ExportJobContainerLogsAsync(context.Background(), runtime, job, benchmarks, slog.Default())

	select {
	case <-exported:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for async log fetch")
	}
}

type stubLogsRuntime struct {
	logs     string
	exported chan struct{}
}

func (r *stubLogsRuntime) WithLogger(_ *slog.Logger) abstractions.Runtime { return r }
func (r *stubLogsRuntime) WithContext(_ context.Context) abstractions.Runtime {
	return r
}
func (r *stubLogsRuntime) Name() string { return "stub" }
func (r *stubLogsRuntime) RunEvaluationJob(_ *api.EvaluationJobResource, _ []api.EvaluationBenchmarkConfig, _ abstractions.RuntimeStorage) error {
	return nil
}
func (r *stubLogsRuntime) DeleteEvaluationJobResources(_ *api.EvaluationJobResource) error {
	return nil
}
func (r *stubLogsRuntime) GetEvaluationLogs(_ *api.EvaluationJobResource, _ []api.EvaluationBenchmarkConfig, _ *int, _ api.EvaluationLogOptions) (string, error) {
	close(r.exported)
	return r.logs, nil
}
