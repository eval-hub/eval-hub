package otel

import (
	"context"
	"crypto/tls"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
)

func TestParseMeterExportInterval(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		interval   time.Duration
		wantDur    time.Duration
		wantErrSub string
	}{
		{
			name:    "zero defaults to 60s",
			wantDur: 60 * time.Second,
		},
		{
			name:     "positive duration",
			interval: 30 * time.Second,
			wantDur:  30 * time.Second,
		},
		{
			name:     "positive compound duration",
			interval: 90 * time.Second,
			wantDur:  90 * time.Second,
		},
		{
			name:     "positive duration from milliseconds",
			interval: 30000 * time.Millisecond,
			wantDur:  30 * time.Second,
		},
		{
			name:     "positive small duration",
			interval: 500 * time.Millisecond,
			wantDur:  500 * time.Millisecond,
		},
		{
			name:     "positive sub-millisecond duration",
			interval: 500 * time.Microsecond,
			wantDur:  500 * time.Microsecond,
		},
		{
			interval:   -5 * time.Millisecond,
			wantErrSub: "must be a positive duration",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := &config.OTELConfig{MetricExportInterval: tt.interval}
			dur, err := parseMeterExportInterval(cfg)

			if tt.wantErrSub != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErrSub)
				}
				if !strings.Contains(err.Error(), tt.wantErrSub) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErrSub, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if dur != tt.wantDur {
				t.Fatalf("expected %v, got %v", tt.wantDur, dur)
			}
		})
	}
}

func TestNewMeterProvider(t *testing.T) {
	logger := slog.Default()
	ctx := context.Background()

	tests := []struct {
		name              string
		cfg               *config.OTELConfig
		prometheusEnabled bool
		wantErrSub        string
	}{
		{
			name: "stdout returns provider",
			cfg: &config.OTELConfig{
				Enabled:      true,
				ExporterType: "stdout",
			},
			prometheusEnabled: false,
		},
		{
			name: "stdout with prometheus returns provider",
			cfg: &config.OTELConfig{
				Enabled:      true,
				ExporterType: "stdout",
			},
			prometheusEnabled: true,
		},
		{
			name: "otlp-grpc insecure returns provider",
			cfg: &config.OTELConfig{
				Enabled:          true,
				ExporterType:     "otlp-grpc",
				ExporterEndpoint: "localhost:4317",
				ExporterInsecure: true,
			},
			prometheusEnabled: false,
		},
		{
			name: "otlp-grpc missing endpoint",
			cfg: &config.OTELConfig{
				Enabled:          true,
				ExporterType:     "otlp-grpc",
				ExporterInsecure: true,
			},
			wantErrSub: "Exporter endpoint is required",
		},
		{
			name: "otlp-grpc no TLS config",
			cfg: &config.OTELConfig{
				Enabled:          true,
				ExporterType:     "otlp-grpc",
				ExporterEndpoint: "localhost:4317",
			},
			wantErrSub: "No TLS config provided",
		},
		{
			name: "otlp-grpc with TLS returns provider",
			cfg: &config.OTELConfig{
				Enabled:          true,
				ExporterType:     "otlp-grpc",
				ExporterEndpoint: "localhost:4317",
				TLSConfig:        &tls.Config{},
			},
			prometheusEnabled: false,
		},
		{
			name: "otlp-http insecure returns provider",
			cfg: &config.OTELConfig{
				Enabled:          true,
				ExporterType:     "otlp-http",
				ExporterEndpoint: "localhost:4318",
				ExporterInsecure: true,
			},
			prometheusEnabled: false,
		},
		{
			name: "otlp-http missing endpoint",
			cfg: &config.OTELConfig{
				Enabled:          true,
				ExporterType:     "otlp-http",
				ExporterInsecure: true,
			},
			wantErrSub: "Exporter endpoint is required",
		},
		{
			name: "otlp-http no TLS config",
			cfg: &config.OTELConfig{
				Enabled:          true,
				ExporterType:     "otlp-http",
				ExporterEndpoint: "localhost:4318",
			},
			wantErrSub: "No TLS config provided",
		},
		{
			name: "otlp-http with TLS returns provider",
			cfg: &config.OTELConfig{
				Enabled:          true,
				ExporterType:     "otlp-http",
				ExporterEndpoint: "localhost:4318",
				TLSConfig:        &tls.Config{},
			},
			prometheusEnabled: false,
		},
		{
			name: "invalid exporter type",
			cfg: &config.OTELConfig{
				Enabled:      true,
				ExporterType: "kafka",
			},
			wantErrSub: "Invalid OTEL exporter type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mp, err := newMeterProvider(ctx, tt.cfg, logger, tt.prometheusEnabled)

			if tt.wantErrSub != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErrSub)
				}
				if !strings.Contains(err.Error(), tt.wantErrSub) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErrSub, err.Error())
				}
				if mp != nil {
					t.Fatal("expected nil provider on error")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if mp == nil {
				t.Fatal("expected non-nil MeterProvider")
			}

			// Shutdown may return an error for OTLP exporters when no collector
			// is running — that is expected and not a test failure. We only verify
			// that the provider was created successfully.
			_ = mp.Shutdown(ctx)
		})
	}
}

