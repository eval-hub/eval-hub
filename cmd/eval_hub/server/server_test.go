package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/cmd/eval_hub/server"
	"github.com/eval-hub/eval-hub/internal/config"
	"github.com/eval-hub/eval-hub/internal/logging"
	"github.com/eval-hub/eval-hub/internal/runtimes"
	"github.com/eval-hub/eval-hub/internal/storage"
	"github.com/eval-hub/eval-hub/internal/validation"
)

func TestNewServer(t *testing.T) {
	t.Run("creates server with default port", func(t *testing.T) {
		os.Unsetenv("PORT")
		srv, err := createServer(8080)
		if err != nil {
			t.Fatalf("NewServer() returned error: %v", err)
		}

		if srv == nil {
			t.Fatal("NewServer() returned nil")
		}

		if srv.GetPort() != 8080 {
			t.Errorf("Expected default port 8080, got %d", srv.GetPort())
		}
	})

	t.Run("creates server with custom port from environment", func(t *testing.T) {
		//os.Setenv("PORT", "9000")
		//defer os.Unsetenv("PORT")

		srv, err := createServer(9000)
		if err != nil {
			t.Fatalf("NewServer() returned error: %v", err)
		}

		if srv.GetPort() != 9000 {
			t.Errorf("Expected port 9000, got %d", srv.GetPort())
		}
	})
}

func TestServerSetupRoutes(t *testing.T) {
	srv, err := createServer(8080)
	if err != nil {
		t.Fatalf("NewServer() returned error: %v", err)
	}
	handler, err := srv.SetupRoutes()
	if err != nil {
		t.Fatalf("SetupRoutes() returned error: %v", err)
	}

	if handler == nil {
		t.Fatal("SetupRoutes() returned nil handler")
	}

	// NOTE: we do not want this code to become complex or
	// hard to maintain. The real test of the API is done
	// in the features tests (see the directory tests/features)

	// Test that routes are registered
	testCases := []struct {
		method string
		path   string
		status int
		body   string
	}{
		{http.MethodGet, "/api/v1/health", http.StatusOK, ""},
		{http.MethodGet, "/api/v1/status", http.StatusOK, ""},
		{http.MethodGet, "/metrics", http.StatusOK, ""},
		{http.MethodGet, "/openapi.yaml", http.StatusOK, ""},
		{http.MethodGet, "/docs", http.StatusOK, ""},
		// Evaluation endpoints
		{http.MethodPost, "/api/v1/evaluations/jobs", http.StatusAccepted, `{"model": {"url": "http://test.com", "name": "test"}}`},
		{http.MethodGet, "/api/v1/evaluations/jobs", http.StatusNotImplemented, ""},
		{http.MethodGet, "/api/v1/evaluations/jobs/test-id", http.StatusNotImplemented, ""},
		// we can not delete - it is done automatically below so no need to test it {http.MethodDelete, "/api/v1/evaluations/jobs/test-id", http.StatusOK, ""},
		{http.MethodGet, "/api/v1/evaluations/jobs/test-id/summary", http.StatusNotImplemented, ""},
		// Benchmarks
		{http.MethodGet, "/api/v1/evaluations/benchmarks", http.StatusOK, ""},
		// Collections
		{http.MethodGet, "/api/v1/evaluations/collections", http.StatusNotImplemented, ""},
		{http.MethodPost, "/api/v1/evaluations/collections", http.StatusNotImplemented, ""},
		{http.MethodGet, "/api/v1/evaluations/collections/test-collection", http.StatusNotImplemented, ""},
		{http.MethodPut, "/api/v1/evaluations/collections/test-collection", http.StatusNotImplemented, ""},
		{http.MethodPatch, "/api/v1/evaluations/collections/test-collection", http.StatusNotImplemented, ""},
		{http.MethodDelete, "/api/v1/evaluations/collections/test-collection", http.StatusNotImplemented, ""},
		// Providers
		{http.MethodGet, "/api/v1/evaluations/providers", http.StatusOK, ""},
		{http.MethodGet, "/api/v1/evaluations/providers/garak", http.StatusOK, ""},
		// System metrics
		{http.MethodGet, "/api/v1/metrics/system", http.StatusNotImplemented, ""},
		// Error cases
		{http.MethodPost, "/api/v1/health", http.StatusMethodNotAllowed, ""},
		{http.MethodGet, "/nonexistent", http.StatusNotFound, ""},
	}

	var evaluationIds []string

	for _, tc := range testCases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			var body io.Reader
			if tc.body != "" {
				body = strings.NewReader(tc.body)
			}
			req := httptest.NewRequest(tc.method, tc.path, body)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != tc.status {
				t.Errorf("Expected status %d for %s %s, got %d with message %s", tc.status, tc.method, tc.path, w.Code, w.Body.String())
			}

			if (tc.method == http.MethodPost) && (w.Body.String() != "") && (strings.HasPrefix(tc.path, "/api/v1/evaluations/jobs")) {
				var body map[string]interface{}
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
					t.Errorf("Failed to unmarshal body: %v", err)
				}
				if body["id"] != "" {
					evaluationIds = append(evaluationIds, body["id"].(string))
				} else {
					t.Errorf("Failed to find id in response body: %s", w.Body.String())
				}
			}
		})
	}

	for _, id := range evaluationIds {
		path := fmt.Sprintf("/api/v1/evaluations/jobs/%s", id)
		req := httptest.NewRequest(http.MethodDelete, path, nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusNoContent {
			t.Errorf("Expected status %d for %s when deleting evaluation job %s, got %d", http.StatusNoContent, path, id, w.Code)
		}
	}
}

func TestServerShutdown(t *testing.T) {
	t.Run("shutdown works with running server", func(t *testing.T) {
		srv, err := createServer(0) // Use random port for testing
		if err != nil {
			t.Fatalf("NewServer() returned error: %v", err)
		}

		// Start server in background
		errChan := make(chan error, 1)
		go func() {
			errChan <- srv.Start()
		}()

		// Wait a bit for server to start
		time.Sleep(100 * time.Millisecond)

		// Shutdown
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		err = srv.Shutdown(ctx)
		if err != nil {
			t.Errorf("Shutdown failed: %v", err)
		}

		// Wait for server to stop
		select {
		case err := <-errChan:
			if err != nil && err != http.ErrServerClosed {
				t.Errorf("Server error: %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Error("Server did not stop within timeout")
		}
	})
}

func createServer(port int) (*server.Server, error) {
	logger, _, err := logging.NewLogger()
	if err != nil {
		return nil, err
	}
	validate, err := validation.NewValidator()
	if err != nil {
		return nil, fmt.Errorf("failed to create validator: %w", err)
	}
	serviceConfig, err := config.LoadConfig(logger, "0.0.1", "local", time.Now().Format(time.RFC3339), "../../../tests")
	if err != nil {
		return nil, fmt.Errorf("failed to load service config: %w", err)
	}
	serviceConfig.Service.Port = port
	storage, err := storage.NewStorage(serviceConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create storage: %w", err)
	}
	// set up the provider configs
	providerConfigs, err := config.LoadProviderConfigs(logger, "../../../config/providers", "../../../../config/providers", "../../../../../config/providers")
	if err != nil {
		// we do this as no point trying to continue
		return nil, fmt.Errorf("failed to load provider configs: %w", err)
	}
	serviceConfig.Service.LocalMode = true // set local mode for testing
	runtime, err := runtimes.NewRuntime(logger, serviceConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create runtime: %w", err)
	}
	return server.NewServer(logger, serviceConfig, providerConfigs, storage, validate, runtime)
}
