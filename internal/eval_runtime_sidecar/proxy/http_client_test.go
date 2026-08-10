package proxy

import (
	"log/slog"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
)

func TestSchemeRequiresTLS(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		url    string
		expect bool
	}{
		{"empty defaults to true", "", true},
		{"http does not require TLS", "http://localhost:8080", false},
		{"https requires TLS", "https://evalhub.example.com", true},
		{"no scheme defaults to true", "example.com", true},
		{"unparseable defaults to true", "://bad", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := schemeRequiresTLS(tc.url); got != tc.expect {
				t.Errorf("schemeRequiresTLS(%q) = %v, want %v", tc.url, got, tc.expect)
			}
		})
	}
}

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

	t.Run("http URL skips TLS config without insecure_skip_verify", func(t *testing.T) {
		cfg := &config.Config{
			Sidecar: &config.SidecarConfig{
				EvalHub: &config.EvalHubClientConfig{
					BaseURL: "http://localhost:8080",
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
	})

	t.Run("https URL with insecure_skip_verify does not require CA cert", func(t *testing.T) {
		cfg := &config.Config{
			Sidecar: &config.SidecarConfig{
				EvalHub: &config.EvalHubClientConfig{
					BaseURL:            "https://evalhub.example.com",
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

	t.Run("http TrackingURI skips TLS config", func(t *testing.T) {
		cfg := &config.Config{
			MLFlow: &config.MLFlowConfig{
				TrackingURI: "http://localhost:5000",
			},
		}
		client, err := NewMLFlowHTTPClient(cfg, false, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client")
		}
	})
}

func TestNewModelHTTPClient(t *testing.T) {
	logger := slog.Default()

	t.Run("returns nil when config is nil", func(t *testing.T) {
		client, err := NewModelHTTPClient(nil, false, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client != nil {
			t.Error("expected nil client when config is nil")
		}
	})

	t.Run("returns nil when Sidecar is nil", func(t *testing.T) {
		cfg := &config.Config{}
		client, err := NewModelHTTPClient(cfg, false, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client != nil {
			t.Error("expected nil client when Sidecar is nil")
		}
	})

	t.Run("returns client with defaults when Model is nil", func(t *testing.T) {
		cfg := &config.Config{Sidecar: &config.SidecarConfig{}}
		client, err := NewModelHTTPClient(cfg, false, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client")
		}
		if client.Timeout != DefaultHTTPTimeout {
			t.Errorf("timeout = %v, want %v", client.Timeout, DefaultHTTPTimeout)
		}
	})

	t.Run("http URL skips TLS config", func(t *testing.T) {
		cfg := &config.Config{
			Sidecar: &config.SidecarConfig{
				Model: &config.SidecarModelConfig{
					URL: "http://localhost:8080",
				},
			},
		}
		client, err := NewModelHTTPClient(cfg, false, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client")
		}
	})

	t.Run("https URL with insecure_skip_verify", func(t *testing.T) {
		cfg := &config.Config{
			Sidecar: &config.SidecarConfig{
				Model: &config.SidecarModelConfig{
					URL:                "https://model.example.com",
					InsecureSkipVerify: true,
				},
			},
		}
		client, err := NewModelHTTPClient(cfg, false, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client")
		}
	})
}

func TestNewOCIHTTPClient(t *testing.T) {
	logger := slog.Default()

	t.Run("returns nil when config is nil", func(t *testing.T) {
		client, err := NewOCIHTTPClient(nil, false, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client != nil {
			t.Error("expected nil client when config is nil")
		}
	})

	t.Run("returns nil when Sidecar is nil", func(t *testing.T) {
		cfg := &config.Config{}
		client, err := NewOCIHTTPClient(cfg, false, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client != nil {
			t.Error("expected nil client when Sidecar is nil")
		}
	})

	t.Run("returns client with defaults when OCI is nil", func(t *testing.T) {
		cfg := &config.Config{Sidecar: &config.SidecarConfig{}}
		client, err := NewOCIHTTPClient(cfg, false, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client")
		}
		if client.Timeout != DefaultHTTPTimeout {
			t.Errorf("timeout = %v, want %v", client.Timeout, DefaultHTTPTimeout)
		}
	})

	t.Run("uses custom timeout from OCI config", func(t *testing.T) {
		cfg := &config.Config{
			Sidecar: &config.SidecarConfig{
				OCI: &config.SidecarOCIConfig{
					HTTPTimeout: 60_000_000_000,
				},
			},
		}
		client, err := NewOCIHTTPClient(cfg, false, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client")
		}
	})
}
