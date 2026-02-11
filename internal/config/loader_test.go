package config_test

import (
	"os"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/config"
	"github.com/eval-hub/eval-hub/internal/logging"
)

func TestLoadConfig(t *testing.T) {
	logger := logging.FallbackLogger()

	t.Run("loading config from tests directory", func(t *testing.T) {
		secret := "mysecret"
		secretPath := "/tmp/db_password"
		err := os.WriteFile(secretPath, []byte(secret), 0600)
		if err != nil {
			t.Fatalf("Failed to create secret: %v", err)
		}
		t.Cleanup(func() {
			os.Remove(secretPath)
		})
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
		secret := "mysecret"
		secretPath := "/tmp/db_password"
		err := os.WriteFile(secretPath, []byte(secret), 0600)
		if err != nil {
			t.Fatalf("Failed to create secret: %v", err)
		}
		t.Cleanup(func() {
			os.Remove(secretPath)
		})
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
		serviceConfig, err := config.LoadConfig(logger, "0.0.1", "local", time.Now().Format(time.RFC3339), "../../tests")
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
