package executioncontext

import (
	"context"
	"log/slog"
	"time"

	"github.com/eval-hub/eval-hub/pkg/api"
)

// ExecutionContext contains execution context for API operations. This pattern enables
// type-safe passing of configuration and state to evaluation-related handlers, which
// receive an ExecutionContext instead of a raw http.Request.
//
// The ExecutionContext contains:
//   - Logger: A request-scoped logger with enriched fields (request_id, method, uri, etc.)
//   - Evaluation-specific state: model info, timeouts, retries, metadata
type ExecutionContext struct {
	Ctx       context.Context
	RequestID string
	Logger    *slog.Logger
	StartedAt time.Time
	User      api.User
	Tenant    api.Tenant
}

// This struct contains per request context information
func NewExecutionContext(
	ctx context.Context,
	requestID string,
	logger *slog.Logger,
	timeout time.Duration,
	user api.User,
	tenant api.Tenant,
) *ExecutionContext {
	return &ExecutionContext{
		Ctx:       ctx,
		RequestID: requestID,
		Logger:    logger,
		StartedAt: time.Now(),
		User:      user,
		Tenant:    tenant,
	}
}

func (e *ExecutionContext) WithContext(ctx context.Context) *ExecutionContext {
	return &ExecutionContext{
		Ctx:       ctx,
		RequestID: e.RequestID,
		Logger:    e.Logger,
		StartedAt: e.StartedAt,
		User:      e.User,
		Tenant:    e.Tenant,
	}
}
