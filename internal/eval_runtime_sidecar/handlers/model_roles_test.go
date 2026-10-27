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
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_runtime_sidecar/proxy"
)

// Exercise the handler and real reverse proxy together. The adapter receives
// a loopback URL; only its credential reference selects the actual upstream.
func TestModelRoleRoutingPaths(t *testing.T) {
	for _, role := range []string{"judge", "auditor", "scenarios", "realism", "generation_provider", "critic"} {
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
