package k8s

import (
	"crypto/tls"
	"encoding/json"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/otel"
)

func TestOtelConfigForJobPod(t *testing.T) {
	t.Run("nil when OTEL disabled", func(t *testing.T) {
		cfg := &config.Config{OTEL: &config.OTELConfig{Enabled: false}}
		if got := otelConfigForJobPod(cfg); got != nil {
			t.Fatalf("expected nil, got %+v", got)
		}
	})

	t.Run("copies enabled OTEL with sidecar service name", func(t *testing.T) {
		cfg := &config.Config{
			OTEL: &config.OTELConfig{
				Enabled:          true,
				EnableMetrics:    true,
				EnableTracing:    true,
				ExporterType:     otel.ExporterTypeOTLPGRPC,
				ExporterEndpoint: "collector:4317",
			},
		}
		got := otelConfigForJobPod(cfg)
		if got == nil {
			t.Fatal("expected OTEL config")
		}
		if got.ServiceName != otel.SidecarServiceName {
			t.Fatalf("service name = %q, want %q", got.ServiceName, otel.SidecarServiceName)
		}
		if got.ExporterEndpoint != "collector:4317" {
			t.Fatalf("endpoint = %q", got.ExporterEndpoint)
		}
	})
}

func TestSidecarForJobPodIncludesOTEL(t *testing.T) {
	cfg := &config.Config{
		OTEL: &config.OTELConfig{
			Enabled:          true,
			EnableMetrics:    true,
			ExporterType:     otel.ExporterTypeStdout,
			ExporterInsecure: true,
		},
		Sidecar: &config.SidecarConfig{Port: 8080},
	}
	jc := &jobConfig{evalHubURL: "http://eval-hub:8080"}

	export, err := sidecarForJobPod(cfg, jc)
	if err != nil {
		t.Fatalf("sidecarForJobPod: %v", err)
	}
	if export.OTEL == nil {
		t.Fatal("expected OTEL in sidecar export")
	}
	if export.OTEL.ServiceName != otel.SidecarServiceName {
		t.Fatalf("service name = %q", export.OTEL.ServiceName)
	}

	data, err := json.Marshal(export)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !json.Valid(data) {
		t.Fatalf("invalid JSON: %s", data)
	}
}

func TestSidecarForJobPodDerivesMLFlowInsecureSkipVerify(t *testing.T) {
	cfg := &config.Config{
		Sidecar: &config.SidecarConfig{Port: 8080},
		MLFlow: &config.MLFlowConfig{
			HTTPTimeout: 30 * time.Second,
			CACertPath:  "/etc/mlflow/ca.crt",
			TLSConfig:   &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		},
	}
	jc := &jobConfig{
		mlflowTrackingURI: "https://mlflow.example.com",
		mlflowWorkspace:   "ws-1",
	}

	export, err := sidecarForJobPod(cfg, jc)
	if err != nil {
		t.Fatalf("sidecarForJobPod: %v", err)
	}
	if export.MLFlow == nil {
		t.Fatal("expected MLFlow in sidecar export")
	}
	if export.MLFlow.HTTPTimeout != 30*time.Second {
		t.Fatalf("HTTPTimeout = %v, want 30s", export.MLFlow.HTTPTimeout)
	}
	if export.MLFlow.CACertPath != "/etc/mlflow/ca.crt" {
		t.Fatalf("CACertPath = %q", export.MLFlow.CACertPath)
	}
	if export.MLFlow.Workspace != "ws-1" {
		t.Fatalf("Workspace = %q", export.MLFlow.Workspace)
	}
}
