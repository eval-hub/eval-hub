package server

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/eval-hub/eval-hub/internal/eval_hub/constants"
	"github.com/google/uuid"
)

type requestContextKey struct{}

type requestLoggerContextKey struct{}

func requestIDFromRequest(r *http.Request) string {
	requestID := strings.TrimSpace(r.Header.Get(TRANSACTION_ID_HEADER))
	if requestID == "" {
		requestID = uuid.New().String()
	}
	return requestID
}

func withRequestContext(baseLogger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := requestIDFromRequest(r)
		w.Header().Set(TRANSACTION_ID_HEADER, requestID)

		ctx := context.WithValue(r.Context(), requestContextKey{}, requestID)
		if baseLogger != nil {
			ctx = context.WithValue(ctx, requestLoggerContextKey{}, baseLogger.With(constants.LOG_REQUEST_ID, requestID))
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestIDFromContext returns the request ID stored by withRequestContext.
func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(requestContextKey{}).(string); ok {
		return id
	}
	return ""
}

func requestLogger(ctx context.Context, fallback *slog.Logger) *slog.Logger {
	if ctx != nil {
		if log, ok := ctx.Value(requestLoggerContextKey{}).(*slog.Logger); ok && log != nil {
			return log
		}
	}
	return fallback
}
