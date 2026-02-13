package server

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/eval-hub/eval-hub/internal/metrics"
)

// Middleware wraps an http.Handler to collect Prometheus metrics
func Middleware(next http.Handler, prometheusMetrics bool, logger *slog.Logger) http.Handler {
	handler := next
	if prometheusMetrics {
		// this should really be in a prometheus package but it uses the http.Handler interface
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Track in-flight requests
			metrics.HTTPRequestInFlight.Inc()
			defer metrics.HTTPRequestInFlight.Dec()

			// Create a response writer wrapper to capture status code
			rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			// Call the next handler
			next.ServeHTTP(rw, r)

			// Calculate duration
			duration := time.Since(start).Seconds()

			// Extract method and endpoint
			method := r.Method
			endpoint := r.URL.Path
			status := strconv.Itoa(rw.statusCode)

			// Record metrics
			metrics.HTTPRequestDuration.WithLabelValues(method, endpoint, status).Observe(duration)
			metrics.HTTPRequestTotal.WithLabelValues(method, endpoint, status).Inc()
		})
		logger.Info("Enabled Prometheus metrics middleware")
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
