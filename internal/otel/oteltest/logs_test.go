package oteltest

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	otellog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/trace"
)

// TestGRPCLogsCollectorCapturesExportedRecord exercises the full OTLP gRPC log
// path: a real exporter sends a record (emitted within a span context) to the
// collector, and the helper functions read back its body, attributes, severity,
// and trace/span correlation. This mirrors the wire behaviour of the Python
// adapter without depending on a Python runtime.
func TestGRPCLogsCollectorCapturesExportedRecord(t *testing.T) {
	collector, err := NewGRPCLogsCollector()
	if err != nil {
		t.Fatalf("NewGRPCLogsCollector: %v", err)
	}
	t.Cleanup(collector.Shutdown)

	ctx := context.Background()
	exporter, err := otlploggrpc.New(ctx,
		otlploggrpc.WithEndpoint(collector.Endpoint()),
		otlploggrpc.WithInsecure(),
	)
	if err != nil {
		t.Fatalf("otlploggrpc.New: %v", err)
	}
	provider := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(exporter)))
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = provider.Shutdown(shutdownCtx)
	})

	// Emit within a span context so the record carries trace/span ids, matching
	// the adapter emitting logs inside its root evaluation span.
	traceID := trace.TraceID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}
	spanID := trace.SpanID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	spanCtx := trace.ContextWithSpanContext(ctx, trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	}))

	var record otellog.Record
	record.SetBody(attribute.StringValue("EVALUATION COMPLETE"))
	record.SetSeverity(otellog.SeverityInfo)
	record.SetSeverityText("INFO")
	record.AddAttributes(
		attribute.String("evalhub.job_id", "job-123"),
		attribute.String("evalhub.benchmark_id", "arc_easy"),
	)
	provider.Logger("test").Emit(spanCtx, record)

	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := provider.ForceFlush(shutdownCtx); err != nil {
		t.Fatalf("ForceFlush: %v", err)
	}

	got := FindLogRecordByBody(collector.LogRecords(), "EVALUATION COMPLETE")
	if got == nil {
		t.Fatal("collector did not capture the exported log record")
	}

	if sev := got.GetSeverityText(); sev != "INFO" {
		t.Errorf("severity text = %q, want INFO", sev)
	}
	if v, ok := LogRecordAttribute(got, "evalhub.job_id"); !ok || v != "job-123" {
		t.Errorf("evalhub.job_id = %q (present=%v), want job-123", v, ok)
	}
	if v, ok := LogRecordAttribute(got, "evalhub.benchmark_id"); !ok || v != "arc_easy" {
		t.Errorf("evalhub.benchmark_id = %q (present=%v), want arc_easy", v, ok)
	}
	if _, ok := LogRecordAttribute(got, "missing.attr"); ok {
		t.Error("LogRecordAttribute reported a missing attribute as present")
	}
	if len(got.GetTraceId()) == 0 {
		t.Error("log record missing trace id")
	}
	if len(got.GetSpanId()) == 0 {
		t.Error("log record missing span id")
	}
}

// TestGRPCLogsCollectorExportNilRequest verifies a nil request is handled.
func TestGRPCLogsCollectorExportNilRequest(t *testing.T) {
	collector, err := NewGRPCLogsCollector()
	if err != nil {
		t.Fatalf("NewGRPCLogsCollector: %v", err)
	}
	t.Cleanup(collector.Shutdown)

	svc := &logsService{c: collector}
	if _, err := svc.Export(context.Background(), nil); err != nil {
		t.Fatalf("Export(nil) returned error: %v", err)
	}
	if len(collector.LogRecords()) != 0 {
		t.Errorf("expected no records, got %d", len(collector.LogRecords()))
	}
}
