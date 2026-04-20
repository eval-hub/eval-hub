package handlers_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/handlers"
)

func TestHandleOpenAPI(t *testing.T) {
	h := handlers.New(nil, nil, nil, nil, nil)

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

func TestHandleOpenAPI_SpecNotFound(t *testing.T) {
	h := handlers.New(nil, nil, nil, nil, nil)

	base := t.TempDir()
	deep := filepath.Join(base, "a", "b", "c", "d")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })
	if err := os.Chdir(deep); err != nil {
		t.Fatal(err)
	}

	ctx := &executioncontext.ExecutionContext{
		Ctx:    context.Background(),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	w := httptest.NewRecorder()
	h.HandleOpenAPI(ctx, createMockRequest("GET", "/openapi.yaml"), &MockResponseWrapper{w})

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected application/json error body, got Content-Type %q", ct)
	}
}

func TestHandleDocs(t *testing.T) {
	h := handlers.New(nil, nil, nil, nil, nil)

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

		for _, key := range []string{"Cache-Control", "Pragma", "Expires"} {
			if w.Header().Get(key) == "" {
				t.Errorf("expected cache-disabling header %q to be set", key)
			}
		}
	})

	t.Run("SWAGGER_VERSION header selects Swagger UI asset version", func(t *testing.T) {
		ctx := createExecutionContext()
		req := createMockRequest("GET", "/docs")
		req.SetHeader("SWAGGER_VERSION", "9.8.7")
		w := httptest.NewRecorder()

		h.HandleDocs(ctx, req, &MockResponseWrapper{w})

		body := w.Body.String()
		if !strings.Contains(body, "swagger-ui-dist@9.8.7") {
			snippet := body
			if len(snippet) > 200 {
				snippet = snippet[:200]
			}
			t.Fatalf("expected custom Swagger UI version in HTML, got body snippet: %s", snippet)
		}
	})

	t.Run("SWAGGER_VERSION header can not insert malicious code", func(t *testing.T) {
		badScript := `<script>alert("Hello, world!");</script>`
		ctx := createExecutionContext()
		req := createMockRequest("GET", "/docs")
		req.SetHeader("SWAGGER_VERSION", badScript)
		w := httptest.NewRecorder()

		h.HandleDocs(ctx, req, &MockResponseWrapper{w})

		body := w.Body.String()
		if !strings.Contains(body, "swagger-ui-dist@&lt;script&gt;") {
			snippet := body
			if len(snippet) > 200 {
				snippet = snippet[:200]
			}
			t.Fatalf("expected custom Swagger UI version in HTML, got body snippet: %s", snippet)
		}
	})

	t.Run("base URL strips request path for spec URL", func(t *testing.T) {
		ctx := createExecutionContext()
		req := &MockRequest{
			TestMethod: "GET",
			TestURI:    "/api/v1/docs",
			TestPath:   "/docs",
			headers:    map[string]string{},
		}
		w := httptest.NewRecorder()

		h.HandleDocs(ctx, req, &MockResponseWrapper{w})

		body := w.Body.String()
		want := `url: "/api/v1/openapi.yaml"`
		if !strings.Contains(body, want) {
			t.Fatalf("expected %q in HTML, got: %s", want, body)
		}
	})
}
