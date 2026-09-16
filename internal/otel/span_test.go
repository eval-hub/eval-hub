package otel

import (
	"context"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
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

func TestDetachedContextHandlesNilContext(t *testing.T) {
	detached := DetachedContext(nil) //nolint:staticcheck // intentional: exercise the nil-safety guarantee documented on DetachedContext

	if sc := trace.SpanContextFromContext(detached); sc.IsValid() {
		t.Errorf("DetachedContext(nil) span context = %v, want invalid", sc)
	}
}
