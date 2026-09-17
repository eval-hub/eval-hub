package proxy

import (
	"encoding/json"
	"encoding/pem"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseLocalModelPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path          string
		wantJobID     string
		wantRemaining string
		wantOK        bool
	}{
		{"/model/job-123/v1/chat/completions", "job-123", "/v1/chat/completions", true},
		{"/model/job-123/v1", "job-123", "/v1", true},
		{"/model/job-123/", "job-123", "/", true},
		{"/model/job-123", "job-123", "", true},
		{"/model/abc-def-ghi/v1/models", "abc-def-ghi", "/v1/models", true},
		{"/model//", "", "", false},
		{"/model//v1/chat", "", "", false},
		{"/model/", "", "", false},
		{"/model", "", "", false},
		{"/models/job-123/v1", "", "", false},
		{"/other/path", "", "", false},
		{"", "", "", false},
	}
	for _, tt := range tests {
		jobID, remaining, ok := ParseLocalModelPath(tt.path)
		if ok != tt.wantOK {
			t.Errorf("ParseLocalModelPath(%q): ok = %v, want %v", tt.path, ok, tt.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if jobID != tt.wantJobID {
			t.Errorf("ParseLocalModelPath(%q): jobID = %q, want %q", tt.path, jobID, tt.wantJobID)
		}
		if remaining != tt.wantRemaining {
			t.Errorf("ParseLocalModelPath(%q): remaining = %q, want %q", tt.path, remaining, tt.wantRemaining)
		}
	}
}

func TestLocalModelProxy_ForwardsToUpstream(t *testing.T) {
	t.Parallel()

	var receivedPath, receivedAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result": "ok"}`))
	}))
	defer upstream.Close()

	jobsDir := t.TempDir()
	writeJobInfo(t, jobsDir, "job-1", `{
		"model": {"url": "`+upstream.URL+`"}
	}`)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cache := NewJobInfoCache(jobsDir, DefaultJobCacheTTL, logger)
	proxy := NewLocalModelReverseProxy(cache, logger)

	req := httptest.NewRequest(http.MethodPost, "/model/job-1/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer sk-adapter-set")
	rw := httptest.NewRecorder()
	proxy.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body = %s", rw.Code, rw.Body.String())
	}
	if receivedPath != "/v1/chat/completions" {
		t.Errorf("upstream path = %q, want /v1/chat/completions", receivedPath)
	}
	if receivedAuth != "Bearer sk-adapter-set" {
		t.Errorf("upstream auth = %q, want Bearer sk-adapter-set (adapter-provided auth should pass through)", receivedAuth)
	}
}

func TestLocalModelProxy_NoAuthWithoutAdapterHeader(t *testing.T) {
	t.Parallel()

	var receivedAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	jobsDir := t.TempDir()
	writeJobInfo(t, jobsDir, "job-noauth", `{
		"model": {"url": "`+upstream.URL+`"}
	}`)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cache := NewJobInfoCache(jobsDir, DefaultJobCacheTTL, logger)
	proxy := NewLocalModelReverseProxy(cache, logger)

	req := httptest.NewRequest(http.MethodPost, "/model/job-noauth/v1/completions", nil)
	rw := httptest.NewRecorder()
	proxy.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rw.Code)
	}
	if receivedAuth != "" {
		t.Errorf("upstream auth = %q, want empty (no auth injection in local mode)", receivedAuth)
	}
}

func TestLocalModelProxy_UnknownJobReturns404(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cache := NewJobInfoCache(t.TempDir(), DefaultJobCacheTTL, logger)
	proxy := NewLocalModelReverseProxy(cache, logger)

	req := httptest.NewRequest(http.MethodGet, "/model/nonexistent-job/v1/models", nil)
	rw := httptest.NewRecorder()
	proxy.ServeHTTP(rw, req)

	if rw.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rw.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(rw.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["error"] != "unknown job-id" {
		t.Errorf("error = %q, want %q", body["error"], "unknown job-id")
	}
	if body["job_id"] != "nonexistent-job" {
		t.Errorf("job_id = %q, want %q", body["job_id"], "nonexistent-job")
	}
}

func TestLocalModelProxy_InvalidPathReturns400(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cache := NewJobInfoCache(t.TempDir(), DefaultJobCacheTTL, logger)
	proxy := NewLocalModelReverseProxy(cache, logger)

	// Path with no job-id (just /model/)
	req := httptest.NewRequest(http.MethodGet, "/model/", nil)
	rw := httptest.NewRecorder()
	proxy.ServeHTTP(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rw.Code)
	}
}

