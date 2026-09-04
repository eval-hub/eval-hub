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

func startModelTestUpstream(t *testing.T, handler http.HandlerFunc, useTLS bool) (*url.URL, *http.Client) {
	t.Helper()
	if useTLS {
		srv := httptest.NewTLSServer(handler)
		t.Cleanup(srv.Close)
		target, err := url.Parse(srv.URL)
		if err != nil {
			t.Fatal(err)
		}
		return target, srv.Client()
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	target, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return target, &http.Client{}
}

func TestModelProxyDropsAuthOnHTTPUpstream(t *testing.T) {
	var gotAuth string
	var logBuf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	target, client := startModelTestUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}, false)

	saTokenDir := t.TempDir()
	saTokenPath := filepath.Join(saTokenDir, "token")
	if err := os.WriteFile(saTokenPath, []byte("sa-token-from-sidecar"), 0600); err != nil {
		t.Fatal(err)
	}

	rp := NewModelReverseProxy(target, client, log, t.TempDir(), saTokenPath)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer local")
	rr := httptest.NewRecorder()
	rp.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if gotAuth != "" {
		t.Fatalf("expected no Authorization on HTTP upstream, got %q", gotAuth)
	}
	if !strings.Contains(logBuf.String(), "Dropping model Authorization header") {
		t.Fatalf("logs = %q, want auth drop warning", logBuf.String())
	}
	if !strings.Contains(logBuf.String(), "upstream URL uses HTTP") {
		t.Fatalf("logs = %q, want HTTP upstream warning", logBuf.String())
	}
}

func TestLoadSecretCache_OpenRootFails(t *testing.T) {
	t.Parallel()
	var logBuf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	missing := filepath.Join(t.TempDir(), "no-such-mount")
	cache := loadSecretCache(missing, log)
	if len(cache) != 0 {
		t.Fatalf("cache = %#v, want empty", cache)
	}
	if !strings.Contains(logBuf.String(), "model secret mount unreadable") {
		t.Fatalf("logs = %q, want unreadable mount warning", logBuf.String())
	}
}

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
	target, client := startModelTestUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}, true)
	secretDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(secretDir, "api-key"), []byte("sk-real-key"), 0600); err != nil {
		t.Fatal(err)
	}

	rp := NewModelReverseProxy(target, client, slog.New(slog.NewTextHandler(io.Discard, nil)), secretDir, "")

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

	upstream1 := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		model1GotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream1.Close)

	upstream2 := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		model2GotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream2.Close)

	// defaultTarget is upstream1 (also what model-1 resolves to via _url file).
	defaultTarget, _ := url.Parse(upstream1.URL)
	client := upstream1.Client()
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

	rp := NewModelReverseProxy(defaultTarget, client, slog.New(slog.NewTextHandler(io.Discard, nil)), secretDir, "")

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

func TestModelProxySATokenInjectedWhenPlaceholderToken(t *testing.T) {
	var gotAuth string
	target, client := startModelTestUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}, true)

	saTokenDir := t.TempDir()
	saTokenPath := filepath.Join(saTokenDir, "token")
	if err := os.WriteFile(saTokenPath, []byte("sa-token-from-sidecar"), 0600); err != nil {
		t.Fatal(err)
	}

	rp := NewModelReverseProxy(target, client, slog.New(slog.NewTextHandler(io.Discard, nil)), t.TempDir(), saTokenPath)

	for _, placeholder := range []string{"Bearer local", "Bearer sk-already-real"} {
		t.Run(placeholder, func(t *testing.T) {
			gotAuth = ""
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			req.Header.Set("Authorization", placeholder)
			rr := httptest.NewRecorder()
			rp.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", rr.Code)
			}
			if gotAuth != "Bearer sa-token-from-sidecar" {
				t.Fatalf("expected SA token injected for %q, got %q", placeholder, gotAuth)
			}
		})
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
	target, client := startModelTestUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}, true)

	saTokenDir := t.TempDir()
	saTokenPath := filepath.Join(saTokenDir, "token")
	if err := os.WriteFile(saTokenPath, []byte("sa-token-from-sidecar"), 0600); err != nil {
		t.Fatal(err)
	}

	rp := NewModelReverseProxy(target, client, slog.New(slog.NewTextHandler(io.Discard, nil)), t.TempDir(), saTokenPath)

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
	target, client := startModelTestUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}, true)

	saTokenDir := t.TempDir()
	saTokenPath := filepath.Join(saTokenDir, "token")
	if err := os.WriteFile(saTokenPath, []byte("sa-token-from-sidecar"), 0600); err != nil {
		t.Fatal(err)
	}

	rp := NewModelReverseProxy(target, client, slog.New(slog.NewTextHandler(io.Discard, nil)), t.TempDir(), saTokenPath)

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
	target, client := startModelTestUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}, true)

	saTokenDir := t.TempDir()
	saTokenPath := filepath.Join(saTokenDir, "token")
	if err := os.WriteFile(saTokenPath, []byte("sa-token-from-sidecar"), 0600); err != nil {
		t.Fatal(err)
	}

	rp := NewModelReverseProxy(target, client, slog.New(slog.NewTextHandler(io.Discard, nil)), t.TempDir(), saTokenPath)

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

