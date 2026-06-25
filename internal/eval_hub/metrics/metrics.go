package metrics

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const instrumentationScope = "github.com/eval-hub/eval-hub/internal/eval_hub/metrics"

var (
	requestTotal     metric.Int64Counter
	requestsInFlight metric.Int64UpDownCounter
)

// Init creates OTEL HTTP request instruments. Call once after otel.SetupOTEL configures the global MeterProvider.
func Init() error {
	meter := otel.Meter(instrumentationScope)

	var err error
	requestTotal, err = meter.Int64Counter(
		"http_requests_total",
		metric.WithDescription("Total number of HTTP requests"),
	)
	if err != nil {
		return err
	}

	requestsInFlight, err = meter.Int64UpDownCounter(
		"http_requests_in_flight",
		metric.WithDescription("Number of HTTP requests currently being processed"),
	)
	return err
}

// RecordHTTPRequest increments the request counter for a completed HTTP request.
func RecordHTTPRequest(ctx context.Context, method, endpoint, status string) {
	if requestTotal == nil {
		return
	}
	requestTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("method", method),
		attribute.String("endpoint", endpoint),
		attribute.String("status", status),
	))
}

// IncHTTPInFlight increments the in-flight request gauge.
func IncHTTPInFlight(ctx context.Context) {
	if requestsInFlight != nil {
		requestsInFlight.Add(ctx, 1)
	}
}

// DecHTTPInFlight decrements the in-flight request gauge.
func DecHTTPInFlight(ctx context.Context) {
	if requestsInFlight != nil {
		requestsInFlight.Add(ctx, -1)
	}
}
