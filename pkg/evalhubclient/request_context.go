package evalhubclient

import (
	"context"
	"log/slog"
)

// TransactionIDHeader is the HTTP header used to correlate requests across services.
const TransactionIDHeader = "X-Global-Transaction-Id"

type requestIDContextKey struct{}

type loggerContextKey struct{}

// ContextWithRequestID returns a copy of ctx that carries requestID for outbound requests.
func ContextWithRequestID(ctx context.Context, requestID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if requestID == "" {
		return ctx
	}
	return context.WithValue(ctx, requestIDContextKey{}, requestID)
}

// RequestIDFromContext returns the request ID stored by ContextWithRequestID.
func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(requestIDContextKey{}).(string); ok {
		return id
	}
	return ""
}

// ContextWithLogger returns a copy of ctx that carries logger for eval-hub client logging.
func ContextWithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if logger == nil {
		return ctx
	}
	return context.WithValue(ctx, loggerContextKey{}, logger)
}

// LoggerFromContext returns the logger stored by ContextWithLogger.
func LoggerFromContext(ctx context.Context) *slog.Logger {
	if ctx == nil {
		return nil
	}
	if log, ok := ctx.Value(loggerContextKey{}).(*slog.Logger); ok {
		return log
	}
	return nil
}
