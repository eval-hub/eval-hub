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

	rp := NewModelReverseProxy(target, &http.Client{}, log, secretDir, "")

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
	base := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	// Callers pass a logger pre-enriched with request_id; simulate that here.
	log := base.With("request_id", "resolve-req-id")

	secretDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(secretDir, "api-key"), []byte("sk-real-key"), 0600); err != nil {
		t.Fatal(err)
	}

	target, _ := url.Parse("https://model.example.com/v1")
	cache := loadSecretCache(secretDir, base)
	_, _, err := resolveModelCredential(log, "Bearer api-key:ref", cache, target, "")
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

	rp := NewModelReverseProxy(target, &http.Client{}, log, secretDir, "")

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

	rp := NewModelReverseProxy(target, &http.Client{}, slog.New(slog.NewTextHandler(io.Discard, nil)), secretDir, "")

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

	rp := NewModelReverseProxy(defaultTarget, &http.Client{}, slog.New(slog.NewTextHandler(io.Discard, nil)), secretDir, "")

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
	rp := NewModelReverseProxy(target, &http.Client{}, slog.New(slog.NewTextHandler(io.Discard, nil)), t.TempDir(), "")

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

	rp := NewModelReverseProxy(target, &http.Client{}, slog.New(slog.NewTextHandler(io.Discard, nil)), secretDir, "")

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer api-key:ref")
	rr := httptest.NewRecorder()
	rp.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty credential, got %d", rr.Code)
	}
}

// TestModelProxySATokenInjectedWhenNoAuth verifies that when the adapter sends no Authorization
// header, the sidecar injects the SA token as a Bearer token before forwarding to the model.
func TestModelProxySATokenInjectedWhenNoAuth(t *testing.T) {
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	saTokenDir := t.TempDir()
	saTokenPath := filepath.Join(saTokenDir, "token")
	if err := os.WriteFile(saTokenPath, []byte("sa-token-from-sidecar"), 0600); err != nil {
		t.Fatal(err)
	}

	target, _ := url.Parse(upstream.URL)
	rp := NewModelReverseProxy(target, &http.Client{}, slog.New(slog.NewTextHandler(io.Discard, nil)), t.TempDir(), saTokenPath)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	// No Authorization header set — simulates adapter with no SA token access.
	rr := httptest.NewRecorder()
	rp.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if gotAuth != "Bearer sa-token-from-sidecar" {
		t.Fatalf("expected SA token injected, got %q", gotAuth)
	}
}

// TestModelProxySATokenInjectedWhenBareBearer verifies that "Authorization: Bearer" (no
// trailing space — what Go's HTTP parser stores when Python sends "Bearer ") triggers SA
// token injection. This is the primary on-wire form when OPENAI_API_KEY is unset.
func TestModelProxySATokenInjectedWhenBareBearer(t *testing.T) {
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	saTokenDir := t.TempDir()
	saTokenPath := filepath.Join(saTokenDir, "token")
	if err := os.WriteFile(saTokenPath, []byte("sa-token-from-sidecar"), 0600); err != nil {
		t.Fatal(err)
	}

	target, _ := url.Parse(upstream.URL)
	rp := NewModelReverseProxy(target, &http.Client{}, slog.New(slog.NewTextHandler(io.Discard, nil)), t.TempDir(), saTokenPath)

	req := httptest.NewRequest(http.MethodPost, "/v1/completions", nil)
	// Go HTTP parser strips trailing space: Python's "Bearer " arrives as "Bearer".
	req.Header.Set("Authorization", "Bearer")
	rr := httptest.NewRecorder()
	rp.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if gotAuth != "Bearer sa-token-from-sidecar" {
		t.Fatalf("expected SA token injected for bare Bearer, got %q", gotAuth)
	}
}