func TestIsExplicitHardcodedToken(t *testing.T) {
	cases := []struct {
		header string
		want   bool
	}{
		{"Bearer token:my-secret", true},
		{"Bearer token:", true},
		{"Bearer local", false},
		{"Bearer api-key:ref", false},
		{"Bearer sk-real", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isExplicitHardcodedToken(tc.header); got != tc.want {
			t.Errorf("isExplicitHardcodedToken(%q) = %v, want %v", tc.header, got, tc.want)
		}
	}
}

func TestExtractExplicitHardcodedToken(t *testing.T) {
	cases := []struct {
		header string
		want   string
	}{
		{"Bearer token:my-secret", "my-secret"},
		{"Bearer token:", ""},
		{"Bearer local", ""},
	}
	for _, tc := range cases {
		if got := extractExplicitHardcodedToken(tc.header); got != tc.want {
			t.Errorf("extractExplicitHardcodedToken(%q) = %q, want %q", tc.header, got, tc.want)
		}
	}
}

func TestModelProxyExplicitHardcodedToken(t *testing.T) {
	var gotAuth string
	target, client := startModelTestUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}, true)

	saTokenDir := t.TempDir()
	saTokenPath := filepath.Join(saTokenDir, "token")
	if err := os.WriteFile(saTokenPath, []byte("sa-token-should-not-be-used"), 0600); err != nil {
		t.Fatal(err)
	}

	rp := NewModelReverseProxy(target, client, slog.New(slog.NewTextHandler(io.Discard, nil)), t.TempDir(), saTokenPath)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer token:adapter-hardcoded-secret")
	rr := httptest.NewRecorder()
	rp.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if gotAuth != "Bearer adapter-hardcoded-secret" {
		t.Fatalf("expected explicit hardcoded token forwarded, got %q", gotAuth)
	}
}

// TestModelProxySATokenSuffixInjectsSATokenWhenEmpty verifies the KFP path:
// secret has "kfp_sa_token: """ (empty) — sidecar injects the SA token and routes to kfp_url.
func TestModelProxySATokenSuffixInjectsSATokenWhenEmpty(t *testing.T) {
	// Clear the shared SA token cache so we read from the file written by this test.
	UpdateCachedToken(AuthTokenInput{TargetEndpoint: "model-sa"}, "")
	var gotAuth, gotHost string
	kfpUpstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotHost = r.Host
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(kfpUpstream.Close)

	defaultUpstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(defaultUpstream.Close)

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
	rp := NewModelReverseProxy(defaultTarget, defaultUpstream.Client(), slog.New(slog.NewTextHandler(io.Discard, nil)), secretDir, saTokenPath)

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
	kfpHost := strings.TrimPrefix(strings.TrimPrefix(kfpUpstream.URL, "https://"), "http://")
	if gotHost != kfpHost {
		t.Fatalf("expected request routed to kfp upstream %q, got host %q", kfpHost, gotHost)
	}
}

// TestModelProxySATokenSuffixUsesExplicitValueWhenNonEmpty verifies that when kfp_sa_token has a
// non-empty value (e.g. a user-provided JWT), it is forwarded as-is without SA injection.
func TestModelProxySATokenSuffixUsesExplicitValueWhenNonEmpty(t *testing.T) {
	var gotAuth string
	target, client := startModelTestUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}, true)

	secretDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(secretDir, "kfp_sa_token"), []byte("user-provided-jwt"), 0600); err != nil {
		t.Fatal(err)
	}

	rp := NewModelReverseProxy(target, client, slog.New(slog.NewTextHandler(io.Discard, nil)), secretDir, "")

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

