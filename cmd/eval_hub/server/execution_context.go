package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"github.com/eval-hub/eval-hub/internal/http_wrappers"
	"github.com/eval-hub/eval-hub/internal/logging"
	"github.com/eval-hub/eval-hub/internal/messages"
	"github.com/eval-hub/eval-hub/pkg/api"
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
		3)
}

// Abstract request objects to not depende on the underlying http framework.
type ReqWrapper struct {
	Request *http.Request
}

func NewRequestWrapper(req *http.Request) http_wrappers.RequestWrapper {
	return &ReqWrapper{
		Request: req,
	}
}

func (r *ReqWrapper) Method() string {
	return r.Request.Method
}

func (r *ReqWrapper) URI() string {
	return r.Request.URL.String()
}

func (r *ReqWrapper) Path() string {
	return r.Request.URL.Path
}

func (r *ReqWrapper) Query(key string) []string {
	return r.Request.URL.Query()[key]
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

func (r *ReqWrapper) PathValue(name string) string {
	return r.Request.PathValue(name)
}

type RespWrapper struct {
	Response http.ResponseWriter
	ctx      *executioncontext.ExecutionContext
}

func NewRespWrapper(response http.ResponseWriter, ctx *executioncontext.ExecutionContext) RespWrapper {
	return RespWrapper{
		Response: response,
		ctx:      ctx,
	}
}

func (r RespWrapper) SetHeader(key string, value string) {
	r.Response.Header().Set(key, value)
}

func (r RespWrapper) DeleteHeader(key string) {
	r.Response.Header().Del(key)
}

func (r RespWrapper) Write(buf []byte) (int, error) {
	return r.Response.Write(buf)
}

func (r RespWrapper) WriteJSON(v any, code int) {
	r.SetHeader("Content-Type", "application/json")
	r.SetStatusCode(code)

	if v != nil {
		err := json.NewEncoder(r.Response).Encode(v)
		if err != nil {
			logging.LogRequestFailed(r.ctx, code, err.Error())
			return
		}
	}
	logging.LogRequestSuccess(r.ctx, code, v)
}

func (r RespWrapper) SetStatusCode(code int) {
	r.Response.WriteHeader(code)
}

func (r RespWrapper) ErrorWithMessageCode(requestId string, messageCode *messages.MessageCode, messageParams ...any) {
	msg := messages.GetErrorMesssage(messageCode, messageParams...)

	r.DeleteHeader("Content-Length")

	r.SetHeader("X-Content-Type-Options", "nosniff")
	r.WriteJSON(api.Error{Message: msg, Code: messageCode.GetCode(), Trace: requestId}, messageCode.GetCode())

	logging.LogRequestFailed(r.ctx, messageCode.GetCode(), msg)
}

func (r RespWrapper) Error(err error, requestId string) {
	if e, ok := err.(abstractions.ServiceError); ok {
		r.ErrorWithMessageCode(requestId, e.MessageCode(), e.MessageParams()...)
		return
	}
	r.ErrorWithMessageCode(requestId, messages.UnknownError, "Error", err.Error())
}
