package server

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/eval-hub/eval-hub/internal/eval_hub/metrics"
)

// Middleware wraps an http.Handler to collect OTEL HTTP request metrics.
func Middleware(next http.Handler, metricsEnabled bool, logger *slog.Logger) http.Handler {
	handler := next
	if metricsEnabled {
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			metrics.IncHTTPInFlight(r.Context())
			defer metrics.DecHTTPInFlight(r.Context())

			rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(rw, r)

			endpoint := r.Pattern
			if endpoint == "" {
				endpoint = "not_found"
			}
			metrics.RecordHTTPRequest(r.Context(), r.Method, endpoint, strconv.Itoa(rw.statusCode))
		})
		logger.Info("Enabled OTEL HTTP metrics middleware")
	}

	return handler
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}