func TestNewMeterProviderInvalidInterval(t *testing.T) {
	t.Parallel()

	cfg := &config.OTELConfig{
		Enabled:              true,
		ExporterType:         "stdout",
		MetricExportInterval: -1 * time.Second,
	}

	mp, err := newMeterProvider(context.Background(), cfg, slog.Default(), false)
	if err == nil {
		t.Fatal("expected error for invalid interval, got nil")
	}
	if !strings.Contains(err.Error(), "must be a positive duration") {
		t.Fatalf("expected error about positive duration, got %q", err.Error())
	}
	if mp != nil {
		t.Fatal("expected nil provider on error")
	}
}

func TestNewTracerProvider(t *testing.T) {
	logger := slog.Default()
	ctx := context.Background()

	tests := []struct {
		name       string
		cfg        *config.OTELConfig
		wantErrSub string
	}{
		{
			name: "stdout returns provider",
			cfg: &config.OTELConfig{
				Enabled:      true,
				ExporterType: ExporterTypeStdout,
			},
		},
		{
			name: "otlp-grpc insecure returns provider",
			cfg: &config.OTELConfig{
				Enabled:          true,
				ExporterType:     ExporterTypeOTLPGRPC,
				ExporterEndpoint: "localhost:4317",
				ExporterInsecure: true,
			},
		},
		{
			name: "otlp-grpc missing endpoint",
			cfg: &config.OTELConfig{
				Enabled:          true,
				ExporterType:     ExporterTypeOTLPGRPC,
				ExporterInsecure: true,
			},
			wantErrSub: "Exporter endpoint is required",
		},
		{
			name: "otlp-grpc no TLS config",
			cfg: &config.OTELConfig{
				Enabled:          true,
				ExporterType:     ExporterTypeOTLPGRPC,
				ExporterEndpoint: "localhost:4317",
			},
			wantErrSub: "No TLS config provided",
		},
		{
			name: "otlp-grpc with TLS returns provider",
			cfg: &config.OTELConfig{
				Enabled:          true,
				ExporterType:     ExporterTypeOTLPGRPC,
				ExporterEndpoint: "localhost:4317",
				TLSConfig:        &tls.Config{},
			},
		},
		{
			name: "otlp-http insecure returns provider",
			cfg: &config.OTELConfig{
				Enabled:          true,
				ExporterType:     ExporterTypeOTLPHTTP,
				ExporterEndpoint: "localhost:4318",
				ExporterInsecure: true,
			},
		},
		{
			name: "otlp-http missing endpoint",
			cfg: &config.OTELConfig{
				Enabled:          true,
				ExporterType:     ExporterTypeOTLPHTTP,
				ExporterInsecure: true,
			},
			wantErrSub: "Exporter endpoint is required",
		},
		{
			name: "otlp-http no TLS config",
			cfg: &config.OTELConfig{
				Enabled:          true,
				ExporterType:     ExporterTypeOTLPHTTP,
				ExporterEndpoint: "localhost:4318",
			},
			wantErrSub: "No TLS config provided",
		},
		{
			name: "otlp-http with TLS returns provider",
			cfg: &config.OTELConfig{
				Enabled:          true,
				ExporterType:     ExporterTypeOTLPHTTP,
				ExporterEndpoint: "localhost:4318",
				TLSConfig:        &tls.Config{},
			},
		},
		{
			name: "invalid exporter type",
			cfg: &config.OTELConfig{
				Enabled:      true,
				ExporterType: "kafka",
			},
			wantErrSub: "Invalid OTEL exporter type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tp, err := newTracerProvider(ctx, tt.cfg, logger)

			if tt.wantErrSub != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErrSub)
				}
				if !strings.Contains(err.Error(), tt.wantErrSub) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErrSub, err.Error())
				}
				if tp != nil {
					t.Fatal("expected nil provider on error")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tp == nil {
				t.Fatal("expected non-nil TracerProvider")
			}
			_ = tp.Shutdown(ctx)
		})
	}
}

