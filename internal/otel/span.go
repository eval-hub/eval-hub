package otel

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type SpanFunction func(context.Context) error

// DetachedContext returns a context.Background()-derived context that carries
// forward ctx's span context as a remote reference, without inheriting ctx's
// cancellation or deadline (nor any context values). Use this at
// request/background boundaries — e.g. before handing evaluation-job
// execution off to a runtime goroutine that must outlive the HTTP request
// that triggered it — so that spans started later against the returned
// context can still be associated with the originating trace via
// trace.LinkFromContext(ctx) and trace.WithLinks, without incorrectly
// extending the original span's parent-child chain across the async gap
// (see OTEL.md "Trace continuity").
//
// If ctx is nil or carries no valid span context (e.g. tracing disabled, or
// ctx is already detached), this is equivalent to context.Background().
func DetachedContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return context.Background()
	}
	return trace.ContextWithRemoteSpanContext(context.Background(), sc)
}

// StartLinkedSpan starts a new span in a fresh trace (trace.WithNewRoot),
// linked to — not parented by — any span context carried in ctx (typically a
// context produced by DetachedContext). Use this for the first span created
// against a detached context, to bridge otherwise-disjoint traces for
// genuinely async work (see DetachedContext and OTEL.md "Trace continuity").
// Spans started later against the returned context behave as normal
// children of the returned span, not additional links.
//
// Caller-provided opts are appended after WithNewRoot and WithLinks, so
// they take precedence for any conflicting option.
//
// If ctx carries no valid span context (e.g. tracing disabled), this
// degrades to a plain tracer.Start(ctx, spanName, opts...).
func StartLinkedSpan(ctx context.Context, tracerName, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	if link := trace.LinkFromContext(ctx); link.SpanContext.IsValid() {
		opts = append([]trace.SpanStartOption{trace.WithNewRoot(), trace.WithLinks(link)}, opts...)
	}
	return otel.Tracer(tracerName).Start(ctx, spanName, opts...)
}

func WithSpan(ctx context.Context, serviceConfig *config.Config, logger *slog.Logger, component string, operation string, attributes map[string]string, fn SpanFunction) error {
	runtimeCtx := ctx
	var runtimeSpan trace.Span

	if serviceConfig.IsOTELEnabled() {
		logger.Debug("Starting OTEL span", "component", component, "operation", operation)
		// Create child span for validation
		runtimeCtx, runtimeSpan = otel.Tracer(component).Start(
			ctx,
			operation,
		)

		var atts []attribute.KeyValue
		for key, value := range attributes {
			if value != "" {
				atts = append(atts, attribute.String(key, value))
			}
		}
		runtimeSpan.SetAttributes(atts...)
	}

	err := fn(runtimeCtx)

	if runtimeSpan != nil {
		if err != nil {
			// Set failed status on root span
			runtimeSpan.SetStatus(codes.Error, fmt.Sprintf("%s failed", operation))
		} else {
			// Set success status on root span
			runtimeSpan.SetStatus(codes.Ok, fmt.Sprintf("%s successful", operation))
		}
		runtimeSpan.End()
		logger.Debug("OTEL span ended", "component", component, "operation", operation)
	}

	return err
}
