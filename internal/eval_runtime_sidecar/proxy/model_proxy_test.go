package proxy

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetOrCreateRequestID(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(globalTransactionIDHeader, "incoming-req-id")
	if got := getOrCreateRequestID(req); got != "incoming-req-id" {
		t.Fatalf("expected header value, got %q", got)
	}

	generated := getOrCreateRequestID(httptest.NewRequest(http.MethodGet, "/", nil))
	if generated == "" {
		t.Fatal("expected generated request ID")
	}
}

func TestModelProxyLogsRequestID(t *testing.T) {
	var logBuf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	target, _ := url.Parse(upstream.URL)
	secretDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(secretDir, "api-key"), []byte("sk-real-key"), 0600); err != nil {
		t.Fatal(err)
	}

	rp := NewModelReverseProxy(target, &http.Client{}, log, secretDir)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set(globalTransactionIDHeader, "model-proxy-req-id")
	req.Header.Set("Authorization", "Bearer api-key:ref")
	rr := httptest.NewRecorder()
	rp.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !strings.Contains(logBuf.String(), "request_id=model-proxy-req-id") {
		t.Fatalf("logs = %q, want request_id field", logBuf.String())
	}
}

func TestResolveModelCredentialLogsRequestID(t *testing.T) {
	var logBuf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	secretDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(secretDir, "api-key"), []byte("sk-real-key"), 0600); err != nil {
		t.Fatal(err)
	}

	target, _ := url.Parse("https://model.example.com/v1")
	_, _, err := resolveModelCredential(log, "resolve-req-id", "Bearer api-key:ref", secretDir, target)
	if err != nil {
		t.Fatalf("resolveModelCredential: %v", err)
	}
	if !strings.Contains(logBuf.String(), "request_id=resolve-req-id") {
		t.Fatalf("logs = %q, want request_id field", logBuf.String())
	}
}

func TestModelProxyReturns400OnMissingRef(t *testing.T) {
	var logBuf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelError}))

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	target, _ := url.Parse(upstream.URL)
	secretDir := t.TempDir() // no files — ref key will be missing

	rp := NewModelReverseProxy(target, &http.Client{}, log, secretDir)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set(globalTransactionIDHeader, "cred-fail-req-id")
	req.Header.Set("Authorization", "Bearer api-key:ref")
	rr := httptest.NewRecorder()
	rp.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
	if got := rr.Header().Get(globalTransactionIDHeader); got != "cred-fail-req-id" {
		t.Fatalf("response %s = %q, want cred-fail-req-id", globalTransactionIDHeader, got)
	}
	if !strings.Contains(logBuf.String(), "request_id=cred-fail-req-id") {
		t.Fatalf("logs = %q, want request_id on credential failure", logBuf.String())
	}
}

func TestModelProxySingleModelResolves(t *testing.T) {
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	target, _ := url.Parse(upstream.URL)
	secretDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(secretDir, "api-key"), []byte("sk-real-key"), 0600); err != nil {
		t.Fatal(err)
	}

	rp := NewModelReverseProxy(target, &http.Client{}, slog.New(slog.NewTextHandler(io.Discard, nil)), secretDir)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer api-key:ref")
	rr := httptest.NewRecorder()
	rp.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if gotAuth != "Bearer sk-real-key" {
		t.Fatalf("expected Authorization %q, got %q", "Bearer sk-real-key", gotAuth)
	}
}

func TestModelProxyMultiModelRoutesToCorrectUpstream(t *testing.T) {
	var model1GotAuth, model2GotAuth string

	upstream1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		model1GotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream1.Close()

	upstream2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		model2GotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream2.Close()

	// defaultTarget is upstream1 (also what model-1 resolves to via _url file).
	defaultTarget, _ := url.Parse(upstream1.URL)
	secretDir := t.TempDir()

	writeFile := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(secretDir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeFile("model-1_api-key", "sk-model1")
	writeFile("model-1_url", upstream1.URL)
	writeFile("model-2_api-key", "sk-model2")
	writeFile("model-2_url", upstream2.URL)

	rp := NewModelReverseProxy(defaultTarget, &http.Client{}, slog.New(slog.NewTextHandler(io.Discard, nil)), secretDir)

	// Request for model-1 should go to upstream1 with model-1's real key.
	req1 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req1.Header.Set("Authorization", "Bearer model-1_api-key:ref")
	rr1 := httptest.NewRecorder()
	rp.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("model-1: expected 200, got %d", rr1.Code)
	}
	if model1GotAuth != "Bearer sk-model1" {
		t.Fatalf("model-1: expected auth %q, got %q", "Bearer sk-model1", model1GotAuth)
	}

	// Request for model-2 should go to upstream2 with model-2's real key.
	req2 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req2.Header.Set("Authorization", "Bearer model-2_api-key:ref")
	rr2 := httptest.NewRecorder()
	rp.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("model-2: expected 200, got %d", rr2.Code)
	}
	if model2GotAuth != "Bearer sk-model2" {
		t.Fatalf("model-2: expected auth %q, got %q", "Bearer sk-model2", model2GotAuth)
	}
}

func TestModelProxyNonRefTokenPassthrough(t *testing.T) {
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	target, _ := url.Parse(upstream.URL)
	rp := NewModelReverseProxy(target, &http.Client{}, slog.New(slog.NewTextHandler(io.Discard, nil)), t.TempDir())

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer sk-already-real")
	rr := httptest.NewRecorder()
	rp.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if gotAuth != "Bearer sk-already-real" {
		t.Fatalf("expected auth passed through unchanged, got %q", gotAuth)
	}
}

func TestModelProxyReturns400OnEmptyCredential(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	target, _ := url.Parse(upstream.URL)
	secretDir := t.TempDir()
	// Write empty file — credential is present but empty.
	if err := os.WriteFile(filepath.Join(secretDir, "api-key"), []byte("   "), 0600); err != nil {
		t.Fatal(err)
	}

	rp := NewModelReverseProxy(target, &http.Client{}, slog.New(slog.NewTextHandler(io.Discard, nil)), secretDir)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer api-key:ref")
	rr := httptest.NewRecorder()
	rp.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty credential, got %d", rr.Code)
	}
}