// TestModelProxySATokenInjectedWhenEmptyBearer verifies that "Authorization: Bearer " (empty
// Bearer value, sent by lm-eval when OPENAI_API_KEY is unset) is treated as absent auth and
// the SA token is injected. This is the real SA-token-auth path from the adapter.
func TestModelProxySATokenInjectedWhenEmptyBearer(t *testing.T) {
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	saTokenDir := t.TempDir()
	saTokenPath := filepath.Join(saTokenDir, "token")
	if err := os.WriteFile(saTokenPath, []byte("sa-token-from-sidecar"), 0600); err != nil {
		t.Fatal(err)
	}

	target, _ := url.Parse(upstream.URL)
	rp := NewModelReverseProxy(target, &http.Client{}, slog.New(slog.NewTextHandler(io.Discard, nil)), t.TempDir(), saTokenPath)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer ") // lm-eval sends this when OPENAI_API_KEY=""
	rr := httptest.NewRecorder()
	rp.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if gotAuth != "Bearer sa-token-from-sidecar" {
		t.Fatalf("expected SA token injected for empty Bearer, got %q", gotAuth)
	}
}

func TestIsBearerEmpty(t *testing.T) {
	cases := []struct {
		header string
		want   bool
	}{
		{"", true},
		{"Bearer", true}, // Go HTTP parser strips trailing space from "Bearer " sent by Python requests
		{"Bearer ", true},
		{"Bearer  ", true},
		{"Bearer \t", true},
		{"Bearer sk-real", false},
		{"Bearer api-key:ref", false},
		{"Token abc", false},
	}
	for _, tc := range cases {
		if got := isBearerEmpty(tc.header); got != tc.want {
			t.Errorf("isBearerEmpty(%q) = %v, want %v", tc.header, got, tc.want)
		}
	}
}

// TestModelProxySATokenNotInjectedWhenAuthPresent verifies that an explicit Authorization
// header from the adapter is forwarded unchanged even when a SA token path is configured.
func TestModelProxySATokenNotInjectedWhenAuthPresent(t *testing.T) {
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	saTokenDir := t.TempDir()
	saTokenPath := filepath.Join(saTokenDir, "token")
	if err := os.WriteFile(saTokenPath, []byte("sa-token-should-not-be-used"), 0600); err != nil {
		t.Fatal(err)
	}

	target, _ := url.Parse(upstream.URL)
	rp := NewModelReverseProxy(target, &http.Client{}, slog.New(slog.NewTextHandler(io.Discard, nil)), t.TempDir(), saTokenPath)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer sk-explicit-key")
	rr := httptest.NewRecorder()
	rp.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if gotAuth != "Bearer sk-explicit-key" {
		t.Fatalf("expected explicit auth forwarded unchanged, got %q", gotAuth)
	}
}

// TestModelProxySATokenSuffixInjectsSATokenWhenEmpty verifies the KFP path:
// secret has "kfp_sa_token: """ (empty) — sidecar injects the SA token and routes to kfp_url.
func TestModelProxySATokenSuffixInjectsSATokenWhenEmpty(t *testing.T) {
	// Clear the shared SA token cache so we read from the file written by this test.
	UpdateCachedToken(AuthTokenInput{TargetEndpoint: "model-sa"}, "")
	var gotAuth, gotHost string
	kfpUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotHost = r.Host
		w.WriteHeader(http.StatusOK)
	}))
	defer kfpUpstream.Close()

	defaultUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer defaultUpstream.Close()

	saTokenDir := t.TempDir()
	saTokenPath := filepath.Join(saTokenDir, "token")
	if err := os.WriteFile(saTokenPath, []byte("sa-token-injected"), 0600); err != nil {
		t.Fatal(err)
	}

	secretDir := t.TempDir()
	writeFile := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(secretDir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// kfp_sa_token is intentionally empty — SA token should be injected.
	writeFile("kfp_sa_token", "")
	writeFile("kfp_url", kfpUpstream.URL)

	defaultTarget, _ := url.Parse(defaultUpstream.URL)
	rp := NewModelReverseProxy(defaultTarget, &http.Client{}, slog.New(slog.NewTextHandler(io.Discard, nil)), secretDir, saTokenPath)

	req := httptest.NewRequest(http.MethodGet, "/apis/v1beta1/runs", nil)
	req.Header.Set("Authorization", "Bearer kfp_sa_token:ref")
	rr := httptest.NewRecorder()
	rp.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if gotAuth != "Bearer sa-token-injected" {
		t.Fatalf("expected SA token injected, got %q", gotAuth)
	}
	kfpHost := strings.TrimPrefix(kfpUpstream.URL, "http://")
	if gotHost != kfpHost {
		t.Fatalf("expected request routed to kfp upstream %q, got host %q", kfpHost, gotHost)
	}
}

