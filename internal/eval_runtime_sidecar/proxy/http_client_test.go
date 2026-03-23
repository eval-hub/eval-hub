package proxy

import (
	"log/slog"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
)

func TestNewEvalHubHTTPClient(t *testing.T) {
	logger := slog.Default()

	t.Run("returns nil when config is nil", func(t *testing.T) {
		client, err := NewEvalHubHTTPClient(nil, false, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client != nil {
			t.Error("expected nil client when config is nil")
		}
	})

	t.Run("returns nil when Sidecar is nil", func(t *testing.T) {
		cfg := &config.Config{}
		client, err := NewEvalHubHTTPClient(cfg, false, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client != nil {
			t.Error("expected nil client when Sidecar is nil")
		}
	})

	t.Run("returns client when Sidecar and EvalHub set", func(t *testing.T) {
		cfg := &config.Config{
			Sidecar: &config.SidecarConfig{
				EvalHub: &config.EvalHubClientConfig{
					InsecureSkipVerify: true,
				},
			},
		}
		client, err := NewEvalHubHTTPClient(cfg, false, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client")
		}
		if client.Timeout == 0 {
			t.Error("expected non-zero timeout")
		}
	})
}

func TestNewMLFlowHTTPClient(t *testing.T) {
	logger := slog.Default()

	t.Run("returns nil when config is nil", func(t *testing.T) {
		client, err := NewMLFlowHTTPClient(nil, false, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client != nil {
			t.Error("expected nil client when config is nil")
		}
	})

	t.Run("returns nil when MLFlow is nil", func(t *testing.T) {
		cfg := &config.Config{}
		client, err := NewMLFlowHTTPClient(cfg, false, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client != nil {
			t.Error("expected nil client when MLFlow is nil")
		}
	})

	t.Run("returns nil when TrackingURI is empty", func(t *testing.T) {
		cfg := &config.Config{
			MLFlow: &config.MLFlowConfig{},
		}
		client, err := NewMLFlowHTTPClient(cfg, false, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client != nil {
			t.Error("expected nil client when TrackingURI is empty")
		}
	})

	t.Run("returns client when MLFlow and TrackingURI set", func(t *testing.T) {
		cfg := &config.Config{
			MLFlow: &config.MLFlowConfig{
				TrackingURI: "https://mlflow.example.com",
			},
		}
		client, err := NewMLFlowHTTPClient(cfg, false, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client")
		}
		if client.Timeout == 0 {
			t.Error("expected non-zero timeout")
		}
	})
}
