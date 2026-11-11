package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOCIPushTimeout(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config *SidecarOCIConfig
		want   time.Duration
	}{
		{"omitted", nil, 5 * time.Minute},
		{"zero", &SidecarOCIConfig{}, 5 * time.Minute},
		{"configured", &SidecarOCIConfig{PushTimeout: time.Minute}, time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.config.EffectivePushTimeout(); got != tc.want {
				t.Fatalf("timeout=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestLoadOCIPushTimeout(t *testing.T) {
	for _, tc := range []struct {
		name, env string
		want      time.Duration
	}{
		{"yaml", "", 2 * time.Minute}, {"environment", "45s", 45 * time.Second}, {"zero", "0", 5 * time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			data := []byte("service:\n  port: 8080\nsidecar:\n  oci:\n    push_timeout: 2m\nenv_mappings:\n  OCI_PUSH_TIMEOUT: sidecar.oci.push_timeout\n")
			if err := os.WriteFile(filepath.Join(dir, "config.yaml"), data, 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("CONFIG_PATH", "")
			t.Setenv("OCI_PUSH_TIMEOUT", tc.env)
			cfg, err := LoadConfig(slog.New(slog.DiscardHandler), "test", "", "", "", dir)
			if err != nil {
				t.Fatal(err)
			}
			if got := cfg.EffectiveOCIPushTimeout(); got != tc.want {
				t.Fatalf("timeout=%v, want %v", got, tc.want)
			}
		})
	}
}
