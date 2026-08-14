package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/constants"
	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/httpwrappers"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/internal/logging"
	"github.com/eval-hub/eval-hub/pkg/api"
)

const (
	TransactionIDHeader = "X-Global-Transaction-Id"
	UserHeader          = "X-User"
	TenantHeader        = "X-Tenant"
)

// newExecutionContext creates a new ExecutionContext with default values. This function
// is called at the route level before invoking evaluation-related handlers to set up
// request-scoped context.
//
// Identity headers: in cluster mode kube-rbac-proxy sets X-Tenant and X-User (required).
// Local mode (--local) does not require these headers.
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

	user := r.Header.Get(UserHeader)
	tenant := r.Header.Get(TenantHeader)

	// add the tenant and user to the logger
	if tenant != "" {
		enhancedLogger = enhancedLogger.With(constants.LogTenant, tenant)
	}
	if user != "" {
		enhancedLogger = enhancedLogger.With(constants.LogUser, user)
	}

	// Use r.Context() so OTEL trace context (and the HTTP span from otelhttp) propagates
	// to handlers and downstream calls (storage, runtime, mlflow). Using context.Background()
	// would break parent-span linkage and create orphan traces.
	return executioncontext.NewExecutionContext(
		r.Context(),
		requestID,
		enhancedLogger,
		api.User(user),
		api.Tenant(tenant))
}

// Abstract request objects to not depend on the underlying HTTP framework.
type ReqWrapper struct {
	Request *http.Request
}

// NewRequestWrapper wraps the request. When maxBodyBytes is >= 0, the body is limited with
// [http.MaxBytesReader]. Pass -1 for maxBodyBytes to disable the limit.
func NewRequestWrapper(w http.ResponseWriter, req *http.Request, maxBodyBytes int64) httpwrappers.RequestWrapper {
	if maxBodyBytes >= 0 {
		req.Body = http.MaxBytesReader(w, req.Body, maxBodyBytes)
	}
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
	values, found := r.Request.URL.Query()[key]
	if found {
		return values
	}
	return []string{}
}

func (r *ReqWrapper) Header(key string) string {
	return r.Request.Header.Get(key)
}

func (r *ReqWrapper) BodyAsBytes() ([]byte, error) {
	bodyBytes, err := io.ReadAll(r.Request.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return nil, serviceerrors.NewServiceError(messages.RequestBodyTooLarge, "Limit", maxErr.Limit)
		}
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

func (r RespWrapper) WriteJSON(v any, code int, arguments ...any) {
	r.SetHeader("Content-Type", "application/json")
	if r.ctx.RequestID != "" {
		r.SetHeader(TransactionIDHeader, r.ctx.RequestID)
	}
	r.SetStatusCode(code)

	if v != nil {
		err := json.NewEncoder(r.Response).Encode(v)
		if err != nil {
			logging.LogRequestFailed(r.ctx, code, err.Error(), 1)
			return
		}
	}
	// Copy variadic args before re-expansion so key/value pairs are not dropped when
	// forwarding ...any into LogRequestSuccess (see handlers TestHandleListEvaluations_WriteJSON_logsExtraArgs).
	logging.LogRequestSuccess(r.ctx, code, v, append([]any(nil), arguments...)...)
}

func (r RespWrapper) SetStatusCode(code int) {
	r.Response.WriteHeader(code)
}

func (r RespWrapper) errorWithMessageCode(requestID string, messageCode *messages.MessageCode, messageParams ...any) {
	msg := messages.GetErrorMessage(messageCode, messageParams...)

	r.DeleteHeader("Content-Length")

	r.SetHeader("X-Content-Type-Options", "nosniff")
	r.WriteJSON(api.Error{Message: msg, MessageCode: messageCode.GetCode(), Trace: requestID}, messageCode.GetStatusCode())

	logging.LogRequestFailed(r.ctx, messageCode.GetStatusCode(), msg, 2)
}

func (r RespWrapper) ErrorWithMessageCode(requestID string, messageCode *messages.MessageCode, messageParams ...any) {
	r.errorWithMessageCode(requestID, messageCode, messageParams...)
}

func (r RespWrapper) Error(err error, requestID string) {
	if e, ok := err.(abstractions.ServiceError); ok {
		r.errorWithMessageCode(requestID, e.MessageCode(), e.MessageParams()...)
		return
	}
	r.errorWithMessageCode(requestID, messages.UnknownError, "Error", err.Error())
}
