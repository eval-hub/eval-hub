package handlers

import (
	"time"

	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"github.com/eval-hub/eval-hub/internal/http_wrappers"
)

func (h *Handlers) HandleStatus(ctx *executioncontext.ExecutionContext, w http_wrappers.ResponseWrapper) {

	w.WriteJSON(map[string]interface{}{
		"service":   "eval-hub",
		"version":   "1.0.0",
		"status":    "running",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}, 200)

}
