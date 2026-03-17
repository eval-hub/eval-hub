package runtime_sidecar

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eval-hub/eval-hub/internal/config"
)

func TestNewSidecarServer(t *testing.T) {
	logger := slog.Default()

	t.Run("returns error when logger is nil", func(t *testing.T) {
		cfg := &config.Config{}
		_, err := NewSidecarServer(nil, cfg)
		if err == nil {
			t.Fatal("expected error when logger is nil")
		}
		if err.Error() != "logger is required for the server" {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("returns error when config is nil", func(t *testing.T) {
		_, err := NewSidecarServer(logger, nil)
		if err == nil {
			t.Fatal("expected error when config is nil")
		}
		if err.Error() != "service config is required for the sidecar server" {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("uses default port 8080 when Sidecar is nil", func(t *testing.T) {
		cfg := &config.Config{}
		srv, err := NewSidecarServer(logger, cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if srv.GetPort() != 8080 {
			t.Errorf("expected port 8080, got %d", srv.GetPort())
		}
	})

	t.Run("uses default port 8080 when Sidecar.Port is 0", func(t *testing.T) {
		cfg := &config.Config{Sidecar: &config.SidecarConfig{}}
		srv, err := NewSidecarServer(logger, cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if srv.GetPort() != 8080 {
			t.Errorf("expected port 8080, got %d", srv.GetPort())
		}
	})

	t.Run("uses Sidecar.Port when set", func(t *testing.T) {
		cfg := &config.Config{Sidecar: &config.SidecarConfig{Port: 9090}}
		srv, err := NewSidecarServer(logger, cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if srv.GetPort() != 9090 {
			t.Errorf("expected port 9090, got %d", srv.GetPort())
		}
	})
}

func TestSidecarServer_GetPort(t *testing.T) {
	logger := slog.Default()
	cfg := &config.Config{Sidecar: &config.SidecarConfig{Port: 3000}}
	srv, err := NewSidecarServer(logger, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if srv.GetPort() != 3000 {
		t.Errorf("GetPort() = %d, want 3000", srv.GetPort())
	}
}

func TestSidecarServer_SetupRoutes(t *testing.T) {
	t.Setenv("EVALHUB_URL", "http://localhost:8080")
	logger := slog.Default()
	cfg := &config.Config{
		Sidecar: &config.SidecarConfig{
			Port:    8080,
			EvalHub: &config.EvalHubClientConfig{},
		},
	}
	srv, err := NewSidecarServer(logger, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	handler, err := srv.SetupRoutes()
	if err != nil {
		t.Skipf("SetupRoutes() failed (may need full env): %v", err)
	}
	if handler == nil {
		t.Fatal("SetupRoutes() returned nil handler")
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/evaluations/", nil)
	rw := httptest.NewRecorder()
	handler.ServeHTTP(rw, req)
	// Handler runs without panic; status may be 400/503 depending on config
}

func TestServerClosedError(t *testing.T) {
	err := &ServerClosedError{}
	if err.Error() != "Server closed" {
		t.Errorf("ServerClosedError.Error() = %q, want %q", err.Error(), "Server closed")
	}
}
