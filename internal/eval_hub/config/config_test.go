package config_test

import (
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
)

func TestIsOTELEnabled(t *testing.T) {
	t.Run("nil config returns false", func(t *testing.T) {
		var c *config.Config
		if c.IsOTELEnabled() {
			t.Error("IsOTELEnabled() on nil config should return false")
		}
	})
	t.Run("nil OTEL returns false", func(t *testing.T) {
		c := &config.Config{}
		if c.IsOTELEnabled() {
			t.Error("IsOTELEnabled() with nil OTEL should return false")
		}
	})
	t.Run("OTEL disabled returns false", func(t *testing.T) {
		c := &config.Config{OTEL: &config.OTELConfig{Enabled: false}}
		if c.IsOTELEnabled() {
			t.Error("IsOTELEnabled() with Enabled=false should return false")
		}
	})
	t.Run("OTEL enabled returns true", func(t *testing.T) {
		c := &config.Config{OTEL: &config.OTELConfig{Enabled: true}}
		if !c.IsOTELEnabled() {
			t.Error("IsOTELEnabled() with Enabled=true should return true")
		}
	})
}

func TestIsPrometheusEnabled(t *testing.T) {
	t.Run("nil config returns false", func(t *testing.T) {
		var c *config.Config
		if c.IsPrometheusEnabled() {
			t.Error("IsPrometheusEnabled() on nil config should return false")
		}
	})
	t.Run("nil Prometheus returns false", func(t *testing.T) {
		c := &config.Config{}
		if c.IsPrometheusEnabled() {
			t.Error("IsPrometheusEnabled() with nil Prometheus should return false")
		}
	})
	t.Run("Prometheus disabled returns false", func(t *testing.T) {
		c := &config.Config{Prometheus: &config.PrometheusConfig{Enabled: false}}
		if c.IsPrometheusEnabled() {
			t.Error("IsPrometheusEnabled() with Enabled=false should return false")
		}
	})
	t.Run("Prometheus enabled returns true", func(t *testing.T) {
		c := &config.Config{Prometheus: &config.PrometheusConfig{Enabled: true}}
		if !c.IsPrometheusEnabled() {
			t.Error("IsPrometheusEnabled() with Enabled=true should return true")
		}
	})
}

func TestIsOTELStorageScansEnabled(t *testing.T) {
	t.Run("OTEL off returns false", func(t *testing.T) {
		c := &config.Config{OTEL: &config.OTELConfig{Enabled: false, DisableDatabaseOTELScans: false}}
		if c.IsOTELStorageScansEnabled() {
			t.Error("expected false when OTEL disabled")
		}
	})
	t.Run("OTEL on and scans disabled returns false", func(t *testing.T) {
		c := &config.Config{OTEL: &config.OTELConfig{Enabled: true, DisableDatabaseOTELScans: true}}
		if c.IsOTELStorageScansEnabled() {
			t.Error("expected false when DisableDatabaseOTELScans")
		}
	})
	t.Run("OTEL on and scans not disabled returns true", func(t *testing.T) {
		c := &config.Config{OTEL: &config.OTELConfig{Enabled: true, DisableDatabaseOTELScans: false}}
		if !c.IsOTELStorageScansEnabled() {
			t.Error("expected true when OTEL enabled and scans allowed")
		}
	})
}

func TestIsAuthenticationEnabled(t *testing.T) {
	t.Run("nil config returns false", func(t *testing.T) {
		var c *config.Config
		if c.IsAuthenticationEnabled() {
			t.Error("nil config")
		}
	})
	t.Run("nil service returns false", func(t *testing.T) {
		c := &config.Config{}
		if c.IsAuthenticationEnabled() {
			t.Error("nil Service")
		}
	})
	t.Run("local mode returns false", func(t *testing.T) {
		c := &config.Config{Service: &config.ServiceConfig{LocalMode: true, DisableAuth: false}}
		if c.IsAuthenticationEnabled() {
			t.Error("LocalMode")
		}
	})
	t.Run("disable auth returns false", func(t *testing.T) {
		c := &config.Config{Service: &config.ServiceConfig{LocalMode: false, DisableAuth: true}}
		if c.IsAuthenticationEnabled() {
			t.Error("DisableAuth")
		}
	})
	t.Run("auth enabled when not local and not disabled", func(t *testing.T) {
		c := &config.Config{Service: &config.ServiceConfig{LocalMode: false, DisableAuth: false}}
		if !c.IsAuthenticationEnabled() {
			t.Error("expected true")
		}
	})
}

func TestServiceConfig_TLS(t *testing.T) {
	t.Run("TLSEnabled false when incomplete", func(t *testing.T) {
		cases := []*config.ServiceConfig{
			{TLSCertFile: "", TLSKeyFile: ""},
			{TLSCertFile: "/c", TLSKeyFile: ""},
			{TLSCertFile: "", TLSKeyFile: "/k"},
		}
		for _, c := range cases {
			if c.TLSEnabled() {
				t.Errorf("TLSEnabled() = true for %#v", c)
			}
		}
	})
	t.Run("TLSEnabled true when both set", func(t *testing.T) {
		c := &config.ServiceConfig{TLSCertFile: "/c", TLSKeyFile: "/k"}
		if !c.TLSEnabled() {
			t.Error("expected true")
		}
	})
	t.Run("ValidateTLSConfig nil error when both or neither", func(t *testing.T) {
		if err := (&config.ServiceConfig{}).ValidateTLSConfig(); err != nil {
			t.Errorf("empty: %v", err)
		}
		if err := (&config.ServiceConfig{TLSCertFile: "/c", TLSKeyFile: "/k"}).ValidateTLSConfig(); err != nil {
			t.Errorf("both: %v", err)
		}
	})
	t.Run("ValidateTLSConfig error when partial", func(t *testing.T) {
		if err := (&config.ServiceConfig{TLSCertFile: "/c"}).ValidateTLSConfig(); err == nil {
			t.Error("cert only: want error")
		}
		if err := (&config.ServiceConfig{TLSKeyFile: "/k"}).ValidateTLSConfig(); err == nil {
			t.Error("key only: want error")
		}
	})
}
