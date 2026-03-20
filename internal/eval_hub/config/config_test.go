package config

import (
	"testing"
)

func TestIsOTELEnabled(t *testing.T) {
	t.Run("nil config returns false", func(t *testing.T) {
		var c *Config
		if c.IsOTELEnabled() {
			t.Error("IsOTELEnabled() on nil config should return false")
		}
	})
	t.Run("nil OTEL returns false", func(t *testing.T) {
		c := &Config{}
		if c.IsOTELEnabled() {
			t.Error("IsOTELEnabled() with nil OTEL should return false")
		}
	})
	t.Run("OTEL disabled returns false", func(t *testing.T) {
		c := &Config{OTEL: &OTELConfig{Enabled: false}}
		if c.IsOTELEnabled() {
			t.Error("IsOTELEnabled() with Enabled=false should return false")
		}
	})
	t.Run("OTEL enabled returns true", func(t *testing.T) {
		c := &Config{OTEL: &OTELConfig{Enabled: true}}
		if !c.IsOTELEnabled() {
			t.Error("IsOTELEnabled() with Enabled=true should return true")
		}
	})
}

func TestIsPrometheusEnabled(t *testing.T) {
	t.Run("nil config returns false", func(t *testing.T) {
		var c *Config
		if c.IsPrometheusEnabled() {
			t.Error("IsPrometheusEnabled() on nil config should return false")
		}
	})
	t.Run("nil Prometheus returns false", func(t *testing.T) {
		c := &Config{}
		if c.IsPrometheusEnabled() {
			t.Error("IsPrometheusEnabled() with nil Prometheus should return false")
		}
	})
	t.Run("Prometheus disabled returns false", func(t *testing.T) {
		c := &Config{Prometheus: &PrometheusConfig{Enabled: false}}
		if c.IsPrometheusEnabled() {
			t.Error("IsPrometheusEnabled() with Enabled=false should return false")
		}
	})
	t.Run("Prometheus enabled returns true", func(t *testing.T) {
		c := &Config{Prometheus: &PrometheusConfig{Enabled: true}}
		if !c.IsPrometheusEnabled() {
			t.Error("IsPrometheusEnabled() with Enabled=true should return true")
		}
	})
}
