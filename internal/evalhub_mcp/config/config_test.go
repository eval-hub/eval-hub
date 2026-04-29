package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Transport != "stdio" {
		t.Errorf("expected default transport \"stdio\", got %q", cfg.Transport)
	}
	if cfg.Host != "localhost" {
		t.Errorf("expected default host \"localhost\", got %q", cfg.Host)
	}
	if cfg.Port != 3001 {
		t.Errorf("expected default port 3001, got %d", cfg.Port)
	}
	if cfg.BaseURL != "" {
		t.Errorf("expected empty default base_url, got %q", cfg.BaseURL)
	}
	if cfg.Token != "" {
		t.Errorf("expected empty default token, got %q", cfg.Token)
	}
	if cfg.Tenant != "" {
		t.Errorf("expected empty default tenant, got %q", cfg.Tenant)
	}
	if cfg.Insecure {
		t.Error("expected default insecure to be false")
	}
}

func TestLoadNoConfig(t *testing.T) {
	clearEnv(t)

	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Transport != "stdio" {
		t.Errorf("expected transport \"stdio\", got %q", cfg.Transport)
	}
}

func TestLoadEnvVars(t *testing.T) {
	clearEnv(t)
	t.Setenv("EVALHUB_BASE_URL", "http://env-host:9090")
	t.Setenv("EVALHUB_TOKEN", "env-token")
	t.Setenv("EVALHUB_TENANT", "env-tenant")
	t.Setenv("EVALHUB_INSECURE", "true")

	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.BaseURL != "http://env-host:9090" {
		t.Errorf("expected base_url from env, got %q", cfg.BaseURL)
	}
	if cfg.Token != "env-token" {
		t.Errorf("expected token from env, got %q", cfg.Token)
	}
	if cfg.Tenant != "env-tenant" {
		t.Errorf("expected tenant from env, got %q", cfg.Tenant)
	}
	if !cfg.Insecure {
		t.Error("expected insecure=true from env")
	}
}

