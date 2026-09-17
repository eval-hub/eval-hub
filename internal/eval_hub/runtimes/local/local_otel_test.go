package local

import (
	"context"
	"errors"
	"testing"

	"github.com/eval-hub/eval-hub/internal/otel"
	"github.com/eval-hub/eval-hub/pkg/api"
	goopentelemetry "go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// withTestTracerProvider installs tp as the global OTEL tracer provider for
// the duration of the test and restores the previous provider on cleanup.
func withTestTracerProvider(t *testing.T, tp trace.TracerProvider) {
	t.Helper()
	prev := goopentelemetry.GetTracerProvider()
	goopentelemetry.SetTracerProvider(tp)
	t.Cleanup(func() { goopentelemetry.SetTracerProvider(prev) })
}

func TestStartBenchmarkSpanLinksBackToDetachedJobTrace(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	withTestTracerProvider(t, tp)

	jobCtx, jobSpan := tp.Tracer("test").Start(context.Background(), "create-job")
	jobSC := jobSpan.SpanContext()
	jobSpan.End()

	detached := otel.DetachedContext(jobCtx)

	bench := api.EvaluationBenchmarkConfig{Ref: api.Ref{ID: "bench-1"}, ProviderID: "provider-1"}
	_, span := startBenchmarkSpan(detached, bench, 2)
	span.End()

	var links []sdktrace.Link
	for _, s := range recorder.Ended() {
		if s.Name() == "local.run_benchmark" {
			links = s.Links()
		}
	}
	if len(links) != 1 || links[0].SpanContext.TraceID() != jobSC.TraceID() {
		t.Errorf("local.run_benchmark span links = %v, want a single link back to the job span %v", links, jobSC)
	}
}

func TestEndBenchmarkSpanRecordsErrorStatus(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	withTestTracerProvider(t, tp)

	bench := api.EvaluationBenchmarkConfig{Ref: api.Ref{ID: "bench-1"}, ProviderID: "provider-1"}
	_, span := startBenchmarkSpan(context.Background(), bench, 0)
	endBenchmarkSpan(span, errors.New("boom"))

	for _, s := range recorder.Ended() {
		if s.Name() == "local.run_benchmark" {
			if s.Status().Code != codes.Error {
				t.Errorf("status code = %v, want Error", s.Status().Code)
			}
			if len(s.Events()) == 0 {
				t.Error("expected RecordError to add an exception event")
			}
		}
	}
}

func TestEndBenchmarkSpanRecordsOKStatusOnSuccess(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	withTestTracerProvider(t, tp)

	bench := api.EvaluationBenchmarkConfig{Ref: api.Ref{ID: "bench-1"}, ProviderID: "provider-1"}
	_, span := startBenchmarkSpan(context.Background(), bench, 0)
	endBenchmarkSpan(span, nil)

	for _, s := range recorder.Ended() {
		if s.Name() == "local.run_benchmark" {
			if s.Status().Code != codes.Ok {
				t.Errorf("status code = %v, want Ok", s.Status().Code)
			}
		}
	}
}