func TestLocalModelProxy_StripsJobIDPrefix(t *testing.T) {
	t.Parallel()

	paths := []struct {
		requestPath  string
		expectedPath string
	}{
		{"/model/j1/v1/chat/completions", "/v1/chat/completions"},
		{"/model/j1/v1", "/v1"},
		{"/model/j1/", "/"},
		{"/model/j1", "/"},
	}

	for _, tc := range paths {
		var receivedPath string
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedPath = r.URL.Path
			w.WriteHeader(http.StatusOK)
		}))

		jobsDir := t.TempDir()
		writeJobInfo(t, jobsDir, "j1", `{"model": {"url": "`+upstream.URL+`"}}`)

		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		cache := NewJobInfoCache(jobsDir, DefaultJobCacheTTL, logger)
		p := NewLocalModelReverseProxy(cache, logger)

		req := httptest.NewRequest(http.MethodGet, tc.requestPath, nil)
		rw := httptest.NewRecorder()
		p.ServeHTTP(rw, req)
		upstream.Close()

		if rw.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", tc.requestPath, rw.Code)
			continue
		}
		if receivedPath != tc.expectedPath {
			t.Errorf("%s: upstream path = %q, want %q", tc.requestPath, receivedPath, tc.expectedPath)
		}
	}
}

func TestLocalModelProxy_MultipleJobsRoutedIndependently(t *testing.T) {
	t.Parallel()

	var receivedPaths []string
	upstream1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPaths = append(receivedPaths, "upstream1:"+r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream1.Close()

	upstream2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPaths = append(receivedPaths, "upstream2:"+r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream2.Close()

	jobsDir := t.TempDir()
	writeJobInfo(t, jobsDir, "job-a", `{"model": {"url": "`+upstream1.URL+`"}}`)
	writeJobInfo(t, jobsDir, "job-b", `{"model": {"url": "`+upstream2.URL+`"}}`)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cache := NewJobInfoCache(jobsDir, DefaultJobCacheTTL, logger)
	proxy := NewLocalModelReverseProxy(cache, logger)

	req1 := httptest.NewRequest(http.MethodPost, "/model/job-a/v1/chat", nil)
	rw1 := httptest.NewRecorder()
	proxy.ServeHTTP(rw1, req1)

	req2 := httptest.NewRequest(http.MethodPost, "/model/job-b/v1/chat", nil)
	rw2 := httptest.NewRecorder()
	proxy.ServeHTTP(rw2, req2)

	if rw1.Code != http.StatusOK || rw2.Code != http.StatusOK {
		t.Fatalf("statuses = %d, %d; want 200, 200", rw1.Code, rw2.Code)
	}
	if len(receivedPaths) != 2 {
		t.Fatalf("received %d requests, want 2", len(receivedPaths))
	}
	if !strings.HasPrefix(receivedPaths[0], "upstream1:") {
		t.Errorf("first request went to %q, want upstream1", receivedPaths[0])
	}
	if !strings.HasPrefix(receivedPaths[1], "upstream2:") {
		t.Errorf("second request went to %q, want upstream2", receivedPaths[1])
	}
}

func TestLocalModelProxy_InvalidAuthSecretPathReturns400(t *testing.T) {
	t.Parallel()

	jobsDir := t.TempDir()
	writeJobInfo(t, jobsDir, "job-bad-path", `{
		"model": {
			"url": "https://api.example.com/v1",
			"auth_secret_mount_path": "/home/user/model-auth"
		}
	}`)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cache := NewJobInfoCache(jobsDir, DefaultJobCacheTTL, logger)
	proxy := NewLocalModelReverseProxy(cache, logger)

	req := httptest.NewRequest(http.MethodGet, "/model/job-bad-path/v1/models", nil)
	rw := httptest.NewRecorder()
	proxy.ServeHTTP(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rw.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(rw.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["error"] != "invalid auth_secret_mount_path: missing file:/// prefix" {
		t.Errorf("error = %q, want %q", body["error"], "invalid auth_secret_mount_path: missing file:/// prefix")
	}
	if body["job_id"] != "job-bad-path" {
		t.Errorf("job_id = %q, want %q", body["job_id"], "job-bad-path")
	}
}

func TestLocalModelProxy_UsesPerJobCACert(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result": "tls-ok"}`))
	}))
	defer upstream.Close()

	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: upstream.Certificate().Raw,
	})

	jobsDir := t.TempDir()
	authDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(authDir, "ca_cert"), certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	writeJobInfo(t, jobsDir, "job-tls", `{
		"model": {
			"url": "`+upstream.URL+`",
			"auth_secret_mount_path": "file://`+authDir+`"
		}
	}`)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cache := NewJobInfoCache(jobsDir, DefaultJobCacheTTL, logger)
	p := NewLocalModelReverseProxy(cache, logger)

	req := httptest.NewRequest(http.MethodGet, "/model/job-tls/v1/models", nil)
	rw := httptest.NewRecorder()
	p.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body = %s (custom CA cert should allow HTTPS)", rw.Code, rw.Body.String())
	}
}

func TestLocalModelProxy_HTTPSWithoutCACertFails(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	jobsDir := t.TempDir()
	writeJobInfo(t, jobsDir, "job-no-ca", `{
		"model": {"url": "`+upstream.URL+`"}
	}`)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cache := NewJobInfoCache(jobsDir, DefaultJobCacheTTL, logger)
	p := NewLocalModelReverseProxy(cache, logger)

	req := httptest.NewRequest(http.MethodGet, "/model/job-no-ca/v1/models", nil)
	rw := httptest.NewRecorder()
	p.ServeHTTP(rw, req)

	if rw.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 (HTTPS without matching CA cert should fail)", rw.Code)
	}
}
