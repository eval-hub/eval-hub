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
	tests := []struct {
		name       string
		envValue   string
		setEnv     bool
		wantDur    time.Duration
		wantErrSub string
	}{
		{
			name:    "unset defaults to 60s",
			setEnv:  false,
			wantDur: 60 * time.Second,
		},
		{
			name:     "empty string defaults to 60s",
			envValue: "",
			setEnv:   true,
			wantDur:  60 * time.Second,
		},
		{
			name:     "valid positive integer",
			envValue: "30",
			setEnv:   true,
			wantDur:  30 * time.Second,
		},
		{
			name:     "valid large integer",
			envValue: "300",
			setEnv:   true,
			wantDur:  300 * time.Second,
		},
		{
			name:       "zero causes error",
			envValue:   "0",
			setEnv:     true,
			wantErrSub: "must be a positive integer",
		},
		{
			name:       "negative causes error",
			envValue:   "-5",
			setEnv:     true,
			wantErrSub: "must be a positive integer",
		},
		{
			name:       "non-integer causes error",
			envValue:   "abc",
			setEnv:     true,
			wantErrSub: "must be a positive integer",
		},
		{
			name:       "float causes error",
			envValue:   "1.5",
			setEnv:     true,
			wantErrSub: "must be a positive integer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				t.Setenv("OTEL_METRIC_EXPORT_INTERVAL", tt.envValue)
			}

			dur, err := parseMeterExportInterval()

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
	t.Setenv("OTEL_METRIC_EXPORT_INTERVAL", "abc")

	cfg := &config.OTELConfig{
		Enabled:      true,
		ExporterType: "stdout",
	}

	mp, err := newMeterProvider(context.Background(), cfg, slog.Default(), false)
	if err == nil {
		t.Fatal("expected error for invalid interval, got nil")
	}
	if !strings.Contains(err.Error(), "must be a positive integer") {
		t.Fatalf("expected error about positive integer, got %q", err.Error())
	}
	if mp != nil {
		t.Fatal("expected nil provider on error")
	}
}
