package otel

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestDetachedContextPreservesSpanContextAsRemoteLink(t *testing.T) {
	tp := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	ctx, span := tp.Tracer("test").Start(context.Background(), "parent")
	wantSC := span.SpanContext()
	span.End()

	detached := DetachedContext(ctx)

	gotSC := trace.SpanContextFromContext(detached)
	if !gotSC.IsValid() {
		t.Fatal("DetachedContext dropped the span context; want it preserved for linking")
	}
	if gotSC.TraceID() != wantSC.TraceID() || gotSC.SpanID() != wantSC.SpanID() {
		t.Errorf("DetachedContext span context = %v, want trace/span IDs matching %v", gotSC, wantSC)
	}
	if !gotSC.IsRemote() {
		t.Error("DetachedContext should mark the carried span context as remote (link source, not parent)")
	}

	link := trace.LinkFromContext(detached)
	if link.SpanContext.TraceID() != wantSC.TraceID() {
		t.Errorf("trace.LinkFromContext(detached).SpanContext.TraceID() = %v, want %v", link.SpanContext.TraceID(), wantSC.TraceID())
	}
}

func TestDetachedContextDropsCancellationAndDeadline(t *testing.T) {
	tp := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	parent, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	ctx, span := tp.Tracer("test").Start(parent, "parent")
	defer span.End()

	detached := DetachedContext(ctx)

	cancel()
	time.Sleep(2 * time.Millisecond)

	if err := detached.Err(); err != nil {
		t.Errorf("DetachedContext should not inherit cancellation/deadline from ctx, got Err() = %v", err)
	}
	if _, ok := detached.Deadline(); ok {
		t.Error("DetachedContext should have no deadline")
	}
}

func TestDetachedContextWithoutValidSpanReturnsBackground(t *testing.T) {
	detached := DetachedContext(context.Background())

	if sc := trace.SpanContextFromContext(detached); sc.IsValid() {
		t.Errorf("DetachedContext(context.Background()) span context = %v, want invalid", sc)
	}
}

func TestStartLinkedSpanLinksRatherThanParents(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	prevTP := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(prevTP) })

	ctx, requestSpan := tp.Tracer("test").Start(context.Background(), "request")
	requestSC := requestSpan.SpanContext()
	requestSpan.End()

	detached := DetachedContext(ctx)

	_, asyncSpan := StartLinkedSpan(detached, "test", "async.work")
	asyncSpan.End()

	asyncSC := asyncSpan.SpanContext()
	if asyncSC.TraceID() == requestSC.TraceID() {
		t.Error("StartLinkedSpan should start a new trace, not continue the request's trace")
	}

	var links []sdktrace.Link
	for _, s := range recorder.Ended() {
		if s.Name() == "async.work" {
			links = s.Links()
		}
	}
	if len(links) != 1 || links[0].SpanContext.TraceID() != requestSC.TraceID() || links[0].SpanContext.SpanID() != requestSC.SpanID() {
		t.Errorf("async.work span links = %v, want a single link back to the request span %v", links, requestSC)
	}
}

func TestStartLinkedSpanWithoutValidLinkStartsPlainSpan(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	prevTP := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(prevTP) })

	_, span := StartLinkedSpan(context.Background(), "test", "async.work")
	span.End()

	for _, s := range recorder.Ended() {
		if s.Name() == "async.work" && len(s.Links()) != 0 {
			t.Errorf("async.work span links = %v, want none", s.Links())
		}
	}
}

func TestDetachedContextHandlesNilContext(t *testing.T) {
	detached := DetachedContext(nil) //nolint:staticcheck // intentional: verify the explicit nil guard in DetachedContext

	if sc := trace.SpanContextFromContext(detached); sc.IsValid() {
		t.Errorf("DetachedContext(nil) span context = %v, want invalid", sc)
	}
}
