package config_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/config"
	"github.com/eval-hub/eval-hub/internal/logging"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestLoadConfig(t *testing.T) {
	logger := logging.FallbackLogger()

	t.Run("loading config from tests directory", func(t *testing.T) {
		serviceConfig, err := config.LoadConfig(logger, "0.0.1", "local", time.Now().Format(time.RFC3339), "../../tests")
		if err != nil {
			t.Fatalf("Failed to load config: %v", err)
		}
		if serviceConfig == nil {
			t.Fatalf("Service config is nil")
		}
		if serviceConfig.Service.ReadyFile != "/tmp/repo-ready" {
			t.Fatalf("Ready file is not /tmp/repo-ready, got %s", serviceConfig.Service.ReadyFile)
		}
		if serviceConfig.Service.TerminationFile != "/tmp/termination-log" {
			t.Fatalf("Termination file is not /tmp/termination-log, got %s", serviceConfig.Service.TerminationFile)
		}
	})

	t.Run("setting environment variables", func(t *testing.T) {
		os.Setenv("MLFLOW_TRACKING_URI", "http://localhost:9999")
		t.Cleanup(func() {
			os.Unsetenv("MLFLOW_TRACKING_URI")
		})
		serviceConfig, err := config.LoadConfig(logger, "0.0.1", "local", time.Now().Format(time.RFC3339), "../../tests")
		if err != nil {
			t.Fatalf("Failed to load config: %v", err)
		}
		if serviceConfig == nil {
			t.Fatalf("Service config is nil")
		}
		if serviceConfig.MLFlow.TrackingURI != "http://localhost:9999" {
			t.Fatalf("MLFlow tracking URI is not http://localhost:9999, got %s", serviceConfig.MLFlow.TrackingURI)
		}
	})

	t.Run("CONFIG_PATH overrides base config values", func(t *testing.T) {
		// Create a base config with sqlite and port 8080
		baseDir := t.TempDir()
		baseContent := `
service:
  port: 8080
  ready_file: "/tmp/repo-ready"
  termination_file: "/tmp/termination-log"
database:
  driver: sqlite
  url: "file::memory:?mode=memory&cache=shared"
`
		err := os.WriteFile(filepath.Join(baseDir, "config.yaml"), []byte(baseContent), 0600)
		if err != nil {
			t.Fatalf("Failed to write base config: %v", err)
		}

		// Operator-mounted config overrides the database driver
		operatorDir := t.TempDir()
		operatorContent := `
database:
  driver: pgx
  url: "postgres://localhost:5432/eval_hub"
`
		err = os.WriteFile(filepath.Join(operatorDir, "config.yaml"), []byte(operatorContent), 0600)
		if err != nil {
			t.Fatalf("Failed to write operator config: %v", err)
		}

		os.Setenv("CONFIG_PATH", filepath.Join(operatorDir, "config.yaml"))
		t.Cleanup(func() {
			os.Unsetenv("CONFIG_PATH")
		})

		serviceConfig, err := config.LoadConfig(logger, "0.0.1", "local", time.Now().Format(time.RFC3339), baseDir)
		if err != nil {
			t.Fatalf("Failed to load config: %v", err)
		}
		// database.driver should be overridden by CONFIG_PATH
		db := *serviceConfig.Database
		if driver, ok := db["driver"]; !ok || driver.(string) != "pgx" {
			t.Fatalf("Expected database driver pgx from CONFIG_PATH, got %v", db["driver"])
		}
		// service.port should be preserved from the base config
		if serviceConfig.Service.Port != 8080 {
			t.Fatalf("Expected port 8080 from base config, got %d", serviceConfig.Service.Port)
		}
	})

	t.Run("CONFIG_PATH without service section preserves base service config", func(t *testing.T) {
		// Create a base config with service section
		baseDir := t.TempDir()
		baseContent := `
service:
  port: 8080
  ready_file: "/tmp/repo-ready"
  termination_file: "/tmp/termination-log"
database:
  driver: sqlite
  url: "file::memory:?mode=memory&cache=shared"
`
		err := os.WriteFile(filepath.Join(baseDir, "config.yaml"), []byte(baseContent), 0600)
		if err != nil {
			t.Fatalf("Failed to write base config: %v", err)
		}

		// Operator config has no service section
		operatorDir := t.TempDir()
		operatorContent := `
database:
  driver: pgx
secrets:
  dir: /tmp
  mappings:
    db-url:optional: database.url
`
		err = os.WriteFile(filepath.Join(operatorDir, "config.yaml"), []byte(operatorContent), 0600)
		if err != nil {
			t.Fatalf("Failed to write operator config: %v", err)
		}

		os.Setenv("CONFIG_PATH", filepath.Join(operatorDir, "config.yaml"))
		t.Cleanup(func() {
			os.Unsetenv("CONFIG_PATH")
		})

		serviceConfig, err := config.LoadConfig(logger, "0.0.1", "local", time.Now().Format(time.RFC3339), baseDir)
		if err != nil {
			t.Fatalf("Failed to load config: %v", err)
		}
		if serviceConfig.Service == nil {
			t.Fatalf("Service should be preserved from base config")
		}
		if serviceConfig.Service.Port != 8080 {
			t.Fatalf("Expected port 8080 from base config, got %d", serviceConfig.Service.Port)
		}
	})

	t.Run("CONFIG_PATH replaces bundled secret mappings", func(t *testing.T) {
		// Bundled config has a non-optional secret mapping (db_password).
		// Operator config has a different mapping (db-url).
		// After merge, only the operator's mapping should exist.
		baseDir := t.TempDir()
		baseContent := `
service:
  port: 8080
  ready_file: "/tmp/repo-ready"
  termination_file: "/tmp/termination-log"
secrets:
  dir: /tmp
  mappings:
    db_password: database.password
`
		err := os.WriteFile(filepath.Join(baseDir, "config.yaml"), []byte(baseContent), 0600)
		if err != nil {
			t.Fatalf("Failed to write base config: %v", err)
		}

		operatorDir := t.TempDir()
		operatorContent := `
database:
  driver: pgx
secrets:
  dir: /tmp
  mappings:
    db-url:optional: database.url
`
		err = os.WriteFile(filepath.Join(operatorDir, "config.yaml"), []byte(operatorContent), 0600)
		if err != nil {
			t.Fatalf("Failed to write operator config: %v", err)
		}

		os.Setenv("CONFIG_PATH", filepath.Join(operatorDir, "config.yaml"))
		t.Cleanup(func() {
			os.Unsetenv("CONFIG_PATH")
		})

		// Should NOT fail looking for /tmp/db_password
		serviceConfig, err := config.LoadConfig(logger, "0.0.1", "local", time.Now().Format(time.RFC3339), baseDir)
		if err != nil {
			t.Fatalf("Failed to load config: %v", err)
		}
		if serviceConfig == nil {
			t.Fatalf("Service config is nil")
		}
	})

	t.Run("loading config from secrets directory", func(t *testing.T) {
		// create a secret and store in /tmp/db_password
		secret := "mysecret"
		secretPath := "/tmp/db_password"
		err := os.WriteFile(secretPath, []byte(secret), 0600)
		if err != nil {
			t.Fatalf("Failed to create secret: %v", err)
		}
		t.Cleanup(func() {
			os.Remove(secretPath)
		})
		serviceConfig, err := config.LoadConfig(logger, "0.0.1", "local", time.Now().Format(time.RFC3339), "../../tests/secrets")
		if err != nil {
			t.Fatalf("Failed to load config: %v", err)
		}
		if serviceConfig == nil {
			t.Fatalf("Service config is nil")
		}
		if serviceConfig.Database == nil {
			t.Fatalf("Database config is nil")
		}
		db := *serviceConfig.Database
		if password, ok := db["password"]; ok {
			if password.(string) != secret {
				t.Fatalf("Database password is not %s, got %s", secret, password.(string))
			}
		} else {
			t.Fatalf("Database password is not set")
		}
	})
}

