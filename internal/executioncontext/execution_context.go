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
	ProviderConfigs map[string]*api.ProviderResource
	Request         Request
}

func NewExecutionContext(
	ctx context.Context,
	requestID string,
	logger *slog.Logger,
	timeout time.Duration,
	mlflowClient interface{},
	providerConfigs map[string]*api.ProviderResource,
	request Request,
) *ExecutionContext {
	return &ExecutionContext{
		Ctx:             ctx,
		RequestID:       requestID,
		Logger:          logger,
		StartedAt:       time.Now(),
		MLflowClient:    mlflowClient,
		Request:         request,
		ProviderConfigs: providerConfigs,
	}
}

// func (ctx *ExecutionContext) GetHeader(key string) string {
// 	if (ctx.headers != nil) && (ctx.headers[key] != nil) && len(ctx.headers[key]) > 0 {
// 		return ctx.headers[key][0]
// 	}
// 	return ""
// }

// func (ctx *ExecutionContext) SetHeader(key string, value string) {
// 	if ctx.headers == nil {
// 		ctx.headers = make(map[string][]string)
// 	}
// 	ctx.headers[key] = []string{value}
// }

// func (ctx *ExecutionContext) GetBody() io.ReadCloser {
// 	return ctx.body
// }

// func (ctx *ExecutionContext) GetBodyAsBytes() ([]byte, error) {
// 	// we could save the body bytes if we need to read this multiple times
// 	// but for now we just allow a single read
// 	if ctx.bodyBytesRead {
// 		return nil, fmt.Errorf("body bytes already read")
// 	}
// 	bodyBytes, err := io.ReadAll(ctx.body)
// 	if err != nil {
// 		return nil, err
// 	}
// 	ctx.bodyBytesRead = true
// 	return bodyBytes, nil
// }

type Request interface {
	Method() string
	URI() string
	Header(key string) string
	SetHeader(key string, value string)
	Path() string
	Query(key string) map[string][]string
	BodyAsBytes() ([]byte, error)
}
