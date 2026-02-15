package otel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/eval-hub/eval-hub/internal/config"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutlog"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.39.0"
	"google.golang.org/grpc/credentials"
)

const (
	ExporterTypeOTLPGRPC = "otlp-grpc"
	ExporterTypeOTLPHTTP = "otlp-http"
	ExporterTypeStdout   = "stdout"

	ServiceName = "github.com/eval-hub/eval-hub"
	Compressor  = "gzip"
)

// setupOTelSDK bootstraps the OpenTelemetry pipeline.
// If it does not return an error, make sure to call shutdown for proper cleanup.
func SetupOTEL(ctx context.Context, config *config.OTELConfig, logger *slog.Logger) (func(context.Context) error, error) {
	if config == nil || !config.Enabled {
		return nil, nil
	}

	// set any default values
	if config.TracerTimeout == 0 {
		config.TracerTimeout = 30 * time.Second
	}
	if config.TracerBatchInterval == 0 {
		config.TracerBatchInterval = 5 * time.Second
	}

	var shutdownFuncs []func(context.Context) error
	var err error

	// shutdown calls cleanup functions registered via shutdownFuncs.
	// The errors from the calls are joined.
	// Each registered cleanup will be invoked once.
	shutdown := func(ctx context.Context) error {
		var err error
		for _, fn := range shutdownFuncs {
			err = errors.Join(err, fn(ctx))
		}
		shutdownFuncs = nil
		return err
	}

	// handleErr calls shutdown for cleanup and makes sure that all errors are returned.
	handleErr := func(inErr error) {
		err = errors.Join(inErr, shutdown(ctx))
	}

	// Set up propagator.
	prop := newPropagator()
	otel.SetTextMapPropagator(prop)

	// Set up trace provider.
	if config.EnableTracing {
		tracerProvider, err := newTracerProvider(ctx, config)
		if err != nil {
			handleErr(err)
			return shutdown, err
		}
		shutdownFuncs = append(shutdownFuncs, tracerProvider.Shutdown)
		otel.SetTracerProvider(tracerProvider)
	}

	// Set up meter provider.
	if config.EnableMetrics {
		meterProvider, err := newMeterProvider(ctx, config)
		if err != nil {
			handleErr(err)
			return shutdown, err
		}
		shutdownFuncs = append(shutdownFuncs, meterProvider.Shutdown)
		otel.SetMeterProvider(meterProvider)
	}

	// Set up logger provider.
	if config.EnableLogs {
		loggerProvider, err := newLoggerProvider(ctx, config)
		if err != nil {
			handleErr(err)
			return shutdown, err
		}
		shutdownFuncs = append(shutdownFuncs, loggerProvider.Shutdown)
		global.SetLoggerProvider(loggerProvider)
	}

	return shutdown, err
}

func newPropagator() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
}

