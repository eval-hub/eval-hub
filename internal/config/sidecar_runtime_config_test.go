package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSidecarRuntimeConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sidecar_config.json")
	json := `{
  "port": 9090,
  "eval_hub": {
    "base_url": "https://hub.example:8443",
    "http_timeout": 5000000000
  },
  "mlflow": {
    "tracking_uri": "https://mlflow.example/ml",
    "token_path": "/var/run/secrets/mlflow/token",
    "ca_cert_path": "/tmp/ca.pem",
    "http_timeout": 10000000000
  }
}`
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadSidecarRuntimeConfig(path, "v1", "b1", "d1")
	if err != nil {
		t.Fatalf("LoadSidecarRuntimeConfig: %v", err)
	}
	if cfg.Service.ReadyFile != SidecarReadyFilePath {
		t.Fatalf("ReadyFile: %q", cfg.Service.ReadyFile)
	}
	if cfg.Sidecar.Port != 9090 {
		t.Fatalf("port %d", cfg.Sidecar.Port)
	}
	if cfg.Sidecar.EvalHub.BaseURL != "https://hub.example:8443" {
		t.Fatalf("eval_hub: %+v", cfg.Sidecar.EvalHub)
	}
	if cfg.MLFlow.TrackingURI != "https://mlflow.example/ml" || cfg.MLFlow.HTTPTimeout != 10_000_000_000 {
		t.Fatalf("MLFlow: %+v", cfg.MLFlow)
	}
}

func TestLoadSidecarRuntimeConfig_EmptyEvalHub(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sidecar_config.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadSidecarRuntimeConfig(path, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Sidecar.EvalHub == nil {
		t.Fatal("expected default EvalHub")
	}
}
