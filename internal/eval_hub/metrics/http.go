package metrics

import (
	"context"
	"net/http"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/semconv/v1.39.0/httpconv"
)

var (
	serverActiveRequests httpconv.ServerActiveRequests
	serverRequestCount   metric.Int64Counter
)

func initHTTPMetrics(meter metric.Meter) error {
	var err error
	serverActiveRequests, err = httpconv.NewServerActiveRequests(meter)
	if err != nil {
		return err
	}

	serverRequestCount, err = meter.Int64Counter(
		"http.server.request.count",
		metric.WithDescription("Number of HTTP server requests completed"),
		metric.WithUnit("{request}"),
	)
	return err
}

func requestScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

// IncHTTPServerActiveRequests increments the semconv http.server.active_requests gauge.
func IncHTTPServerActiveRequests(ctx context.Context, r *http.Request) {
	if serverRequestCount == nil {
		return
	}
	serverActiveRequests.Add(ctx, 1, httpconv.RequestMethodAttr(r.Method), requestScheme(r))
}

// DecHTTPServerActiveRequests decrements the semconv http.server.active_requests gauge.
func DecHTTPServerActiveRequests(ctx context.Context, r *http.Request) {
	if serverRequestCount == nil {
		return
	}
	serverActiveRequests.Add(ctx, -1, httpconv.RequestMethodAttr(r.Method), requestScheme(r))
}

// RecordHTTPServerRequest increments the request count for a completed HTTP request.
func RecordHTTPServerRequest(ctx context.Context, method, route string, statusCode int) {
	if serverRequestCount == nil {
		return
	}
	if route == "" {
		route = "not_found"
	}
	serverRequestCount.Add(ctx, 1, metric.WithAttributes(
		attribute.String("http.request.method", method),
		attribute.String("http.route", route),
		attribute.Int("http.response.status_code", statusCode),
	))
}