func TestNewLoggerProvider(t *testing.T) {
	logger := slog.Default()
	ctx := context.Background()

	tests := []struct {
		name       string
		cfg        *config.OTELConfig
		wantErrSub string
	}{
		{
			name: "stdout returns provider",
			cfg: &config.OTELConfig{
				Enabled:      true,
				ExporterType: ExporterTypeStdout,
			},
		},
		{
			name: "default exporter type uses stdout",
			cfg: &config.OTELConfig{
				Enabled: true,
			},
		},
		{
			name: "otlp-grpc insecure returns provider",
			cfg: &config.OTELConfig{
				Enabled:          true,
				ExporterType:     ExporterTypeOTLPGRPC,
				ExporterEndpoint: "localhost:4317",
				ExporterInsecure: true,
			},
		},
		{
			name: "otlp-grpc missing endpoint",
			cfg: &config.OTELConfig{
				Enabled:          true,
				ExporterType:     ExporterTypeOTLPGRPC,
				ExporterInsecure: true,
			},
			wantErrSub: "Exporter endpoint is required",
		},
		{
			name: "invalid exporter type",
			cfg: &config.OTELConfig{
				Enabled:      true,
				ExporterType: "kafka",
			},
			wantErrSub: "Invalid OTEL exporter type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lp, err := newLoggerProvider(ctx, tt.cfg, logger)

			if tt.wantErrSub != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErrSub)
				}
				if !strings.Contains(err.Error(), tt.wantErrSub) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErrSub, err.Error())
				}
				if lp != nil {
					t.Fatal("expected nil provider on error")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if lp == nil {
				t.Fatal("expected non-nil LoggerProvider")
			}
			_ = lp.Shutdown(ctx)
		})
	}
}

func TestNewSampler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		ratio float64
		want  string
	}{
		{name: "always sample", ratio: 1.0, want: "AlwaysOnSampler"},
		{name: "above one", ratio: 2.0, want: "AlwaysOnSampler"},
		{name: "never sample", ratio: 0.0, want: "AlwaysOffSampler"},
		{name: "below zero", ratio: -0.5, want: "AlwaysOffSampler"},
		{name: "ratio based", ratio: 0.5, want: "TraceIDRatioBased"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sampler := newSampler(tt.ratio)
			if sampler == nil {
				t.Fatal("expected non-nil sampler")
			}
			if got := sampler.Description(); !strings.Contains(got, tt.want) {
				t.Fatalf("sampler description = %q, want substring %q", got, tt.want)
			}
		})
	}
}

func TestSafeURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		endpoint string
		wantSub  string
	}{
		{
			name:     "redacts password",
			endpoint: "http://user:secret@localhost:4317",
			wantSub:  "xxxxx",
		},
		{
			name:     "plain endpoint unchanged",
			endpoint: "localhost:4317",
			wantSub:  "localhost:4317",
		},
		{
			name:     "invalid url returned as-is",
			endpoint: "://bad",
			wantSub:  "://bad",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := safeURL(tt.endpoint)
			if !strings.Contains(got, tt.wantSub) {
				t.Fatalf("safeURL(%q) = %q, want substring %q", tt.endpoint, got, tt.wantSub)
			}
			if tt.name == "redacts password" && strings.Contains(got, "secret") {
				t.Fatalf("password not redacted: %q", got)
			}
		})
	}
}

func TestSetupOTEL(t *testing.T) {
	logger := slog.Default()
	ctx := context.Background()

	t.Run("nil config skips setup", func(t *testing.T) {
		shutdown, err := SetupOTEL(ctx, nil, logger, false)
		if err != nil {
			t.Fatalf("SetupOTEL: %v", err)
		}
		if shutdown != nil {
			t.Fatal("expected nil shutdown func")
		}
	})

	t.Run("disabled config skips setup", func(t *testing.T) {
		shutdown, err := SetupOTEL(ctx, &config.OTELConfig{Enabled: false}, logger, false)
		if err != nil {
			t.Fatalf("SetupOTEL: %v", err)
		}
		if shutdown != nil {
			t.Fatal("expected nil shutdown func")
		}
	})

	t.Run("tracing stdout", func(t *testing.T) {
		shutdown, err := SetupOTEL(ctx, &config.OTELConfig{
			Enabled:       true,
			EnableTracing: true,
			ExporterType:  ExporterTypeStdout,
		}, logger, false)
		if err != nil {
			t.Fatalf("SetupOTEL: %v", err)
		}
		if shutdown == nil {
			t.Fatal("expected shutdown func")
		}
		if err := shutdown(ctx); err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	})

	t.Run("metrics stdout", func(t *testing.T) {
		shutdown, err := SetupOTEL(ctx, &config.OTELConfig{
			Enabled:       true,
			EnableMetrics: true,
			ExporterType:  ExporterTypeStdout,
		}, logger, false)
		if err != nil {
			t.Fatalf("SetupOTEL: %v", err)
		}
		if shutdown == nil {
			t.Fatal("expected shutdown func")
		}
		if err := shutdown(ctx); err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	})

	t.Run("logs stdout", func(t *testing.T) {
		shutdown, err := SetupOTEL(ctx, &config.OTELConfig{
			Enabled:    true,
			EnableLogs: true,
		}, logger, false)
		if err != nil {
			t.Fatalf("SetupOTEL: %v", err)
		}
		if shutdown == nil {
			t.Fatal("expected shutdown func")
		}
		if err := shutdown(ctx); err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	})

	t.Run("tracing error returns shutdown cleanup", func(t *testing.T) {
		shutdown, err := SetupOTEL(ctx, &config.OTELConfig{
			Enabled:       true,
			EnableTracing: true,
			ExporterType:  "kafka",
		}, logger, false)
		if err == nil {
			t.Fatal("expected error for invalid tracing exporter")
		}
		if shutdown == nil {
			t.Fatal("expected shutdown func on error path")
		}
		_ = shutdown(ctx)
	})

	t.Run("metrics error returns shutdown cleanup", func(t *testing.T) {
		shutdown, err := SetupOTEL(ctx, &config.OTELConfig{
			Enabled:       true,
			EnableMetrics: true,
			ExporterType:  "kafka",
		}, logger, false)
		if err == nil {
			t.Fatal("expected error for invalid metrics exporter")
		}
		if shutdown == nil {
			t.Fatal("expected shutdown func on error path")
		}
		_ = shutdown(ctx)
	})
}
