package handlers

import (
	"time"

	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"github.com/eval-hub/eval-hub/internal/http_wrappers"
)

const (
	STATUS_HEALTHY = "healthy"
)

type HealthResponse struct {
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
}

func (h *Handlers) HandleHealth(ctx *executioncontext.ExecutionContext, r http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {
	// for now we serialize on each call but we could add
	// a struct to store the health information and only
	// serialize it when something changes
	healthInfo := HealthResponse{
		Status:    STATUS_HEALTHY,
		Timestamp: time.Now().UTC(),
	}
	w.WriteJSON(healthInfo, 200)
}
