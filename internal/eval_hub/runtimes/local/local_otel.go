package local

import (
	"context"

	"github.com/eval-hub/eval-hub/internal/otel"
	"github.com/eval-hub/eval-hub/pkg/api"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// localTracerName scopes spans emitted by the local runtime's subprocess
// lifecycle, matching the OTEL.md "Related files" convention of one tracer
// name per component.
const localTracerName = "eval-hub/local-runtime"

// startBenchmarkSpan starts a span covering a single benchmark's subprocess
// lifecycle (job-spec write, process start, wait, cleanup — see
// runBenchmark). It uses otel.StartLinkedSpan rather than a plain
// tracer.Start so that when ctx is the runtime's detached background context
// (see internal/otel.DetachedContext, set via WithContext in
// executeEvaluationJob), the resulting span still links back to the HTTP
// request trace that triggered the job, instead of starting an orphaned
// trace with no relation to it.
func startBenchmarkSpan(ctx context.Context, bench api.EvaluationBenchmarkConfig, benchmarkIndex int) (context.Context, trace.Span) {
	return otel.StartLinkedSpan(ctx, localTracerName, "local.run_benchmark",
		trace.WithAttributes(
			attribute.String("benchmark.id", bench.ID),
			attribute.String("provider.id", bench.ProviderID),
			attribute.Int("benchmark.index", benchmarkIndex),
		),
	)
}

// endBenchmarkSpan records err (if any) on span and ends it.
func endBenchmarkSpan(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else {
		span.SetStatus(codes.Ok, "")
	}
	span.End()
}