// TestModelProxySATokenSuffixUsesExplicitValueWhenNonEmpty verifies that when kfp_sa_token has a
// non-empty value (e.g. a user-provided JWT), it is forwarded as-is without SA injection.
func TestModelProxySATokenSuffixUsesExplicitValueWhenNonEmpty(t *testing.T) {
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	secretDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(secretDir, "kfp_sa_token"), []byte("user-provided-jwt"), 0600); err != nil {
		t.Fatal(err)
	}

	target, _ := url.Parse(upstream.URL)
	rp := NewModelReverseProxy(target, &http.Client{}, slog.New(slog.NewTextHandler(io.Discard, nil)), secretDir, "")

	req := httptest.NewRequest(http.MethodGet, "/apis/v1beta1/runs", nil)
	req.Header.Set("Authorization", "Bearer kfp_sa_token:ref")
	rr := httptest.NewRecorder()
	rp.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if gotAuth != "Bearer user-provided-jwt" {
		t.Fatalf("expected explicit token forwarded, got %q", gotAuth)
	}
}

// TestModelProxyReturns400OnURLKeyRef verifies that a *_url:ref Bearer token is rejected
// with 400. URL keys are routing hints — not credentials — and must never be forwarded
// as bearer tokens even if the key exists in the secret cache.
func TestModelProxyReturns400OnURLKeyRef(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	secretDir := t.TempDir()
	// kfp_url is present in cache — proxy must still reject it as a ref token.
	if err := os.WriteFile(filepath.Join(secretDir, "kfp_url"), []byte(upstream.URL), 0600); err != nil {
		t.Fatal(err)
	}

	target, _ := url.Parse(upstream.URL)
	rp := NewModelReverseProxy(target, &http.Client{}, slog.New(slog.NewTextHandler(io.Discard, nil)), secretDir, "")

	req := httptest.NewRequest(http.MethodGet, "/v1/completions", nil)
	req.Header.Set("Authorization", "Bearer kfp_url:ref")
	rr := httptest.NewRecorder()
	rp.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for _url ref key, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestModelProxySATokenSuffixReturns400WhenEmptyAndNoSAToken verifies that when kfp_sa_token is
// empty and no SA token is available, the proxy returns 400 rather than forwarding.
func TestModelProxySATokenSuffixReturns400WhenEmptyAndNoSAToken(t *testing.T) {
	// Clear the shared SA token cache so a stale token from a prior test doesn't mask the error.
	UpdateCachedToken(AuthTokenInput{TargetEndpoint: "model-sa"}, "")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	secretDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(secretDir, "kfp_sa_token"), []byte(""), 0600); err != nil {
		t.Fatal(err)
	}

	target, _ := url.Parse(upstream.URL)
	// No SA token path — simulates unavailable SA token.
	rp := NewModelReverseProxy(target, &http.Client{}, slog.New(slog.NewTextHandler(io.Discard, nil)), secretDir, "")

	req := httptest.NewRequest(http.MethodGet, "/apis/v1beta1/runs", nil)
	req.Header.Set("Authorization", "Bearer kfp_sa_token:ref")
	rr := httptest.NewRecorder()
	rp.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when _sa_token empty and no SA token, got %d", rr.Code)
	}
}