func TestRedactedJSON(t *testing.T) {
	type inner struct {
		URL      string `json:"url"`
		Driver   string `json:"driver"`
		Password string `json:"password"`
	}
	type outer struct {
		Database inner  `json:"database"`
		Name     string `json:"name"`
	}

	t.Run("redacts password with [redacted]", func(t *testing.T) {
		v := outer{
			Database: inner{Password: "s3cret", Driver: "pgx"},
			Name:     "test",
		}
		result := config.RedactedJSON(v, []string{"database.password"})
		if !contains(result, `"password":"[redacted]"`) {
			t.Fatalf("Expected password to be [redacted], got %s", result)
		}
		if !contains(result, `"name":"test"`) {
			t.Fatalf("Expected name to be preserved, got %s", result)
		}
	})

	t.Run("sanitises URL by stripping password", func(t *testing.T) {
		v := outer{
			Database: inner{
				URL:    "postgres://user:p4ss@db-host:5432/evalhub",
				Driver: "pgx",
			},
		}
		result := config.RedactedJSON(v, []string{"database.url"})
		if contains(result, "p4ss") {
			t.Fatalf("Password should be stripped from URL, got %s", result)
		}
		if !contains(result, "user@db-host:5432") {
			t.Fatalf("Expected sanitised URL with user and host, got %s", result)
		}
	})

	t.Run("no redacted fields returns full JSON", func(t *testing.T) {
		v := outer{
			Database: inner{Password: "s3cret"},
			Name:     "test",
		}
		result := config.RedactedJSON(v, nil)
		if !contains(result, "s3cret") {
			t.Fatalf("Expected unredacted output, got %s", result)
		}
	})

	t.Run("non-existent field path is a no-op", func(t *testing.T) {
		v := outer{
			Database: inner{Password: "s3cret"},
		}
		result := config.RedactedJSON(v, []string{"database.missing"})
		if !contains(result, "s3cret") {
			t.Fatalf("Expected password to be untouched, got %s", result)
		}
	})
}

