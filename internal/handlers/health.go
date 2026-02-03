package handlers

import (
	"time"

	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"github.com/eval-hub/eval-hub/internal/http_wrappers"
)

func (h *Handlers) HandleHealth(ctx *executioncontext.ExecutionContext, r http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {

	version := "unknown"
	if h.serviceConfig != nil {
		version = h.serviceConfig.Service.Version
	}

	w.WriteJSON(map[string]interface{}{
		"status":    "running",
		"version":   version,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}, 200)
}
