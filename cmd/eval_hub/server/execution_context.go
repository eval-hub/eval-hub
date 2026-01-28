package server

import (
	"context"
	"io"
	"net/http"

	"github.com/eval-hub/eval-hub/internal/executioncontext"
)

// newExecutionContext creates a new ExecutionContext with default values. This function
// is called at the route level before invoking evaluation-related handlers to set up
// request-scoped context.
//
// The function automatically:
//   - Enhances the logger with request-specific fields via logging.LoggerWithRequest
//   - Sets default timeout (60 minutes) and retry attempts (3)
//   - Initializes an empty metadata map
//
// This enables automatic request ID tracking (from X-Global-Transaction-Id header or
// auto-generated UUID) and structured logging with consistent request metadata.
//
// Parameters:
//   - r: The HTTP request to extract context from
//   - logger: The base logger to enhance with request fields
//   - serviceConfig: The service configuration to include in the context
//
// Returns:
//   - *ExecutionContext: A new execution context ready for use in handlers
func (s *Server) newExecutionContext(r *http.Request) *executioncontext.ExecutionContext {
	// Enhance logger with request-specific fields
	requestID, enhancedLogger := s.loggerWithRequest(r)

	return executioncontext.NewExecutionContext(
		context.Background(),
		requestID,
		enhancedLogger,
		3,
		nil,
		s.providerConfigs,
		&ReqWrapper{Request: r},
	)
}

// Abstract request objects to not depende on the underlying http framework.
type ReqWrapper struct {
	Request *http.Request
}

func (r *ReqWrapper) Method() string {
	return r.Request.Method
}

func (r *ReqWrapper) URI() string {
	return r.Request.RequestURI
}

func (r *ReqWrapper) Path() string {
	return r.Request.URL.Path
}

func (r *ReqWrapper) Query(key string) map[string][]string {
	return r.Request.URL.Query()
}

func (r *ReqWrapper) Header(key string) string {
	return r.Request.Header.Get(key)
}

func (r *ReqWrapper) BodyAsBytes() ([]byte, error) {
	bodyBytes, err := io.ReadAll(r.Request.Body)
	if err != nil {
		return nil, err
	}

	return bodyBytes, nil
}

func (r *ReqWrapper) SetHeader(key string, value string) {
	r.Request.Header.Set(key, value)
}
