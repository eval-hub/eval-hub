package k8s

import (
	"context"
	"errors"
	"testing"

	"github.com/eval-hub/eval-hub/internal/otel"
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

func TestStartK8sSpanLinksBackToDetachedRequestTrace(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	withTestTracerProvider(t, tp)

	reqCtx, reqSpan := tp.Tracer("test").Start(context.Background(), "request")
	reqSC := reqSpan.SpanContext()
	reqSpan.End()

	detached := otel.DetachedContext(reqCtx)

	_, span := startK8sSpan(detached, "create_job", "Job", "team-a", "job-1")
	span.End()

	var links []sdktrace.Link
	for _, s := range recorder.Ended() {
		if s.Name() == "k8s.create_job" {
			links = s.Links()
		}
	}
	if len(links) != 1 || links[0].SpanContext.TraceID() != reqSC.TraceID() {
		t.Errorf("k8s.create_job span links = %v, want a single link back to the request span %v", links, reqSC)
	}
}

func TestEndK8sSpanRecordsErrorStatus(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	withTestTracerProvider(t, tp)

	_, span := startK8sSpan(context.Background(), "create_job", "Job", "team-a", "job-1")
	endK8sSpan(span, errors.New("boom"))

	for _, s := range recorder.Ended() {
		if s.Name() == "k8s.create_job" {
			if s.Status().Code != codes.Error {
				t.Errorf("status code = %v, want Error", s.Status().Code)
			}
			if len(s.Events()) == 0 {
				t.Error("expected RecordError to add an exception event")
			}
		}
	}
}

func TestEndK8sSpanRecordsOKStatusOnSuccess(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	withTestTracerProvider(t, tp)

	_, span := startK8sSpan(context.Background(), "delete_job", "Job", "team-a", "job-1")
	endK8sSpan(span, nil)

	for _, s := range recorder.Ended() {
		if s.Name() == "k8s.delete_job" {
			if s.Status().Code != codes.Ok {
				t.Errorf("status code = %v, want Ok", s.Status().Code)
			}
		}
	}
}
