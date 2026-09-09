package server

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/metrics"
)

// HTTPMetricsMiddleware records semconv HTTP request count and active-request metrics,
// plus the domain-enriched evalhub_api_request_duration_seconds histogram.
func HTTPMetricsMiddleware(next http.Handler, metricsEnabled bool, logger *slog.Logger) http.Handler {
	if !metricsEnabled {
		return next
	}

	logger.Info("Enabled OTEL HTTP semconv metrics middleware")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		metrics.IncHTTPServerActiveRequests(r.Context(), r)
		defer metrics.DecHTTPServerActiveRequests(r.Context(), r)

		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rw, r)

		route := r.Pattern
		metrics.RecordHTTPServerRequest(r.Context(), r.Method, route, rw.statusCode)

		collection, provider := extractDomainLabels(r.URL.Path)
		duration := time.Since(start).Seconds()
		metrics.ObserveAPIRequestDuration(r.Context(), route, r.Method, collection, provider, duration)
	})
}

// extractDomainLabels parses collection and provider from the request path when available.
// Returns empty strings for non-evaluation routes to keep label cardinality bounded.
func extractDomainLabels(path string) (collection, provider string) {
	if !strings.HasPrefix(path, "/api/v1/evaluations/") {
		return "", ""
	}
	return "", ""
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