func TestLoadYAMLOverridesEnv(t *testing.T) {
	clearEnv(t)
	t.Setenv("EVALHUB_BASE_URL", "http://env-host:9090")
	t.Setenv("EVALHUB_TOKEN", "env-token")

	configFile := writeConfig(t, `
default_profile: dev
profiles:
  dev:
    base_url: http://yaml-host:8080
    token: yaml-token
    tenant: yaml-tenant
`)

	flags := &Flags{ConfigPath: configFile}
	cfg, err := Load(flags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.BaseURL != "http://yaml-host:8080" {
		t.Errorf("expected YAML base_url to override env, got %q", cfg.BaseURL)
	}
	if cfg.Token != "yaml-token" {
		t.Errorf("expected YAML token to override env, got %q", cfg.Token)
	}
	if cfg.Tenant != "yaml-tenant" {
		t.Errorf("expected tenant from YAML, got %q", cfg.Tenant)
	}
}

func TestLoadCLIFlagsOverrideYAMLAndEnv(t *testing.T) {
	clearEnv(t)
	t.Setenv("EVALHUB_BASE_URL", "http://env-host:9090")

	configFile := writeConfig(t, `
default_profile: default
profiles:
  default:
    base_url: http://yaml-host:8080
    insecure: false
`)

	transport := "http"
	host := "0.0.0.0"
	port := 4000
	insecure := true

	flags := &Flags{
		ConfigPath: configFile,
		Transport:  &transport,
		Host:       &host,
		Port:       &port,
		Insecure:   &insecure,
	}
	cfg, err := Load(flags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Transport != "http" {
		t.Errorf("expected CLI transport, got %q", cfg.Transport)
	}
	if cfg.Host != "0.0.0.0" {
		t.Errorf("expected CLI host, got %q", cfg.Host)
	}
	if cfg.Port != 4000 {
		t.Errorf("expected CLI port 4000, got %d", cfg.Port)
	}
	if !cfg.Insecure {
		t.Error("expected CLI insecure=true to override YAML")
	}
	if cfg.BaseURL != "http://yaml-host:8080" {
		t.Errorf("expected YAML base_url (not env), got %q", cfg.BaseURL)
	}
}

func TestLoadMissingDefaultConfigFile(t *testing.T) {
	clearEnv(t)

	cfg, err := Load(&Flags{})
	if err != nil {
		t.Fatalf("missing default config file should not error, got: %v", err)
	}
	if cfg.Transport != "stdio" {
		t.Errorf("expected default transport, got %q", cfg.Transport)
	}
}

func TestLoadMissingExplicitConfigFile(t *testing.T) {
	clearEnv(t)

	_, err := Load(&Flags{ConfigPath: "/nonexistent/config.yaml"})
	if err == nil {
		t.Fatal("expected error for missing explicit config file")
	}
}

func TestLoadMalformedYAML(t *testing.T) {
	clearEnv(t)

	configFile := writeConfig(t, `{{{not valid yaml`)

	_, err := Load(&Flags{ConfigPath: configFile})
	if err == nil {
		t.Fatal("expected error for malformed YAML")
	}
}

func TestLoadMissingProfile(t *testing.T) {
	clearEnv(t)

	configFile := writeConfig(t, `
default_profile: nonexistent
profiles:
  dev:
    base_url: http://localhost:8080
`)

	_, err := Load(&Flags{ConfigPath: configFile})
	if err == nil {
		t.Fatal("expected error for missing profile")
	}
}

func TestLoadProfileInsecurePointer(t *testing.T) {
	clearEnv(t)
	t.Setenv("EVALHUB_INSECURE", "true")

	configFile := writeConfig(t, `
default_profile: secure
profiles:
  secure:
    base_url: http://localhost:8080
    insecure: false
`)

	cfg, err := Load(&Flags{ConfigPath: configFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Insecure {
		t.Error("expected YAML insecure=false to override env insecure=true")
	}
}

func TestValidateTransport(t *testing.T) {
	tests := []struct {
		transport string
		wantErr   bool
	}{
		{"stdio", false},
		{"http", false},
		{"grpc", true},
		{"websocket", true},
		{"", true},
	}

	for _, tt := range tests {
		t.Run(tt.transport, func(t *testing.T) {
			cfg := &Config{Transport: tt.transport, Host: "localhost", Port: 3001}
			err := Validate(cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate(transport=%q) error = %v, wantErr %v", tt.transport, err, tt.wantErr)
			}
		})
	}
}

func TestValidatePort(t *testing.T) {
	tests := []struct {
		port    int
		wantErr bool
	}{
		{0, false},
		{1, false},
		{3001, false},
		{65535, false},
		{65536, true},
		{-1, true},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			cfg := &Config{Transport: "http", Host: "localhost", Port: tt.port}
			err := Validate(cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate(port=%d) error = %v, wantErr %v", tt.port, err, tt.wantErr)
			}
		})
	}
}

func TestValidateStdioIgnoresPort(t *testing.T) {
	cfg := &Config{Transport: "stdio", Host: "localhost", Port: 0}
	if err := Validate(cfg); err != nil {
		t.Errorf("stdio transport should not validate port, got: %v", err)
	}
}

func TestValidateBaseURL(t *testing.T) {
	t.Run("valid URL passes", func(t *testing.T) {
		cfg := &Config{Transport: "stdio", Host: "localhost", BaseURL: "http://example.com:8080"}
		if err := Validate(cfg); err != nil {
			t.Errorf("expected valid URL to pass, got: %v", err)
		}
	})

	t.Run("empty URL passes (omitempty)", func(t *testing.T) {
		cfg := &Config{Transport: "stdio", Host: "localhost"}
		if err := Validate(cfg); err != nil {
			t.Errorf("expected empty URL to pass, got: %v", err)
		}
	})

	t.Run("invalid URL fails", func(t *testing.T) {
		cfg := &Config{Transport: "stdio", Host: "localhost", BaseURL: "not-a-url"}
		if err := Validate(cfg); err == nil {
			t.Error("expected invalid URL to fail validation")
		}
	})
}

func TestEnvVarInsecureInvalidValue(t *testing.T) {
	clearEnv(t)
	t.Setenv("EVALHUB_INSECURE", "not-a-bool")

	_, err := Load(nil)
	if err == nil {
		t.Fatal("expected error for invalid EVALHUB_INSECURE value")
	}
}

func TestLoadEmptyProfilesSection(t *testing.T) {
	clearEnv(t)

	configFile := writeConfig(t, `
default_profile: dev
profiles:
`)

	cfg, err := Load(&Flags{ConfigPath: configFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Transport != "stdio" {
		t.Errorf("expected default transport, got %q", cfg.Transport)
	}
}

// helpers

func clearEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"EVALHUB_BASE_URL", "EVALHUB_TOKEN", "EVALHUB_TENANT", "EVALHUB_INSECURE"} {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writing test config: %v", err)
	}
	return path
}