func newTracerProvider(ctx context.Context, config *config.OTELConfig) (*trace.TracerProvider, error) {
	switch config.ExporterType {
	case ExporterTypeOTLPGRPC:
		if config.ExporterEndpoint == "" {
			return nil, fmt.Errorf("Exporter endpoint is required for OTEL %s exporter", config.ExporterType)
		}
		opts := []otlptracegrpc.Option{
			otlptracegrpc.WithEndpoint(config.ExporterEndpoint),
			otlptracegrpc.WithTimeout(config.TracerTimeout),
			otlptracegrpc.WithCompressor(Compressor),
		}
		if config.ExporterInsecure {
			opts = append(opts, otlptracegrpc.WithInsecure())
		} else if config.TLSConfig != nil {
			opts = append(opts, otlptracegrpc.WithTLSCredentials(credentials.NewTLS(config.TLSConfig)))
		} else {
			return nil, fmt.Errorf("No TLS config provided for secure OTEL %s exporter", config.ExporterType)
		}
		traceExporter, err := otlptracegrpc.New(ctx, opts...)
		if err != nil {
			return nil, err
		}
		res, err := createResource(config)
		if err != nil {
			return nil, err
		}
		tracerProvider := trace.NewTracerProvider(
			trace.WithBatcher(traceExporter, trace.WithBatchTimeout(config.TracerBatchInterval)),
			trace.WithSampler(newSampler(config.SamplingRatio)),
			trace.WithResource(res),
		)
		return tracerProvider, nil
	case ExporterTypeOTLPHTTP:
		if config.ExporterEndpoint == "" {
			return nil, fmt.Errorf("Exporter endpoint is required for OTEL %s exporter", config.ExporterType)
		}
		opts := []otlptracehttp.Option{
			otlptracehttp.WithEndpoint(config.ExporterEndpoint),
			otlptracehttp.WithTimeout(config.TracerTimeout),
		}
		if config.ExporterInsecure {
			opts = append(opts, otlptracehttp.WithInsecure())
		} else if config.TLSConfig != nil {
			opts = append(opts, otlptracehttp.WithTLSClientConfig(config.TLSConfig))
		} else {
			return nil, fmt.Errorf("No TLS config provided for secure OTEL %s exporter", config.ExporterType)
		}
		traceExporter, err := otlptracehttp.New(ctx, opts...)
		if err != nil {
			return nil, err
		}
		res, err := createResource(config)
		if err != nil {
			return nil, err
		}
		tracerProvider := trace.NewTracerProvider(
			trace.WithBatcher(traceExporter, trace.WithBatchTimeout(config.TracerBatchInterval)),
			trace.WithSampler(newSampler(config.SamplingRatio)),
			trace.WithResource(res),
		)
		return tracerProvider, nil
	case ExporterTypeStdout:
		traceExporter, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
		if err != nil {
			return nil, err
		}
		tracerProvider := trace.NewTracerProvider(
			trace.WithBatcher(traceExporter, trace.WithBatchTimeout(config.TracerBatchInterval)),
		)
		return tracerProvider, nil
	default:
		return nil, fmt.Errorf("Invalid OTEL exporter type: %s", config.ExporterType)
	}
}

func createResource(config *config.OTELConfig) (*resource.Resource, error) {
	attrs := []attribute.KeyValue{
		semconv.ServiceName(ServiceName),
		// semconv.ServiceVersion(config.ServiceVersion),
	}

	// Add custom attributes
	for key, value := range config.AdditionalAttributes {
		attrs = append(attrs, attribute.String(key, value))
	}

	return resource.NewWithAttributes(semconv.SchemaURL, attrs...), nil
}

func CreateProcessResource(ctx context.Context) (*resource.Resource, error) {
	return resource.New(ctx,
		resource.WithProcess(),
		resource.WithOS(),
		resource.WithContainer(),
		resource.WithHost(),
	)
}

func newMeterProvider(_ context.Context, _ *config.OTELConfig) (*metric.MeterProvider, error) {
	return nil, fmt.Errorf("Not implemented")
	/* TODO: Implement metric exporter
	metricExporter, err := stdoutmetric.New(stdoutmetric.WithPrettyPrint())
	if err != nil {
		return nil, err
	}

	meterProvider := metric.NewMeterProvider(
		metric.WithReader(metric.NewPeriodicReader(metricExporter,
			// Default is 1m. Set to 3s for demonstrative purposes.
			metric.WithInterval(3*time.Second))),
	)
	return meterProvider, nil
	*/
}

func newLoggerProvider(_ context.Context, _ *config.OTELConfig) (*log.LoggerProvider, error) {
	// TODO: Implement logger exporter for something other than stdout
	logExporter, err := stdoutlog.New(stdoutlog.WithPrettyPrint())
	if err != nil {
		return nil, err
	}

	loggerProvider := log.NewLoggerProvider(
		log.WithProcessor(log.NewBatchProcessor(logExporter)),
	)
	return loggerProvider, nil
}

// newSampler creates a sampler based on the sampling ratio
func newSampler(ratio float64) trace.Sampler {
	if ratio >= 1.0 {
		return trace.AlwaysSample()
	}
	if ratio <= 0.0 {
		return trace.NeverSample()
	}
	return trace.TraceIDRatioBased(ratio)
}

func NewRoundTripper(base http.RoundTripper) http.RoundTripper {
	return otelhttp.NewTransport(base)
}
