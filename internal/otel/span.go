package otel

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/eval-hub/eval-hub/internal/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type SpanFunction func(context.Context) error

func WithSpan(ctx context.Context, serviceConfig *config.Config, logger *slog.Logger, component string, operation string, attributes map[string]string, fn SpanFunction) error {
	runtimeCtx := ctx
	var runtimeSpan trace.Span

	if serviceConfig != nil && serviceConfig.IsOTELEnabled() {
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
	}

	return err
}
