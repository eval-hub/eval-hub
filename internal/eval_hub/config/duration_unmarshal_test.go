package config_test

import (
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/spf13/viper"
)

func TestViperUnmarshalDurationFields(t *testing.T) {
	v := viper.New()
	v.Set("otel.metric_export_interval", "60s")
	v.Set("otel.tracer_timeout", "30s")

	var cfg config.Config
	if err := v.Unmarshal(&cfg); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if cfg.OTEL == nil {
		t.Fatal("expected OTEL config")
	}
	if cfg.OTEL.MetricExportInterval != 60*time.Second {
		t.Fatalf("MetricExportInterval = %v, want 60s", cfg.OTEL.MetricExportInterval)
	}
	if cfg.OTEL.TracerTimeout != 30*time.Second {
		t.Fatalf("TracerTimeout = %v, want 30s", cfg.OTEL.TracerTimeout)
	}
}
