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
//   - Config: The service configuration
//   - Evaluation-specific state: model info, timeouts, retries, metadata
type ExecutionContext struct {
	Ctx             context.Context
	RequestID       string
	Logger          *slog.Logger
	StartedAt       time.Time
	MLflowClient    interface{}
	ProviderConfigs map[string]api.ProviderResource
}

func NewExecutionContext(
	ctx context.Context,
	requestID string,
	logger *slog.Logger,
	timeout time.Duration,
	mlflowClient interface{},
	providerConfigs map[string]api.ProviderResource,
) *ExecutionContext {
	return &ExecutionContext{
		Ctx:             ctx,
		RequestID:       requestID,
		Logger:          logger,
		StartedAt:       time.Now(),
		MLflowClient:    mlflowClient,
		ProviderConfigs: providerConfigs,
	}
}
