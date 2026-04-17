package config_test

import (
	"net/http"
	"testing"
	"time"

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

func TestServiceConfig_HTTP(t *testing.T) {
	t.Run("EffectiveReadHeaderTimeout default", func(t *testing.T) {
		var c *config.ServiceConfig
		if got := c.EffectiveReadHeaderTimeout(); got != 15*time.Second {
			t.Errorf("nil ServiceConfig: got %v", got)
		}
		if got := (&config.ServiceConfig{}).EffectiveReadHeaderTimeout(); got != 15*time.Second {
			t.Errorf("empty: got %v", got)
		}
	})
	t.Run("EffectiveReadHeaderTimeout explicit", func(t *testing.T) {
		c := &config.ServiceConfig{ReadHeaderTimeout: 3 * time.Second}
		if got := c.EffectiveReadHeaderTimeout(); got != 3*time.Second {
			t.Errorf("got %v", got)
		}
	})
	t.Run("EffectiveReadWriteIdleTimeout defaults", func(t *testing.T) {
		var c *config.ServiceConfig
		if got := c.EffectiveReadTimeout(); got != 15*time.Second {
			t.Errorf("ReadTimeout nil: got %v", got)
		}
		if got := c.EffectiveWriteTimeout(); got != 15*time.Second {
			t.Errorf("WriteTimeout nil: got %v", got)
		}
		if got := c.EffectiveIdleTimeout(); got != 60*time.Second {
			t.Errorf("IdleTimeout nil: got %v", got)
		}
		empty := &config.ServiceConfig{}
		if got := empty.EffectiveReadTimeout(); got != 15*time.Second {
			t.Errorf("ReadTimeout empty: got %v", got)
		}
		if got := empty.EffectiveWriteTimeout(); got != 15*time.Second {
			t.Errorf("WriteTimeout empty: got %v", got)
		}
		if got := empty.EffectiveIdleTimeout(); got != 60*time.Second {
			t.Errorf("IdleTimeout empty: got %v", got)
		}
	})
	t.Run("EffectiveReadWriteIdleTimeout explicit", func(t *testing.T) {
		c := &config.ServiceConfig{
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 45 * time.Second,
			IdleTimeout:  120 * time.Second,
		}
		if c.EffectiveReadTimeout() != 30*time.Second || c.EffectiveWriteTimeout() != 45*time.Second || c.EffectiveIdleTimeout() != 120*time.Second {
			t.Errorf("got read=%v write=%v idle=%v", c.EffectiveReadTimeout(), c.EffectiveWriteTimeout(), c.EffectiveIdleTimeout())
		}
	})
	t.Run("EffectiveMaxHeaderBytes", func(t *testing.T) {
		var c *config.ServiceConfig
		if got := c.EffectiveMaxHeaderBytes(); got != http.DefaultMaxHeaderBytes {
			t.Errorf("nil: got %d want %d", got, http.DefaultMaxHeaderBytes)
		}
		if got := (&config.ServiceConfig{}).EffectiveMaxHeaderBytes(); got != http.DefaultMaxHeaderBytes {
			t.Errorf("empty: got %d want %d", got, http.DefaultMaxHeaderBytes)
		}
		if got := (&config.ServiceConfig{MaxHeaderBytes: 8192}).EffectiveMaxHeaderBytes(); got != 8192 {
			t.Errorf("explicit: got %d", got)
		}
	})
	t.Run("EffectiveMaxRequestBodyBytes", func(t *testing.T) {
		var c *config.ServiceConfig
		if got := c.EffectiveMaxRequestBodyBytes(); got != config.DefaultMaxRequestBodyBytes {
			t.Errorf("nil: got %d", got)
		}
		if got := (&config.ServiceConfig{}).EffectiveMaxRequestBodyBytes(); got != config.DefaultMaxRequestBodyBytes {
			t.Errorf("zero: got %d", got)
		}
		if got := (&config.ServiceConfig{MaxRequestBodyBytes: -1}).EffectiveMaxRequestBodyBytes(); got != -1 {
			t.Errorf("unlimited: got %d", got)
		}
		if got := (&config.ServiceConfig{MaxRequestBodyBytes: 1024}).EffectiveMaxRequestBodyBytes(); got != 1024 {
			t.Errorf("explicit: got %d", got)
		}
	})
	t.Run("ValidateHTTPConfig", func(t *testing.T) {
		if err := (&config.ServiceConfig{}).ValidateHTTPConfig(); err != nil {
			t.Errorf("empty: %v", err)
		}
		if err := (&config.ServiceConfig{ReadTimeout: -1}).ValidateHTTPConfig(); err == nil {
			t.Error("negative read_timeout: want error")
		}
		if err := (&config.ServiceConfig{WriteTimeout: -1}).ValidateHTTPConfig(); err == nil {
			t.Error("negative write_timeout: want error")
		}
		if err := (&config.ServiceConfig{IdleTimeout: -1}).ValidateHTTPConfig(); err == nil {
			t.Error("negative idle_timeout: want error")
		}
		if err := (&config.ServiceConfig{ReadHeaderTimeout: -1}).ValidateHTTPConfig(); err == nil {
			t.Error("negative readheader: want error")
		}
		if err := (&config.ServiceConfig{MaxHeaderBytes: -1}).ValidateHTTPConfig(); err == nil {
			t.Error("negative max_header_bytes: want error")
		}
		if err := (&config.ServiceConfig{MaxRequestBodyBytes: -2}).ValidateHTTPConfig(); err == nil {
			t.Error("max body < -1: want error")
		}
	})
}
