package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const minimalJobBody = `{
	"name": "test-evaluation-job",
	"model": {"url": "http://test.com", "name": "test"},
	"benchmarks": [{"id": "arc_easy", "provider_id": "lm_evaluation_harness"}]
}`

func TestClusterModeRequiresIdentityHeaders(t *testing.T) {
	srv, err := createServerWithLocalMode(t, 8080, false)
	if err != nil {
		t.Fatalf("createServerWithLocalMode: %v", err)
	}
	handler, err := srv.SetupRoutes()
	if err != nil {
		t.Fatalf("SetupRoutes: %v", err)
	}

	t.Run("health does not require identity headers", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("health without headers: got status %d body %s", w.Code, w.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode health body: %v", err)
		}
		if body["status"] != "healthy" {
			t.Fatalf("health status: got %v want healthy", body["status"])
		}
		for _, field := range []string{"build", "build_date", "git_hash", "version"} {
			if _, ok := body[field]; ok {
				t.Fatalf("health must not expose %q", field)
			}
		}
	})

	t.Run("missing X-Tenant returns 400", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/evaluations/jobs", strings.NewReader(minimalJobBody))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("got status %d body %s", w.Code, w.Body.String())
		}
		assertMessageCode(t, w, "missing_tenant_header")
	})

	t.Run("missing X-User returns 400", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/evaluations/jobs", strings.NewReader(minimalJobBody))
		req.Header.Set("X-Tenant", "test-tenant")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("got status %d body %s", w.Code, w.Body.String())
		}
		assertMessageCode(t, w, "missing_user_header")
	})

	t.Run("with X-Tenant and X-User accepts job", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/evaluations/jobs", strings.NewReader(minimalJobBody))
		req.Header.Set("X-Tenant", "test-tenant")
		req.Header.Set("X-User", "test-user")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusAccepted {
			t.Fatalf("got status %d body %s", w.Code, w.Body.String())
		}
	})

	t.Run("GET providers requires identity headers", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/evaluations/providers", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("got status %d body %s", w.Code, w.Body.String())
		}
		assertMessageCode(t, w, "missing_tenant_header")
	})
}

func TestLocalModeDoesNotRequireIdentityHeaders(t *testing.T) {
	srv, err := createServerWithLocalMode(t, 8080, true)
	if err != nil {
		t.Fatalf("createServerWithLocalMode: %v", err)
	}
	handler, err := srv.SetupRoutes()
	if err != nil {
		t.Fatalf("SetupRoutes: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/evaluations/jobs", strings.NewReader(minimalJobBody))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("local mode: got status %d body %s", w.Code, w.Body.String())
	}
}

func assertMessageCode(t *testing.T, w *httptest.ResponseRecorder, want string) {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v raw=%s", err, w.Body.String())
	}
	got, _ := body["message_code"].(string)
	if got != want {
		t.Fatalf("message_code: got %q want %q body %s", got, want, w.Body.String())
	}
}
