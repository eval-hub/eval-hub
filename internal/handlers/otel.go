package handlers

import (
	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"github.com/eval-hub/eval-hub/internal/otel"
)

func (h *Handlers) withSpan(ctx *executioncontext.ExecutionContext, fn otel.SpanFunction, component string, operation string, atts ...string) error {
	attributes := make(map[string]string)
	for i := 0; i < len(atts); i += 2 {
		if i+1 >= len(atts) {
			attributes[atts[i]] = ""
		} else {
			attributes[atts[i]] = atts[i+1]
		}
	}
	return otel.WithSpan(
		ctx.Ctx,
		h.serviceConfig,
		ctx.Logger,
		component,
		operation,
		attributes,
		fn,
	)
}
