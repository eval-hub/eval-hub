package server

import (
	"log/slog"
	"net/http"

	"github.com/eval-hub/eval-hub/internal/eval_hub/metrics"
)

// HTTPMetricsMiddleware records semconv HTTP request count and active-request metrics.
// Request duration is collected separately by otelhttp on each route.
func HTTPMetricsMiddleware(next http.Handler, metricsEnabled bool, logger *slog.Logger) http.Handler {
	if !metricsEnabled {
		return next
	}

	logger.Info("Enabled OTEL HTTP semconv metrics middleware")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metrics.IncHTTPServerActiveRequests(r.Context(), r)
		defer metrics.DecHTTPServerActiveRequests(r.Context(), r)

		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rw, r)

		route := r.Pattern
		metrics.RecordHTTPServerRequest(r.Context(), r.Method, route, rw.statusCode)
	})
}

// responseWriter wraps http.ResponseWriter to capture status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}
