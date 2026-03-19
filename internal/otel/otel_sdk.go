package otel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/eval-hub/eval-hub/internal/config"
	"github.com/go-logr/logr"
	"go.opentelemetry.io/contrib/detectors/aws/ecs"
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
		logger.Info("OTEL is not enabled, skipping setup")
		return nil, nil
	}

	if !config.DoNotRedirectOTELLogs {
		// have the OTEL SDK send its logs to our logger
		lr := logr.FromSlogHandler(logger.Handler())
		otel.SetLogger(lr)
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
		tracerProvider, err := newTracerProvider(ctx, config, logger)
		if err != nil {
			handleErr(err)
			return shutdown, err
		}
		shutdownFuncs = append(shutdownFuncs, tracerProvider.Shutdown)
		otel.SetTracerProvider(tracerProvider)
		logger.Info("OTEL tracer provider created", "exporter_type", config.ExporterType, "exporter_endpoint", safeURL(config.ExporterEndpoint), "exporter_insecure", config.ExporterInsecure)
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
		logger.Info("OTEL meter provider created")
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
		logger.Info("OTEL logger provider created")
	}

	if err != nil {
		logger.Error("Failed to setup OTEL", "error", err.Error())
	} else {
		logger.Info("OTEL setup complete")
	}

	return shutdown, err
}

func newPropagator() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
}

func newTracerProvider(ctx context.Context, config *config.OTELConfig, logger *slog.Logger) (*trace.TracerProvider, error) {
	// set default values for tracer timeout and batch interval
	tracerTimeout := config.TracerTimeout
	if tracerTimeout == 0 {
		tracerTimeout = 30 * time.Second
	}
	tracerBatchInterval := config.TracerBatchInterval
	if tracerBatchInterval == 0 {
		tracerBatchInterval = 5 * time.Second
	}
	samplingRatio := float64(1.0)
	if config.SamplingRatio != nil {
		samplingRatio = *config.SamplingRatio
	}

	switch config.ExporterType {
	case ExporterTypeOTLPGRPC:
		if config.ExporterEndpoint == "" {
			return nil, fmt.Errorf("Exporter endpoint is required for OTEL %s exporter", config.ExporterType)
		}
		opts := []otlptracegrpc.Option{
			otlptracegrpc.WithEndpoint(config.ExporterEndpoint),
			otlptracegrpc.WithTimeout(tracerTimeout),
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
		res, err := createResource(ctx, config, logger)
		if err != nil {
			return nil, err
		}
		tracerProvider := trace.NewTracerProvider(
			trace.WithBatcher(traceExporter, trace.WithBatchTimeout(tracerBatchInterval)),
			trace.WithSampler(newSampler(samplingRatio)),
			trace.WithResource(res),
		)
		return tracerProvider, nil
	case ExporterTypeOTLPHTTP:
		if config.ExporterEndpoint == "" {
			return nil, fmt.Errorf("Exporter endpoint is required for OTEL %s exporter", config.ExporterType)
		}
		opts := []otlptracehttp.Option{
			otlptracehttp.WithEndpoint(config.ExporterEndpoint),
			otlptracehttp.WithTimeout(tracerTimeout),
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
		res, err := createResource(ctx, config, logger)
		if err != nil {
			return nil, err
		}
		tracerProvider := trace.NewTracerProvider(
			trace.WithBatcher(traceExporter, trace.WithBatchTimeout(tracerBatchInterval)),
			trace.WithSampler(newSampler(samplingRatio)),
			trace.WithResource(res),
		)
		return tracerProvider, nil
	case ExporterTypeStdout:
		traceExporter, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
		if err != nil {
			return nil, err
		}
		tracerProvider := trace.NewTracerProvider(
			trace.WithBatcher(traceExporter, trace.WithBatchTimeout(tracerBatchInterval)),
		)
		return tracerProvider, nil
	default:
		return nil, fmt.Errorf("Invalid OTEL exporter type: %s", config.ExporterType)
	}
}

func createResource(ctx context.Context, config *config.OTELConfig, logger *slog.Logger) (*resource.Resource, error) {
	attrs := []attribute.KeyValue{
		semconv.ServiceName(ServiceName),
		// semconv.ServiceVersion(config.ServiceVersion),
	}

	// Add custom attributes
	for key, value := range config.AdditionalAttributes {
		attrs = append(attrs, attribute.String(key, value))
	}

	var res *resource.Resource = resource.Default()

	pr, createErr := createProcessResource(ctx, config)
	if pr != nil {
		if newRes, mergeErr := resource.Merge(res, pr); mergeErr == nil {
			res = newRes
		}
	}
	if createErr != nil {
		logger.Error("Process resource detector failed", "error", createErr)
	}

	return resource.Merge(
		res,
		resource.NewSchemaless(attrs...),
	)
}

func createProcessResource(ctx context.Context, config *config.OTELConfig) (*resource.Resource, error) {
	var opts []resource.Option
	opts = append(opts, resource.WithProcess())
	opts = append(opts, resource.WithOS())
	opts = append(opts, resource.WithHost())
	if config.EnableECSResourceDetection {
		opts = append(opts, resource.WithDetectors(ecs.NewResourceDetector()))
	} else {
		opts = append(opts, resource.WithContainer())
	}
	return resource.New(ctx, opts...)
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

func safeURL(endpoint string) string {
	uri, err := url.Parse(endpoint)
	if err != nil {
		return endpoint
	}
	return uri.Redacted() // this will return the URL with the password redacted
}
