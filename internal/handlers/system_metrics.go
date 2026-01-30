package handlers

import (
	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"github.com/eval-hub/eval-hub/internal/http_wrappers"
)

// HandleGetSystemMetrics handles GET /api/v1/metrics/system
func (h *Handlers) HandleGetSystemMetrics(ctx *executioncontext.ExecutionContext, w http_wrappers.ResponseWrapper) {
	w.Error("Not implemented", 501, ctx.RequestID)
}
