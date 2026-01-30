package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eval-hub/eval-hub/internal/config"
)

// TestCorsMiddleware_HeadersSet verifies that all required CORS headers are set correctly
func TestCorsMiddleware_HeadersSet(t *testing.T) {
	testCases := []struct {
		name            string
		method          string
		expectedOrigin  string
		expectedMethods string
		expectedHeaders string
		expectedMaxAge  string
	}{
		{
			name:            "GET request has all CORS headers",
			method:          http.MethodGet,
			expectedOrigin:  "*",
			expectedMethods: "GET, POST, PUT, PATCH, DELETE, OPTIONS",
			expectedHeaders: "Content-Type, Authorization, X-Global-Transaction-Id",
			expectedMaxAge:  "3600",
		},
		{
			name:            "POST request has all CORS headers",
			method:          http.MethodPost,
			expectedOrigin:  "*",
			expectedMethods: "GET, POST, PUT, PATCH, DELETE, OPTIONS",
			expectedHeaders: "Content-Type, Authorization, X-Global-Transaction-Id",
			expectedMaxAge:  "3600",
		},
		{
			name:            "PUT request has all CORS headers",
			method:          http.MethodPut,
			expectedOrigin:  "*",
			expectedMethods: "GET, POST, PUT, PATCH, DELETE, OPTIONS",
			expectedHeaders: "Content-Type, Authorization, X-Global-Transaction-Id",
			expectedMaxAge:  "3600",
		},
		{
			name:            "DELETE request has all CORS headers",
			method:          http.MethodDelete,
			expectedOrigin:  "*",
			expectedMethods: "GET, POST, PUT, PATCH, DELETE, OPTIONS",
			expectedHeaders: "Content-Type, Authorization, X-Global-Transaction-Id",
			expectedMaxAge:  "3600",
		},
		{
			name:            "PATCH request has all CORS headers",
			method:          http.MethodPatch,
			expectedOrigin:  "*",
			expectedMethods: "GET, POST, PUT, PATCH, DELETE, OPTIONS",
			expectedHeaders: "Content-Type, Authorization, X-Global-Transaction-Id",
			expectedMaxAge:  "3600",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a mock handler that returns 200 OK
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("OK"))
			})

			// Wrap with CORS middleware
			wrapped := CorsMiddleware(handler, &config.Config{})

			// Create test request
			req := httptest.NewRequest(tc.method, "/test", nil)
			w := httptest.NewRecorder()

			// Serve the request
			wrapped.ServeHTTP(w, req)

			// Verify CORS headers
			if got := w.Header().Get("Access-Control-Allow-Origin"); got != tc.expectedOrigin {
				t.Errorf("Expected Access-Control-Allow-Origin %q, got %q", tc.expectedOrigin, got)
			}

			if got := w.Header().Get("Access-Control-Allow-Methods"); got != tc.expectedMethods {
				t.Errorf("Expected Access-Control-Allow-Methods %q, got %q", tc.expectedMethods, got)
			}

			if got := w.Header().Get("Access-Control-Allow-Headers"); got != tc.expectedHeaders {
				t.Errorf("Expected Access-Control-Allow-Headers %q, got %q", tc.expectedHeaders, got)
			}

			if got := w.Header().Get("Access-Control-Max-Age"); got != tc.expectedMaxAge {
				t.Errorf("Expected Access-Control-Max-Age %q, got %q", tc.expectedMaxAge, got)
			}
		})
	}
}

// TestCorsMiddleware_PreflightRequest verifies that OPTIONS requests are handled as preflight
func TestCorsMiddleware_PreflightRequest(t *testing.T) {
	t.Run("OPTIONS request returns 204 No Content", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// This should NOT be called for OPTIONS requests
			t.Error("Handler should not be called for OPTIONS requests")
			w.WriteHeader(http.StatusOK)
		})

		wrapped := CorsMiddleware(handler, &config.Config{})

		req := httptest.NewRequest(http.MethodOptions, "/test", nil)
		w := httptest.NewRecorder()

		wrapped.ServeHTTP(w, req)

		if w.Code != http.StatusNoContent {
			t.Errorf("Expected status code %d, got %d", http.StatusNoContent, w.Code)
		}
	})

	t.Run("OPTIONS request sets all CORS headers", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		wrapped := CorsMiddleware(handler, &config.Config{})

		req := httptest.NewRequest(http.MethodOptions, "/test", nil)
		w := httptest.NewRecorder()

		wrapped.ServeHTTP(w, req)

		// Verify all CORS headers are present
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
			t.Errorf("Expected Access-Control-Allow-Origin *, got %q", got)
		}

		if got := w.Header().Get("Access-Control-Allow-Methods"); got == "" {
			t.Error("Access-Control-Allow-Methods header not set")
		}

		if got := w.Header().Get("Access-Control-Allow-Headers"); got == "" {
			t.Error("Access-Control-Allow-Headers header not set")
		}

		if got := w.Header().Get("Access-Control-Max-Age"); got != "3600" {
			t.Errorf("Expected Access-Control-Max-Age 3600, got %q", got)
		}
	})

	t.Run("OPTIONS request returns empty body", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		wrapped := CorsMiddleware(handler, &config.Config{})

		req := httptest.NewRequest(http.MethodOptions, "/test", nil)
		w := httptest.NewRecorder()

		wrapped.ServeHTTP(w, req)

		if w.Body.String() != "" {
			t.Errorf("Expected empty body, got %q", w.Body.String())
		}
	})
}