func TestLoadSecretCache_EmptyMountPath(t *testing.T) {
	t.Parallel()
	cache := loadSecretCache("", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if len(cache) != 0 {
		t.Fatalf("cache = %#v, want empty", cache)
	}
}

func TestLoadSecretCache_SkipsDirectoriesAndLoadsFiles(t *testing.T) {
	t.Parallel()
	var logBuf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	secretDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(secretDir, "ignored-dir"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secretDir, "api-key"), []byte("sk-test"), 0600); err != nil {
		t.Fatal(err)
	}

	cache := loadSecretCache(secretDir, log)
	if cache["api-key"] != "sk-test" {
		t.Fatalf("cache = %#v, want api-key loaded", cache)
	}
	if _, ok := cache["ignored-dir"]; ok {
		t.Fatal("expected directories to be skipped")
	}
	if !strings.Contains(logBuf.String(), "Loaded model secret cache") {
		t.Fatalf("logs = %q, want cache loaded info", logBuf.String())
	}
}

func TestLoadSecretCache_SkipsUnreadableSecretFile(t *testing.T) {
	t.Parallel()
	var logBuf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	secretDir := t.TempDir()
	if err := os.Symlink(filepath.Join(secretDir, "missing-target"), filepath.Join(secretDir, "broken-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secretDir, "api-key"), []byte("sk-test"), 0600); err != nil {
		t.Fatal(err)
	}

	cache := loadSecretCache(secretDir, log)
	if cache["api-key"] != "sk-test" {
		t.Fatalf("cache = %#v, want readable secret loaded", cache)
	}
	if !strings.Contains(logBuf.String(), "skipping unreadable secret file") {
		t.Fatalf("logs = %q, want unreadable file warning", logBuf.String())
	}
}

func TestResolveModelCredentialRejectsInvalidRefKey(t *testing.T) {
	t.Parallel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	target, _ := url.Parse("https://model.example.com/v1")

	for _, authHeader := range []string{"Bearer :ref", `Bearer bad/key:ref`} {
		_, _, err := resolveModelCredential(log, authHeader, map[string]string{}, target, "")
		if err == nil {
			t.Fatalf("resolveModelCredential(%q) = nil, want error", authHeader)
		}
		if !strings.Contains(err.Error(), "invalid key") {
			t.Fatalf("error = %q, want invalid key", err.Error())
		}
	}
}

func TestResolveUpstreamURLInvalidFallsBackToDefault(t *testing.T) {
	t.Parallel()
	var logBuf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	defaultTarget, _ := url.Parse("https://default.example.com/v1")
	cache := map[string]string{
		"model-1_api-key": "sk-test",
		"model-1_url":     "://invalid",
	}

	got := resolveUpstreamURL(log, "model-1_api-key", cache, defaultTarget)
	if got.String() != defaultTarget.String() {
		t.Fatalf("got %q, want default %q", got, defaultTarget)
	}
	if !strings.Contains(logBuf.String(), "service URL in secret cache is invalid") {
		t.Fatalf("logs = %q, want invalid URL warning", logBuf.String())
	}
}

func TestModelProxyExplicitEmptyTokenPrefixOnHTTPS(t *testing.T) {
	var gotAuth string
	var logBuf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	target, client := startModelTestUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}, true)

	rp := NewModelReverseProxy(target, client, log, t.TempDir(), "")

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer token:")
	rr := httptest.NewRecorder()
	rp.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if gotAuth != "" {
		t.Fatalf("expected adapter Authorization stripped when token empty, got %q", gotAuth)
	}
	if !strings.Contains(logBuf.String(), "Explicit token: prefix with empty value") {
		t.Fatalf("logs = %q, want empty token warning", logBuf.String())
	}
}

func TestModelProxySATokenUnavailableOnHTTPS(t *testing.T) {
	var gotAuth string
	var logBuf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	target, client := startModelTestUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}, true)

	rp := NewModelReverseProxy(target, client, log, t.TempDir(), "")

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	rr := httptest.NewRecorder()
	rp.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if gotAuth != "" {
		t.Fatalf("expected no Authorization when SA token unavailable, got %q", gotAuth)
	}
	if !strings.Contains(logBuf.String(), "SA token injection skipped") {
		t.Fatalf("logs = %q, want SA token unavailable warning", logBuf.String())
	}
}

func TestModelProxyDropsCopiedAdapterAuthOnHTTPWithoutCredential(t *testing.T) {
	var gotAuth string
	var logBuf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	target, client := startModelTestUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}, false)

	rp := NewModelReverseProxy(target, client, log, t.TempDir(), "")

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer token:")
	rr := httptest.NewRecorder()
	rp.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if gotAuth != "" {
		t.Fatalf("expected adapter Authorization stripped on HTTP upstream, got %q", gotAuth)
	}
	if !strings.Contains(logBuf.String(), "Explicit token: prefix with empty value") {
		t.Fatalf("logs = %q, want empty token warning", logBuf.String())
	}
}
