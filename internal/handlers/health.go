package handlers

import (
	"context"
	"log/slog"
	"time"

	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"github.com/eval-hub/eval-hub/internal/http_wrappers"
	"github.com/eval-hub/eval-hub/internal/logging"
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
	// We do not want to flood logs with health checks from readiness and liveness probes,
	// so all health checks are set to log at debug level. The logger is overridden with this
	// at the start of HandleHealth, by setting the log level in the ExecutionContext.
	ctx.Ctx = context.WithValue(ctx.Ctx, logging.LogLevelKey, slog.LevelDebug)
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
