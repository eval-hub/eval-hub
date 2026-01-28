package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/handlers"
)

func TestHandleOpenAPI(t *testing.T) {
	h := handlers.New(nil, nil)

	// Ensure the OpenAPI file exists for testing
	apiPath := filepath.Join("..", "..", "api", "openapi.yaml")
	if _, err := os.Stat(apiPath); os.IsNotExist(err) {
		// Try alternative path
		apiPath = "api/openapi.yaml"
		if _, err := os.Stat(apiPath); os.IsNotExist(err) {
			t.Skip("OpenAPI spec file not found, skipping test")
		}
	}

	t.Run("GET request returns OpenAPI spec", func(t *testing.T) {
		ctx := createExecutionContext(http.MethodGet, "/openapi.yaml")
		w := httptest.NewRecorder()

		h.HandleOpenAPI(ctx, w)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
		}

		contentType := w.Header().Get("Content-Type")
		if contentType != "application/yaml" && contentType != "application/json" {
			t.Errorf("Expected Content-Type application/yaml or application/json, got %s", contentType)
		}

		if len(w.Body.Bytes()) == 0 {
			t.Error("Response body is empty")
		}

		// Check if response contains OpenAPI keywords
		body := w.Body.String()
		if !strings.Contains(body, "openapi") && !strings.Contains(body, "OpenAPI") {
			t.Error("Response does not appear to be an OpenAPI specification")
		}
	})

	t.Run("POST request returns method not allowed", func(t *testing.T) {
		ctx := createExecutionContext(http.MethodPost, "/openapi.yaml")
		w := httptest.NewRecorder()

		h.HandleOpenAPI(ctx, w)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("Expected status code %d, got %d", http.StatusMethodNotAllowed, w.Code)
		}
	})

	t.Run("JSON content type when Accept header is application/json", func(t *testing.T) {
		ctx := createExecutionContext(http.MethodGet, "/openapi.yaml")
		ctx.Request.SetHeader("Accept", "application/json")
		w := httptest.NewRecorder()

		h.HandleOpenAPI(ctx, w)

		contentType := w.Header().Get("Content-Type")
		if contentType != "application/json" {
			t.Errorf("Expected Content-Type application/json, got %s", contentType)
		}
	})
}

func TestHandleDocs(t *testing.T) {
	h := handlers.New(nil, nil)

	t.Run("GET request returns HTML documentation", func(t *testing.T) {
		ctx := createExecutionContext(http.MethodGet, "/docs")
		w := httptest.NewRecorder()

		h.HandleDocs(ctx, w)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
		}

		contentType := w.Header().Get("Content-Type")
		if contentType != "text/html; charset=utf-8" {
			t.Errorf("Expected Content-Type text/html; charset=utf-8, got %s", contentType)
		}

		body := w.Body.String()
		if !strings.Contains(body, "swagger-ui") && !strings.Contains(body, "SwaggerUI") {
			t.Error("Response does not appear to be Swagger UI HTML")
		}

		if !strings.Contains(body, "openapi.yaml") {
			t.Error("Response does not reference openapi.yaml")
		}
	})

	t.Run("POST request returns method not allowed", func(t *testing.T) {
		ctx := createExecutionContext(http.MethodPost, "/docs")
		w := httptest.NewRecorder()

		h.HandleDocs(ctx, w)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("Expected status code %d, got %d", http.StatusMethodNotAllowed, w.Code)
		}
	})
}
