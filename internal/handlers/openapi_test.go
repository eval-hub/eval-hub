package handlers_test

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/handlers"
)

func TestHandleOpenAPI(t *testing.T) {
	h := handlers.New(nil, nil, nil, nil, nil, nil)

	// Ensure the OpenAPI file exists for testing
	apiPath := filepath.Join("..", "..", "docs", "openapi.yaml")
	if _, err := os.Stat(apiPath); os.IsNotExist(err) {
		// Try alternative path
		apiPath = "docs/openapi.yaml"
		if _, err := os.Stat(apiPath); os.IsNotExist(err) {
			t.Skip("OpenAPI spec file not found, skipping test")
		}
	}

	t.Run("GET request returns OpenAPI spec", func(t *testing.T) {
		ctx := createExecutionContext()
		w := httptest.NewRecorder()

		h.HandleOpenAPI(ctx, createMockRequest("GET", "/openapi.yaml"), &MockResponseWrapper{w})

		if w.Code != 200 {
			t.Errorf("Expected status code %d, got %d", 200, w.Code)
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

	t.Run("JSON content type when Accept header is application/json", func(t *testing.T) {
		ctx := createExecutionContext()
		req := createMockRequest("GET", "/openapi.yaml")
		req.SetHeader("Accept", "application/json")
		w := httptest.NewRecorder()

		h.HandleOpenAPI(ctx, req, &MockResponseWrapper{w})

		contentType := w.Header().Get("Content-Type")
		if contentType != "application/json" {
			t.Errorf("Expected Content-Type application/json, got %s", contentType)
		}
	})
}

func TestHandleDocs(t *testing.T) {
	h := handlers.New(nil, nil, nil, nil, nil, nil)

	t.Run("GET request returns HTML documentation", func(t *testing.T) {
		ctx := createExecutionContext()
		w := httptest.NewRecorder()

		h.HandleDocs(ctx, createMockRequest("GET", "/docs"), &MockResponseWrapper{w})

		if w.Code != 200 {
			t.Errorf("Expected status code %d, got %d", 200, w.Code)
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

}
