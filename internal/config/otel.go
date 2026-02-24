package config

import (
	"crypto/tls"
	"time"
)

type OTELConfig struct {
	Enabled bool `mapstructure:"enabled"`
	// ExporterType defines the exporter to use: "otlp-grpc", "otlp-http", or "stdout"
	ExporterType string `mapstructure:"exporter_type,omitempty"`
	// ExporterEndpoint is the endpoint for the OTLP exporter (e.g., "localhost:4317" for gRPC)
	ExporterEndpoint string `mapstructure:"exporter_endpoint,omitempty"`
	// ExporterInsecure determines whether to use insecure connection for OTLP exporter
	ExporterInsecure bool `mapstructure:"exporter_insecure,omitempty"`
	// SamplingRatio is the ratio of traces to sample (0.0 to 1.0) - defaults to 1.0 if not set
	SamplingRatio *float64 `mapstructure:"sampling_ratio,omitempty"`
	// Used to enable tracing
	EnableTracing bool `mapstructure:"enable_tracing,omitempty"`
	// TracerTimeout is the timeout for the tracer - defaults to 30 seconds if not set
	TracerTimeout time.Duration `mapstructure:"tracer_timeout,omitempty"`
	// TracerBatchInterval is the interval for the tracer batch - defaults to 5 seconds if not set
	TracerBatchInterval time.Duration `mapstructure:"tracer_batch_interval,omitempty"`
	// Used to enable metrics
	EnableMetrics bool `mapstructure:"enable_metrics,omitempty"`
	// Used to enable sending of logs
	EnableLogs bool `mapstructure:"enable_logs,omitempty"`
	// AdditionalAttributes are custom attributes to add to all traces
	AdditionalAttributes map[string]string `mapstructure:"additional_attributes,omitempty"`
	// The TLS config if running securely (that is not loaded from the config)
	TLSConfig *tls.Config
}
