package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"github.com/eval-hub/eval-hub/internal/logging"
	"github.com/go-playground/validator/v10"
)

type Handlers struct {
	storage  abstractions.Storage
	validate *validator.Validate
}

func New(storage abstractions.Storage, validate *validator.Validate) *Handlers {
	return &Handlers{
		storage:  storage,
		validate: validate,
	}
}

func (h *Handlers) checkMethod(ctx *executioncontext.ExecutionContext, method string, w http.ResponseWriter) bool {
	if ctx.Request.Method() != method {
		http.Error(w, fmt.Sprintf("Method %s not allowed, expecting %s", ctx.Request.Method(), method), http.StatusMethodNotAllowed)
		return false
	}
	return true
}

func (h *Handlers) getErrorMessage(ctx *executioncontext.ExecutionContext, errorMessage string, code int) string {
	return fmt.Sprintf(`{"error":"%s","code":%d,"trace":"%s"}`, errorMessage, code, ctx.RequestID)
}

func (h *Handlers) setApplicationJSON(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
}

func (h *Handlers) serializationError(ctx *executioncontext.ExecutionContext, w http.ResponseWriter, err error, code int) {
	// we might want to check the error type and create a more meaningful error message
	msg := err.Error()
	h.errorResponse(ctx, w, msg, code)
}

func (h *Handlers) errorResponse(ctx *executioncontext.ExecutionContext, w http.ResponseWriter, errorMessage string, code int) {
	// copied from http.Error but changed because we want to return a JSON error message
	header := w.Header()

	// Delete the Content-Length header, which might be for some other content.
	// Assuming the error string fits in the writer's buffer, we'll figure
	// out the correct Content-Length for it later.
	//
	// We don't delete Content-Encoding, because some middleware sets
	// Content-Encoding: gzip and wraps the ResponseWriter to compress on-the-fly.
	// See https://go.dev/issue/66343.
	header.Del("Content-Length")

	// There might be content type already set, but we reset it to
	// text/plain for the error message.
	h.setApplicationJSON(w)
	header.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	fmt.Fprintln(w, h.getErrorMessage(ctx, errorMessage, code))

	logging.LogRequestFailed(ctx, code, errorMessage)
}

func (h *Handlers) successResponse(ctx *executioncontext.ExecutionContext, w http.ResponseWriter, response any, code int) {
	jsonBytes, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		h.errorResponse(ctx, w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.setApplicationJSON(w)
	w.WriteHeader(code)
	w.Write(jsonBytes)

	logging.LogRequestSuccess(ctx, code, response)
}
