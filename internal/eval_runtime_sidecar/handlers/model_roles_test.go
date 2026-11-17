package handlers

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_runtime_sidecar/proxy"
)

// Exercise the handler and real reverse proxy together. The adapter receives
// a loopback URL; only its credential reference selects the actual upstream.
func TestModelRoleRoutingPaths(t *testing.T) {
	for _, role := range []string{"judge", "embedding", "grader", "auditor", "scenarios", "realism", "generation_provider", "critic"} {
		for _, path := range []string{"/v1/chat/completions", "/v1/completions", "/v1/embeddings", "/v1/models", "/v1/responses", "/v1/responses/input_tokens", "/provider-prefix/v1/chat/completions"} {
			t.Run(role+path, func(t *testing.T) {
				primaryCalls := 0
				primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					primaryCalls++
					w.WriteHeader(http.StatusTeapot)
				}))
				defer primary.Close()
				roleCalls := 0
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					roleCalls++
					if r.URL.RequestURI() != path+"?routing=1" {
						t.Errorf("upstream URI = %q", r.URL.RequestURI())
					}
					if r.Header.Get("Authorization") != "Bearer test-"+role {
						t.Error("upstream did not receive the matching role credential")
					}
					body, err := io.ReadAll(r.Body)
					if err != nil || string(body) != `{"model":"deployed-role"}` {
						t.Error("model request body was changed")
					}
					w.WriteHeader(http.StatusOK)
				}))
				defer upstream.Close()
				secret := t.TempDir()
				for key, value := range map[string]string{role + "_api-key": "test-" + role, role + "_url": upstream.URL + "/v1"} {
					if err := os.WriteFile(filepath.Join(secret, key), []byte(value), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				target, err := url.Parse(primary.URL)
				if err != nil {
					t.Fatal(err)
				}
				logger := slog.New(slog.NewTextHandler(io.Discard, nil))
				h := &Handlers{serviceConfig: &config.Config{}, logger: logger,
					modelProxy: proxy.NewModelReverseProxy(target, upstream.Client(), logger, secret, "")}
				method := http.MethodPost
				if path == "/v1/models" {
					method = http.MethodGet
				}
				req := httptest.NewRequest(method, path+"?routing=1", strings.NewReader(`{"model":"deployed-role"}`))
				req.Header.Set("Authorization", "Bearer "+role+"_api-key:ref")
				response := httptest.NewRecorder()
				h.HandleProxyCall(response, req)
				if response.Code != http.StatusOK || roleCalls != 1 || primaryCalls != 0 {
					t.Fatalf("status=%d roleCalls=%d primaryCalls=%d", response.Code, roleCalls, primaryCalls)
				}
			})
		}
	}
}

func TestModelRoleRoutingFallbacks(t *testing.T) {
	for _, role := range []string{"judge", "embedding", "grader", "auditor", "scenarios", "realism", "generation_provider", "critic"} {
		for _, tc := range []struct {
			name          string
			keyPresent    bool
			keyValue      string
			urlPresent    bool
			usePrimaryRef bool
			wantStatus    int
			wantAuth      string
			wantCalls     int32
		}{
			{
				name: "absent role uses adapter-selected primary reference", usePrimaryRef: true,
				wantStatus: http.StatusOK, wantAuth: "Bearer test-primary", wantCalls: 1,
			},
			{
				name: "key alone uses primary endpoint with role credential", keyPresent: true, keyValue: "test-role",
				wantStatus: http.StatusOK, wantAuth: "Bearer test-role", wantCalls: 1,
			},
			// Adapters validate URL/key pairing before making requests. This case checks
			// that the sidecar rejects an unresolved role reference even when its URL exists.
			{name: "missing role credential reference is rejected when URL exists", urlPresent: true, wantStatus: http.StatusBadRequest},
			{name: "empty key is rejected", keyPresent: true, urlPresent: true, wantStatus: http.StatusBadRequest},
			{name: "missing role reference never falls back", wantStatus: http.StatusBadRequest},
		} {
			t.Run(role+"/"+tc.name, func(t *testing.T) {
				path := "/v1/chat/completions?routing=1"
				if role == "embedding" {
					path = "/v1/embeddings?routing=1"
				}
				const requestBody = `{"model":"deployed-role"}`
				var primaryCalls, roleCalls atomic.Int32
				primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					primaryCalls.Add(1)
					if r.Header.Get("Authorization") != tc.wantAuth {
						t.Error("primary did not receive the selected credential")
					}
					if r.URL.RequestURI() != path {
						t.Errorf("primary URI = %q; want %q", r.URL.RequestURI(), path)
					}
					body, err := io.ReadAll(r.Body)
					if err != nil || string(body) != requestBody {
						t.Error("model request body was changed")
					}
					w.WriteHeader(http.StatusOK)
				}))
				defer primary.Close()
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					roleCalls.Add(1)
					w.WriteHeader(http.StatusTeapot)
				}))
				defer upstream.Close()
				secret := t.TempDir()
				entries := map[string]string{"api-key": "test-primary"}
				if tc.keyPresent {
					entries[role+"_api-key"] = tc.keyValue
				}
				if tc.urlPresent {
					entries[role+"_url"] = upstream.URL + "/v1"
				}
				for key, value := range entries {
					if err := os.WriteFile(filepath.Join(secret, key), []byte(value), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				target, err := url.Parse(primary.URL)
				if err != nil {
					t.Fatal(err)
				}
				logger := slog.New(slog.NewTextHandler(io.Discard, nil))
				h := &Handlers{serviceConfig: &config.Config{}, logger: logger,
					modelProxy: proxy.NewModelReverseProxy(target, upstream.Client(), logger, secret, "")}
				req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(requestBody))
				key := role + "_api-key"
				if tc.usePrimaryRef {
					// The adapter reuses api-key:ref when no role entries exist.
					// The sidecar must not silently repair a missing role ref.
					key = "api-key"
				}
				req.Header.Set("Authorization", "Bearer "+key+":ref")
				response := httptest.NewRecorder()
				h.HandleProxyCall(response, req)
				if response.Code != tc.wantStatus || primaryCalls.Load() != tc.wantCalls || roleCalls.Load() != 0 {
					t.Fatalf("status=%d primaryCalls=%d roleCalls=%d", response.Code, primaryCalls.Load(), roleCalls.Load())
				}
				if tc.wantStatus == http.StatusBadRequest && !strings.Contains(response.Body.String(), key) {
					t.Fatal("rejection must identify the unresolved credential reference")
				}
			})
		}
	}
}
