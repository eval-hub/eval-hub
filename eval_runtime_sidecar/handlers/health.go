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
	Build     string    `json:"build,omitempty"`
	BuildDate string    `json:"build_date,omitempty"`
}

func (h *Handlers) HandleHealth(ctx *executioncontext.ExecutionContext, r http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper, build string, buildDate string) {
	if build == "0.0.1" {
		// for now we only want a real build number and not the default value
		build = ""
	}
	// for now we serialize on each call but we could add
	// a struct to store the health information and only
	// serialize it when something changes
	healthInfo := HealthResponse{
		Status:    STATUS_HEALTHY,
		Timestamp: time.Now().UTC(),
		Build:     build,
		BuildDate: buildDate,
	}
	w.WriteJSON(healthInfo, 200)
}
