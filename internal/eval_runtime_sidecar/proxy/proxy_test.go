package proxy

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestHeadersForLog(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{"Bearer obfuscated", "Bearer secret-token", "Bearer ***"},
		{"Basic obfuscated", "Basic dXNlcjpwYXNz", "Basic ***"},
		{"Other auth obfuscated", "Digest xxx", "***"},
		{"Empty auth", "", "Empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := http.Header{}
			if tt.header != "" {
				h.Set("Authorization", tt.header)
			}
			out := headersForLog(h)
			got := out.Get("Authorization")
			if got != tt.want {
				t.Errorf("headersForLog() Authorization = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSetAuthHeader(t *testing.T) {
	t.Run("no op when token empty", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		SetAuthHeader(req, "")
		if req.Header.Get("Authorization") != "" {
			t.Errorf("expected no Authorization header, got %q", req.Header.Get("Authorization"))
		}
	})

	t.Run("adds Bearer prefix when missing", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		SetAuthHeader(req, "mytoken")
		got := req.Header.Get("Authorization")
		if got != "Bearer mytoken" {
			t.Errorf("Authorization = %q, want Bearer mytoken", got)
		}
	})

	t.Run("keeps Bearer prefix when present", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		SetAuthHeader(req, "Bearer already")
		got := req.Header.Get("Authorization")
		if got != "Bearer already" {
			t.Errorf("Authorization = %q, want Bearer already", got)
		}
	})

	t.Run("keeps Basic prefix when present", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		SetAuthHeader(req, "Basic dXNlcjpwYXNz")
		got := req.Header.Get("Authorization")
		if !strings.HasPrefix(got, "Basic ") {
			t.Errorf("Authorization = %q, should have Basic prefix", got)
		}
	})
}

func TestContextWithAuthInput(t *testing.T) {
	ctx := context.Background()
	_, ok := AuthInputFromContext(ctx)
	if ok {
		t.Error("AuthInputFromContext(background) should not have value")
	}
	input := AuthTokenInput{TargetEndpoint: "ep", AuthToken: "tok"}
	ctx = ContextWithAuthInput(ctx, input)
	got, ok := AuthInputFromContext(ctx)
	if !ok {
		t.Fatal("AuthInputFromContext should have value after ContextWithAuthInput")
	}
	if got.TargetEndpoint != "ep" || got.AuthToken != "tok" {
		t.Errorf("AuthInputFromContext = %+v, want TargetEndpoint=ep AuthToken=tok", got)
	}
}

func TestNewReverseProxy(t *testing.T) {
	logger := slog.Default()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/evaluations/jobs" {
			t.Errorf("backend path = %s", r.URL.Path)
		}
		w.Header().Set("X-Backend", "ok")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer backend.Close()

	target, err := url.Parse(strings.TrimSuffix(backend.URL, "/"))
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	proxy := NewReverseProxy(target, backend.Client(), logger, nil)
	authInput := AuthTokenInput{
		TargetEndpoint: "proxy-test",
		AuthToken:      "test-token",
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/evaluations/jobs", nil)
	req = req.WithContext(ContextWithAuthInput(req.Context(), authInput))
	rw := httptest.NewRecorder()
	proxy.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rw.Code)
	}
	if rw.Header().Get("X-Backend") != "ok" {
		t.Errorf("X-Backend = %q, want ok", rw.Header().Get("X-Backend"))
	}
	if body := rw.Body.String(); body != "ok" {
		t.Errorf("body = %q, want ok", body)
	}
}

func TestContextWithOriginalRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "client.example:8443"
	req.Header.Set("X-Forwarded-Proto", "https")
	ctx := ContextWithOriginalRequest(context.Background(), req)
	got, ok := OriginalRequestFromContext(ctx)
	if !ok {
		t.Fatal("expected OriginalRequest on context")
	}
	if got.Host != "client.example:8443" || got.Scheme != "https" {
		t.Errorf("OriginalRequest = %+v, want Host client.example:8443 Scheme https", got)
	}
}

func TestNewReverseProxyModifyResponseHook(t *testing.T) {
	logger := slog.Default()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Stripped", "gone")
		w.Header().Set("Location", "https://registry.io/v2/")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer backend.Close()

	target, _ := url.Parse(strings.TrimSuffix(backend.URL, "/"))
	client := backend.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	var sawOrig bool
	p := NewReverseProxy(target, client, logger, func(resp *http.Response) error {
		if o, ok := OriginalRequestFromContext(resp.Request.Context()); ok {
			sawOrig = o.Host == "sidecar.local"
			resp.Header.Del("X-Stripped")
			ociRewriteLocationHeader(resp, o)
		}
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/v2/repo/tags/list", nil)
	req.Host = "sidecar.local"
	ctx := ContextWithOriginalRequest(req.Context(), req)
	req = req.WithContext(ctx)
	rw := httptest.NewRecorder()
	p.ServeHTTP(rw, req)

	if !sawOrig {
		t.Error("ModifyResponse should see OriginalRequest from context")
	}
	if rw.Header().Get("X-Stripped") != "" {
		t.Errorf("hook should strip X-Stripped, got %q", rw.Header().Get("X-Stripped"))
	}
	wantLoc := "http://sidecar.local/v2/"
	if got := rw.Header().Get("Location"); got != wantLoc {
		t.Errorf("Location = %q, want %q", got, wantLoc)
	}
}
