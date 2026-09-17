package server_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	sidecarServer "github.com/eval-hub/eval-hub/internal/eval_runtime_sidecar/server"
)

func TestNewSidecarServer(t *testing.T) {
	logger := slog.Default()

	t.Run("returns error when logger is nil", func(t *testing.T) {
		cfg := &config.Config{}
		_, err := sidecarServer.NewSidecarServer(nil, cfg)
		if err == nil {
			t.Fatal("expected error when logger is nil")
		}
		if err.Error() != "logger is required for the server" {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("returns error when config is nil", func(t *testing.T) {
		_, err := sidecarServer.NewSidecarServer(logger, nil)
		if err == nil {
			t.Fatal("expected error when config is nil")
		}
		if err.Error() != "service config is required for the sidecar server" {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("returns error when sidecar config is nil", func(t *testing.T) {
		cfg := &config.Config{}
		_, err := sidecarServer.NewSidecarServer(logger, cfg)
		if err == nil {
			t.Fatal("expected error when sidecar config is nil")
		}
		if err.Error() != "sidecar config is required" {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("uses port from Sidecar.BaseURL when set", func(t *testing.T) {
		sc := &config.SidecarConfig{BaseURL: "http://localhost:9090"}
		if err := sc.ResolvePort(); err != nil {
			t.Fatal(err)
		}
		cfg := &config.Config{Sidecar: sc}
		srv, err := sidecarServer.NewSidecarServer(logger, cfg)
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
	sc := &config.SidecarConfig{BaseURL: "http://localhost:3000"}
	if err := sc.ResolvePort(); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Sidecar: sc}
	srv, err := sidecarServer.NewSidecarServer(logger, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if srv.GetPort() != 3000 {
		t.Errorf("GetPort() = %d, want 3000", srv.GetPort())
	}
}

func TestSidecarServer_SetupRoutes(t *testing.T) {
	logger := slog.Default()
	cfg := &config.Config{
		Sidecar: &config.SidecarConfig{
			BaseURL: "http://localhost:8080",
			EvalHub: &config.EvalHubClientConfig{
				BaseURL:            "http://localhost:8080",
				InsecureSkipVerify: true,
			},
		},
	}
	srv, err := sidecarServer.NewSidecarServer(logger, cfg)
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

func TestSidecarServer_HealthEndpoint(t *testing.T) {
	logger := slog.Default()
	cfg := &config.Config{
		Sidecar: &config.SidecarConfig{
			BaseURL: "http://localhost:8080",
			EvalHub: &config.EvalHubClientConfig{
				BaseURL:            "http://localhost:8080",
				InsecureSkipVerify: true,
			},
		},
	}
	srv, err := sidecarServer.NewSidecarServer(logger, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	handler, err := srv.SetupRoutes()
	if err != nil {
		t.Skipf("SetupRoutes() failed (may need full env): %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rw := httptest.NewRecorder()
	handler.ServeHTTP(rw, req)
	if rw.Code != http.StatusOK {
		t.Errorf("GET /health status = %d, want 200", rw.Code)
	}
}

func TestSidecarServer_ShutdownBeforeStart(t *testing.T) {
	logger := slog.Default()
	cfg := &config.Config{
		Sidecar: &config.SidecarConfig{
			BaseURL: "http://localhost:8082",
			EvalHub: &config.EvalHubClientConfig{
				BaseURL:            "http://localhost:8080",
				InsecureSkipVerify: true,
			},
		},
	}
	srv, err := sidecarServer.NewSidecarServer(logger, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Run("shutdown before start does not panic", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		err := srv.Shutdown(ctx)
		if err != nil {
			t.Fatalf("Shutdown before Start returned error: %v", err)
		}
	})

	t.Run("start after pending shutdown returns without blocking", func(t *testing.T) {
		done := make(chan error, 1)
		go func() {
			done <- srv.Start()
		}()
		select {
		case err := <-done:
			if !errors.Is(err, &sidecarServer.ServerClosedError{}) {
				t.Fatalf("Start after Shutdown = %v, want ServerClosedError", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Start blocked despite a pending shutdown")
		}
	})
}

func TestSidecarServer_ConcurrentShutdownAndStart(t *testing.T) {
	logger := slog.Default()
	cfg := &config.Config{
		Sidecar: &config.SidecarConfig{
			BaseURL: "http://localhost:8083",
			EvalHub: &config.EvalHubClientConfig{
				BaseURL:            "http://localhost:8080",
				InsecureSkipVerify: true,
			},
		},
	}
	srv, err := sidecarServer.NewSidecarServer(logger, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	startDone := make(chan error, 1)
	go func() {
		startDone <- srv.Start()
	}()

	// Give Start a moment to begin, then race Shutdown against it.
	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown returned error: %v", err)
	}

	select {
	case err := <-startDone:
		if err != nil && !errors.Is(err, &sidecarServer.ServerClosedError{}) {
			t.Fatalf("Start returned unexpected error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Start did not return after Shutdown")
	}
}

func TestServerClosedError(t *testing.T) {
	err := &sidecarServer.ServerClosedError{}
	if err.Error() != "Sidecar server closed" {
		t.Errorf("ServerClosedError.Error() = %q, want %q", err.Error(), "Sidecar server closed")
	}
	if !errors.Is(err, &sidecarServer.ServerClosedError{}) {
		t.Error("errors.Is should match two distinct ServerClosedError pointers")
	}
}