// TestCorsMiddleware_PassThrough verifies that non-OPTIONS requests pass through correctly
func TestCorsMiddleware_PassThrough(t *testing.T) {
	t.Run("GET request calls next handler", func(t *testing.T) {
		called := false
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("test response"))
		})

		wrapped := CorsMiddleware(handler, &config.Config{})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		w := httptest.NewRecorder()

		wrapped.ServeHTTP(w, req)

		if !called {
			t.Error("Handler was not called for GET request")
		}

		if w.Code != http.StatusOK {
			t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
		}

		if w.Body.String() != "test response" {
			t.Errorf("Expected body 'test response', got %q", w.Body.String())
		}
	})

	t.Run("POST request calls next handler with body", func(t *testing.T) {
		called := false
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte("created"))
		})

		wrapped := CorsMiddleware(handler, &config.Config{})

		req := httptest.NewRequest(http.MethodPost, "/test", nil)
		w := httptest.NewRecorder()

		wrapped.ServeHTTP(w, req)

		if !called {
			t.Error("Handler was not called for POST request")
		}

		if w.Code != http.StatusCreated {
			t.Errorf("Expected status code %d, got %d", http.StatusCreated, w.Code)
		}

		if w.Body.String() != "created" {
			t.Errorf("Expected body 'created', got %q", w.Body.String())
		}
	})

	t.Run("CORS headers added while preserving other headers", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Custom-Header", "custom-value")
			w.WriteHeader(http.StatusOK)
		})

		wrapped := CorsMiddleware(handler, &config.Config{})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		w := httptest.NewRecorder()

		wrapped.ServeHTTP(w, req)

		// Verify CORS headers are present
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
			t.Errorf("Expected Access-Control-Allow-Origin *, got %q", got)
		}

		// Verify other headers are preserved
		if got := w.Header().Get("Content-Type"); got != "application/json" {
			t.Errorf("Expected Content-Type application/json, got %q", got)
		}

		if got := w.Header().Get("X-Custom-Header"); got != "custom-value" {
			t.Errorf("Expected X-Custom-Header custom-value, got %q", got)
		}
	})

	t.Run("Response status code from next handler is preserved", func(t *testing.T) {
		testCases := []struct {
			name       string
			statusCode int
		}{
			{"200 OK", http.StatusOK},
			{"201 Created", http.StatusCreated},
			{"400 Bad Request", http.StatusBadRequest},
			{"404 Not Found", http.StatusNotFound},
			{"500 Internal Server Error", http.StatusInternalServerError},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(tc.statusCode)
				})

				wrapped := CorsMiddleware(handler, &config.Config{})

				req := httptest.NewRequest(http.MethodGet, "/test", nil)
				w := httptest.NewRecorder()

				wrapped.ServeHTTP(w, req)

				if w.Code != tc.statusCode {
					t.Errorf("Expected status code %d, got %d", tc.statusCode, w.Code)
				}
			})
		}
	})
}

// TestCorsMiddleware_EdgeCases verifies edge cases and unusual scenarios
func TestCorsMiddleware_EdgeCases(t *testing.T) {
	t.Run("handler can overwrite CORS headers", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Handler sets different CORS header - this will overwrite middleware's header
			// This is expected behavior: middleware sets headers first, then handler runs
			w.Header().Set("Access-Control-Allow-Origin", "http://example.com")
			w.WriteHeader(http.StatusOK)
		})

		wrapped := CorsMiddleware(handler, &config.Config{})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		w := httptest.NewRecorder()

		wrapped.ServeHTTP(w, req)

		// Handler's header overwrites middleware's header (handler runs after middleware sets headers)
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://example.com" {
			t.Errorf("Expected Access-Control-Allow-Origin http://example.com, got %q", got)
		}
	})

	t.Run("multiple sequential OPTIONS requests", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		wrapped := CorsMiddleware(handler, &config.Config{})

		for i := range 3 {
			req := httptest.NewRequest(http.MethodOptions, "/test", nil)
			w := httptest.NewRecorder()

			wrapped.ServeHTTP(w, req)

			if w.Code != http.StatusNoContent {
				t.Errorf("Request %d: Expected status code %d, got %d", i+1, http.StatusNoContent, w.Code)
			}

			if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
				t.Errorf("Request %d: Expected Access-Control-Allow-Origin *, got %q", i+1, got)
			}
		}
	})

	t.Run("middleware does not panic with nil config", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		// Pass nil config - middleware currently doesn't use it but shouldn't panic
		wrapped := CorsMiddleware(handler, nil)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		w := httptest.NewRecorder()

		// Should not panic
		wrapped.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
		}
	})
}