func TestLoadProviderConfigs(t *testing.T) {
	logger := logging.FallbackLogger()

	t.Run("loads providers from explicit config dir", func(t *testing.T) {
		dir := t.TempDir()
		provDir := filepath.Join(dir, "providers")
		os.MkdirAll(provDir, 0755)
		writeProviderYAML(t, provDir, "alpha", "Alpha Provider")

		providers, err := config.LoadProviderConfigs(logger, dir)
		if err != nil {
			t.Fatalf("LoadProviderConfigs failed: %v", err)
		}
		if len(providers) != 1 {
			t.Fatalf("Expected 1 provider, got %d", len(providers))
		}
		if _, ok := providers["alpha"]; !ok {
			t.Fatalf("Expected provider 'alpha', got keys: %v", providerIDs(providers))
		}
	})

	t.Run("loads multiple providers from explicit dir", func(t *testing.T) {
		dir := t.TempDir()
		provDir := filepath.Join(dir, "providers")
		os.MkdirAll(provDir, 0755)
		writeProviderYAML(t, provDir, "alpha", "Alpha")
		writeProviderYAML(t, provDir, "beta", "Beta")
		writeProviderYAML(t, provDir, "gamma", "Gamma")

		providers, err := config.LoadProviderConfigs(logger, dir)
		if err != nil {
			t.Fatalf("LoadProviderConfigs failed: %v", err)
		}
		if len(providers) != 3 {
			t.Fatalf("Expected 3 providers, got %d", len(providers))
		}
	})

	t.Run("skips providers with missing id", func(t *testing.T) {
		dir := t.TempDir()
		provDir := filepath.Join(dir, "providers")
		os.MkdirAll(provDir, 0755)
		content := "name: No ID Provider\ndescription: missing id field\n"
		os.WriteFile(filepath.Join(provDir, "noid.yaml"), []byte(content), 0600)

		providers, err := config.LoadProviderConfigs(logger, dir)
		if err != nil {
			t.Fatalf("LoadProviderConfigs failed: %v", err)
		}
		if len(providers) != 0 {
			t.Fatalf("Expected 0 providers (missing id), got %d", len(providers))
		}
	})

	t.Run("ignores non-yaml files", func(t *testing.T) {
		dir := t.TempDir()
		provDir := filepath.Join(dir, "providers")
		os.MkdirAll(provDir, 0755)
		writeProviderYAML(t, provDir, "alpha", "Alpha")
		os.WriteFile(filepath.Join(provDir, "readme.txt"), []byte("ignore me"), 0600)
		os.MkdirAll(filepath.Join(provDir, "subdir"), 0755)

		providers, err := config.LoadProviderConfigs(logger, dir)
		if err != nil {
			t.Fatalf("LoadProviderConfigs failed: %v", err)
		}
		if len(providers) != 1 {
			t.Fatalf("Expected 1 provider (ignoring non-yaml), got %d", len(providers))
		}
	})

	t.Run("returns empty map when providers dir missing", func(t *testing.T) {
		dir := t.TempDir() // no "providers" subdir

		providers, err := config.LoadProviderConfigs(logger, dir)
		if err != nil {
			t.Fatalf("LoadProviderConfigs failed: %v", err)
		}
		if len(providers) != 0 {
			t.Fatalf("Expected 0 providers, got %d", len(providers))
		}
	})

	t.Run("falls back to default lookup paths when no explicit dir", func(t *testing.T) {
		providers, err := config.LoadProviderConfigs(logger, "")
		if err != nil {
			t.Fatalf("LoadProviderConfigs failed: %v", err)
		}
		// Should find the bundled config/providers/*.yaml via default lookup paths
		if len(providers) == 0 {
			t.Log("No providers found via default paths (expected in some CI environments)")
		}
	})

	t.Run("provider fields are correctly parsed", func(t *testing.T) {
		dir := t.TempDir()
		provDir := filepath.Join(dir, "providers")
		os.MkdirAll(provDir, 0755)
		content := `id: mytest
name: My Test Provider
description: A test provider for unit testing
type: builtin
benchmarks:
  - id: bench1
    name: Benchmark One
    description: First benchmark
    category: safety
`
		os.WriteFile(filepath.Join(provDir, "mytest.yaml"), []byte(content), 0600)

		providers, err := config.LoadProviderConfigs(logger, dir)
		if err != nil {
			t.Fatalf("LoadProviderConfigs failed: %v", err)
		}
		p, ok := providers["mytest"]
		if !ok {
			t.Fatalf("Expected provider 'mytest'")
		}
		if p.Name != "My Test Provider" {
			t.Fatalf("Expected name 'My Test Provider', got '%s'", p.Name)
		}
		if p.Description != "A test provider for unit testing" {
			t.Fatalf("Expected description 'A test provider for unit testing', got '%s'", p.Description)
		}
		if len(p.Benchmarks) != 1 {
			t.Fatalf("Expected 1 benchmark, got %d", len(p.Benchmarks))
		}
		if p.Benchmarks[0].ID != "bench1" {
			t.Fatalf("Expected benchmark id 'bench1', got '%s'", p.Benchmarks[0].ID)
		}
	})
}

func writeProviderYAML(t *testing.T, dir, id, name string) {
	t.Helper()
	content := fmt.Sprintf("id: %s\nname: %s\ndescription: test provider\n", id, name)
	if err := os.WriteFile(filepath.Join(dir, id+".yaml"), []byte(content), 0600); err != nil {
		t.Fatalf("Failed to write provider YAML: %v", err)
	}
}

func providerIDs(providers map[string]api.ProviderResource) []string {
	ids := make([]string, 0, len(providers))
	for id := range providers {
		ids = append(ids, id)
	}
	return ids
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
