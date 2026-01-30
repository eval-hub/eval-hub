package handlers

import (
	"time"

	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"github.com/eval-hub/eval-hub/internal/http_wrappers"
)

func (h *Handlers) HandleHealth(ctx *executioncontext.ExecutionContext, w http_wrappers.ResponseWrapper) {

	w.WriteJSON(map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}, 200)

}
