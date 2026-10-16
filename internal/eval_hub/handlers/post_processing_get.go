package handlers

import (
	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/httpwrappers"
	"github.com/eval-hub/eval-hub/internal/eval_hub/workloads"
)

// HandleGetPostProcessing retrieves a standalone post-processing resource.
func (h *Handlers) HandleGetPostProcessing(ctx *executioncontext.ExecutionContext, req httpwrappers.RequestWrapper, w httpwrappers.ResponseWrapper) {
	h.HandleGetJob(ctx, req, w, workloads.PostProcessing)
}
