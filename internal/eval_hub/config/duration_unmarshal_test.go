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
	v.Set("service.init.test_data_timeout", "45m")

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
	if cfg.Service == nil {
		t.Fatal("expected Service config")
	}
	if cfg.Service.Init.TestDataTimeout != 45*time.Minute {
		t.Fatalf("Init.TestDataTimeout = %v, want 45m", cfg.Service.Init.TestDataTimeout)
	}
}
