package otel

import (
	"context"
	"errors"
	"log/slog"

	"go.opentelemetry.io/contrib/bridges/otelslog"
)

// BridgeSlogToOTEL wraps base so each log record is written to both its original
// handler (for example zap/stdout) and the global OTEL LoggerProvider via otelslog.
// Returns base unchanged when base is nil. Call only after SetupOTEL has registered
// a LoggerProvider (otel.enable_logs).
func BridgeSlogToOTEL(base *slog.Logger) *slog.Logger {
	if base == nil {
		return base
	}
	otelHandler := otelslog.NewHandler(ServiceName)
	return slog.New(newTeeHandler(base.Handler(), otelHandler))
}

// teeHandler fans out slog records to multiple handlers without mutating the original record.
type teeHandler struct {
	handlers []slog.Handler
}

// newTeeHandler builds a handler that dispatches each record to every child handler.
func newTeeHandler(handlers ...slog.Handler) *teeHandler {
	return &teeHandler{handlers: handlers}
}

// Enabled reports whether any child handler would emit at the given level.
func (h *teeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

// Handle clones the record for each enabled child handler and joins any errors returned.
func (h *teeHandler) Handle(ctx context.Context, record slog.Record) error {
	var errs []error
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, record.Level) {
			if err := handler.Handle(ctx, record.Clone()); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

// WithAttrs returns a tee handler whose children each received the given attributes.
func (h *teeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		next[i] = handler.WithAttrs(attrs)
	}
	return newTeeHandler(next...)
}

// WithGroup returns a tee handler whose children each entered the named group.
func (h *teeHandler) WithGroup(name string) slog.Handler {
	next := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		next[i] = handler.WithGroup(name)
	}
	return newTeeHandler(next...)
}
