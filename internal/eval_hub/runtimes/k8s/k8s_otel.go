package k8s

import (
	"context"

	"github.com/eval-hub/eval-hub/internal/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.39.0"
	"go.opentelemetry.io/otel/trace"
)

// k8sTracerName scopes spans emitted by the Kubernetes runtime's client-go
// calls, matching the OTEL.md "Related files" convention of one tracer name
// per component.
const k8sTracerName = "eval-hub/k8s"

// startK8sSpan starts a span for a single Kubernetes API call (Create/Delete
// Job, ConfigMap, Secret; owner-reference and annotation/label patches).
// It uses otel.StartLinkedSpan rather than a plain tracer.Start so that when
// ctx is a runtime's detached background context (see
// internal/otel.DetachedContext), the resulting span still links back to the
// HTTP request trace that triggered the async job/benchmark execution,
// instead of starting an orphaned trace with no relation to it. When ctx is
// an ordinary request-scoped context, this behaves like a normal child span.
func startK8sSpan(ctx context.Context, operation, resourceKind, namespace, name string) (context.Context, trace.Span) {
	return otel.StartLinkedSpan(ctx, k8sTracerName, "k8s."+operation,
		trace.WithAttributes(
			semconv.K8SNamespaceName(namespace),
			attribute.String("k8s.resource.kind", resourceKind),
			attribute.String("k8s.resource.name", name),
		),
	)
}

// endK8sSpan records err (if any) on span and ends it.
func endK8sSpan(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else {
		span.SetStatus(codes.Ok, "")
	}
	span.End()
}
