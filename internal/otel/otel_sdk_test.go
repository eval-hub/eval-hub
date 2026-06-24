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
			name:       "negative duration causes error",
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
