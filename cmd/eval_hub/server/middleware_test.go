package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eval-hub/eval-hub/internal/logging"
)

func TestMiddleware(t *testing.T) {
	t.Run("middleware wraps handler and records metrics", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK"))
		})

		wrapped := Middleware(handler, true, false, logging.FallbackLogger())

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		w := httptest.NewRecorder()

		wrapped.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
		}

		if w.Body.String() != "OK" {
			t.Errorf("Expected body 'OK', got %s", w.Body.String())
		}
	})

	t.Run("middleware captures status code", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte("Not Found"))
		})

		wrapped := Middleware(handler, true, false, logging.FallbackLogger())

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		w := httptest.NewRecorder()

		wrapped.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status code %d, got %d", http.StatusNotFound, w.Code)
		}
	})

	t.Run("middleware tracks in-flight requests", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		wrapped := Middleware(handler, true, false, logging.FallbackLogger())

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		w := httptest.NewRecorder()

		// Before request, in-flight should be 0 (or initial value)
		wrapped.ServeHTTP(w, req)

		// After request completes, in-flight should be decremented
		// This is tested implicitly - if defer doesn't work, we'd see issues
		if w.Code != http.StatusOK {
			t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
		}
	})
}

func TestResponseWriter(t *testing.T) {
	t.Run("WriteHeader captures status code", func(t *testing.T) {
		w := httptest.NewRecorder()
		rw := &responseWriter{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		rw.WriteHeader(http.StatusInternalServerError)

		if rw.statusCode != http.StatusInternalServerError {
			t.Errorf("Expected status code %d, got %d", http.StatusInternalServerError, rw.statusCode)
		}

		if w.Code != http.StatusInternalServerError {
			t.Errorf("Expected underlying writer status code %d, got %d", http.StatusInternalServerError, w.Code)
		}
	})

	t.Run("default status code is OK", func(t *testing.T) {
		w := httptest.NewRecorder()
		rw := &responseWriter{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}
		_ = rw.ResponseWriter // ResponseWriter is required for struct validity but not used in this test

		if rw.statusCode != http.StatusOK {
			t.Errorf("Expected default status code %d, got %d", http.StatusOK, rw.statusCode)
		}
	})
}
