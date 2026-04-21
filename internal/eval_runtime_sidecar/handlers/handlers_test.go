package handlers

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_runtime_sidecar/proxy"
)

func TestNew(t *testing.T) {
	logger := slog.Default()

	t.Run("returns error when eval_hub.base_url is not set", func(t *testing.T) {
		cfg := &config.Config{
			Sidecar: &config.SidecarConfig{
				EvalHub: &config.EvalHubClientConfig{
					InsecureSkipVerify: true,
				},
			},
		}
		_, err := New(cfg, logger)
		if err == nil {
			t.Fatal("expected error when eval_hub.base_url is not set")
		}
		if err.Error() != "eval_hub.base_url is not set in sidecar config" {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("returns Handlers when eval_hub.base_url and mlflow set", func(t *testing.T) {
		cfg := &config.Config{
			Sidecar: &config.SidecarConfig{
				EvalHub: &config.EvalHubClientConfig{
					BaseURL:            "http://localhost:8080",
					InsecureSkipVerify: true,
				},
			},
			MLFlow: &config.MLFlowConfig{TrackingURI: "http://localhost:5000"},
		}
		h, err := New(cfg, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if h == nil {
			t.Fatal("expected non-nil Handlers")
		}
		if h.evalHubProxy == nil {
			t.Error("expected non-nil evalHubProxy")
		}
		if h.mlflowProxy == nil {
			t.Error("expected non-nil mlflowProxy")
		}
		if h.modelProxy != nil {
			t.Error("expected nil modelProxy when model proxy not configured")
		}
	})

	t.Run("returns Handlers with model proxy when model url set", func(t *testing.T) {
		cfg := &config.Config{
			Sidecar: &config.SidecarConfig{
				EvalHub: &config.EvalHubClientConfig{
					BaseURL:            "http://localhost:8080",
					InsecureSkipVerify: true,
				},
				Model: &config.SidecarModelConfig{
					URL:                "http://127.0.0.1:9999/v1",
					InsecureSkipVerify: true,
				},
			},
			MLFlow: &config.MLFlowConfig{TrackingURI: "http://localhost:5000"},
		}
		h, err := New(cfg, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if h.modelProxy == nil {
			t.Fatal("expected non-nil modelProxy")
		}
		if h.huggingFaceProxy != nil {
			t.Error("expected nil huggingFaceProxy when Hugging Face proxy not configured")
		}
	})

	t.Run("returns Handlers with Hugging Face proxy when sidecar.huggingface set", func(t *testing.T) {
		cfg := &config.Config{
			Sidecar: &config.SidecarConfig{
				EvalHub: &config.EvalHubClientConfig{
					BaseURL:            "http://localhost:8080",
					InsecureSkipVerify: true,
				},
				HuggingFace: &config.SidecarHuggingFaceConfig{
					InsecureSkipVerify: true,
				},
			},
			MLFlow: &config.MLFlowConfig{TrackingURI: "http://localhost:5000"},
		}
		h, err := New(cfg, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if h.huggingFaceProxy == nil {
			t.Fatal("expected non-nil huggingFaceProxy")
		}
	})
}

func TestHandlers_HandleHealth(t *testing.T) {
	h := &Handlers{logger: slog.Default()}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rw := httptest.NewRecorder()
	h.HandleHealth(rw, req)
	if rw.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rw.Code)
	}
	if body := rw.Body.String(); body != "" {
		t.Errorf("body = %q, want empty", body)
	}
}

func TestHandlers_HandleProxyCall(t *testing.T) {
	logger := slog.Default()
	cfg := &config.Config{
		Sidecar: &config.SidecarConfig{
			EvalHub: &config.EvalHubClientConfig{
				BaseURL:            "http://localhost:8080",
				InsecureSkipVerify: true,
			},
		},
		MLFlow: &config.MLFlowConfig{TrackingURI: "http://localhost:5000"},
	}
	h, err := New(cfg, logger)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	t.Run("unknown path returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/unknown", nil)
		rw := httptest.NewRecorder()
		h.HandleProxyCall(rw, req)
		if rw.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rw.Code)
		}
		if body := rw.Body.String(); body != "unknown proxy call: /unknown\n" {
			t.Errorf("body = %q", body)
		}
	})

	t.Run("eval-hub path with nil EvalHub returns 400", func(t *testing.T) {
		h2 := &Handlers{
			logger: logger,
			serviceConfig: &config.Config{
				Sidecar: &config.SidecarConfig{EvalHub: nil},
				MLFlow:  &config.MLFlowConfig{TrackingURI: "http://localhost:5000"},
			},
		}
		req := httptest.NewRequest(http.MethodGet, "/api/v1/evaluations/jobs", nil)
		rw := httptest.NewRecorder()
		h2.HandleProxyCall(rw, req)
		if rw.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rw.Code)
		}
	})

	t.Run("eval-hub path with prefix matches", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/evaluations/jobs/123", nil)
		rw := httptest.NewRecorder()
		h.HandleProxyCall(rw, req)
		if body := rw.Body.String(); body == "unknown proxy call: /api/v1/evaluations/jobs/123\n" {
			t.Errorf("eval-hub path should match prefix; got unknown proxy call")
		}
	})

	t.Run("mlflow API path with configured MLFlow matches", func(t *testing.T) {
		for _, path := range []string{
			"/api/2.0/mlflow",
			"/api/2.0/mlflow/experiments/list",
			"/api/2.0/mlflow/runs/create",
			"/api/2.0/mlflow/experiments/search?max_results=1",
			"/api/2.0/mlflow-artifacts",
			"/api/2.0/mlflow-artifacts/artifact",
			"/api/2.0/mlflow-artifacts/get-artifact?path=x",
			"/api/2.0/mlflow-custom/endpoint",
		} {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rw := httptest.NewRecorder()
			h.HandleProxyCall(rw, req)
			if strings.Contains(rw.Body.String(), "unknown proxy call") {
				t.Errorf("%q: expected mlflow route, got unknown proxy call", path)
			}
		}
	})

	t.Run("path with mlflow segment but not MLflow API prefix is unknown", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/foo/mlflow/bar", nil)
		rw := httptest.NewRecorder()
		h.HandleProxyCall(rw, req)
		if rw.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rw.Code)
		}
		if body := rw.Body.String(); !strings.Contains(body, "unknown proxy call") {
			t.Errorf("body = %q, want unknown proxy call", body)
		}
	})

	t.Run("mlflow path with nil MLFlow returns 400", func(t *testing.T) {
		cfgNoMLFlow := &config.Config{
			Sidecar: &config.SidecarConfig{
				EvalHub: &config.EvalHubClientConfig{
					BaseURL:            "http://localhost:8080",
					InsecureSkipVerify: true,
				},
			},
		}
		hNoMLFlow, err := New(cfgNoMLFlow, logger)
		if err != nil {
			t.Fatalf("New() error: %v", err)
		}
		req := httptest.NewRequest(http.MethodGet, "/api/2.0/mlflow/experiments/list", nil)
		rw := httptest.NewRecorder()
		hNoMLFlow.HandleProxyCall(rw, req)
		if rw.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400 (mlflow proxy not configured)", rw.Code)
		}
	})

	t.Run("model proxy with auth_api_key_path sends bearer from file", func(t *testing.T) {
		dir := t.TempDir()
		keyFile := filepath.Join(dir, "apikey")
		if err := os.WriteFile(keyFile, []byte("secret-from-file\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		var gotAuth string
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(upstream.Close)

		cfg := &config.Config{
			Sidecar: &config.SidecarConfig{
				EvalHub: &config.EvalHubClientConfig{
					BaseURL:            "http://localhost:8080",
					InsecureSkipVerify: true,
				},
				Model: &config.SidecarModelConfig{
					URL:                upstream.URL + "/v1",
					InsecureSkipVerify: true,
					AuthAPIKeyPath:     keyFile,
				},
			},
			MLFlow: &config.MLFlowConfig{TrackingURI: "http://localhost:5000"},
		}
		hm, err := New(cfg, logger)
		if err != nil {
			t.Fatalf("New() error: %v", err)
		}
		req := httptest.NewRequest(http.MethodGet, "/model/v1/models", nil)
		rw := httptest.NewRecorder()
		hm.HandleProxyCall(rw, req)
		if rw.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %q", rw.Code, rw.Body.String())
		}
		if gotAuth != "Bearer secret-from-file" {
			t.Fatalf("Authorization = %q, want Bearer secret-from-file", gotAuth)
		}
	})

	t.Run("Hugging Face proxy with token_path sends bearer from file", func(t *testing.T) {
		dir := t.TempDir()
		keyFile := filepath.Join(dir, "hftoken")
		if err := os.WriteFile(keyFile, []byte("hf-secret\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		var gotAuth string
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("Authorization")
			if r.URL.Path != "/api/models" {
				t.Errorf("upstream path = %q, want /api/models", r.URL.Path)
			}
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(upstream.Close)

		u := upstream.URL
		cfg := &config.Config{
			Sidecar: &config.SidecarConfig{
				EvalHub: &config.EvalHubClientConfig{
					BaseURL:            "http://localhost:8080",
					InsecureSkipVerify: true,
				},
				HuggingFace: &config.SidecarHuggingFaceConfig{
					URL:                u,
					InsecureSkipVerify: true,
					TokenPath:          keyFile,
				},
			},
			MLFlow: &config.MLFlowConfig{TrackingURI: "http://localhost:5000"},
		}
		hhf, err := New(cfg, logger)
		if err != nil {
			t.Fatalf("New() error: %v", err)
		}
		req := httptest.NewRequest(http.MethodGet, "/huggingface/api/models", nil)
		rw := httptest.NewRecorder()
		hhf.HandleProxyCall(rw, req)
		if rw.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %q", rw.Code, rw.Body.String())
		}
		if gotAuth != "Bearer hf-secret" {
			t.Fatalf("Authorization = %q, want Bearer hf-secret", gotAuth)
		}
	})

	t.Run("Hugging Face proxy without token_path sends no authorization", func(t *testing.T) {
		proxy.UpdateCachedToken(proxy.AuthTokenInput{TargetEndpoint: "huggingface"}, "")
		var gotAuth string
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(upstream.Close)

		cfg := &config.Config{
			Sidecar: &config.SidecarConfig{
				EvalHub: &config.EvalHubClientConfig{
					BaseURL:            "http://localhost:8080",
					InsecureSkipVerify: true,
				},
				HuggingFace: &config.SidecarHuggingFaceConfig{
					URL:                upstream.URL,
					InsecureSkipVerify: true,
				},
			},
			MLFlow: &config.MLFlowConfig{TrackingURI: "http://localhost:5000"},
		}
		hhf, err := New(cfg, logger)
		if err != nil {
			t.Fatalf("New() error: %v", err)
		}
		req := httptest.NewRequest(http.MethodGet, "/huggingface/api/models", nil)
		rw := httptest.NewRecorder()
		hhf.HandleProxyCall(rw, req)
		if rw.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %q", rw.Code, rw.Body.String())
		}
		if gotAuth != "" {
			t.Fatalf("Authorization = %q, want empty for anonymous Hub access", gotAuth)
		}
	})

	t.Run("registry path with nil OCI returns 400", func(t *testing.T) {
		// h has no Sidecar.OCI, so ociRepository is empty; path without repository name does not match OCI -> unknown proxy call
		req := httptest.NewRequest(http.MethodGet, "/registry/v2/", nil)
		rw := httptest.NewRecorder()
		h.HandleProxyCall(rw, req)
		if rw.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rw.Code)
		}
		if body := rw.Body.String(); !strings.Contains(body, "unknown proxy call") {
			t.Errorf("body = %q, want unknown proxy call (OCI not configured, no repository to match)", body)
		}
	})
}

func TestOciRouteMatch(t *testing.T) {
	h := &Handlers{ociRepository: "org/repo"}
	tests := []struct {
		uri  string
		want bool
	}{
		{"/v2/org/repo/manifests/latest", true},
		{"/v2/ac/org/repo/manifests/latest", false},
		{"/org/repo/tags/list", true},
		{"/xorg/repo/tags/list", false},
		{"/v2/org/repo2/tags/list", false},
		// Query must not affect matching (path only).
		{"/v2/org/repo/blobs/uploads?q=/v2/evil/org/repo/extra", true},
		{"/v2/evil/blobs?q=org%2Frepo", false},
	}
	for _, tt := range tests {
		if got := h.ociRouteMatch(tt.uri); got != tt.want {
			t.Errorf("ociRouteMatch(%q) = %v, want %v", tt.uri, got, tt.want)
		}
	}
}

func TestIsMLflowProxyPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/api/2.0/mlflow", true},
		{"/api/2.0/mlflow/", true},
		{"/api/2.0/mlflow/experiments/list", true},
		{"/api/2.0/mlflow-extra", true},
		{"/api/2.0/mlflow-artifacts", true},
		{"/api/2.0/mlflow-artifacts/", true},
		{"/api/2.0/mlflow-artifacts/get-artifact", true},
		{"/api/2.0/mlflow-artifactsmalicious", true},
		{"/api/2.0/ml", false},
		{"/prefix/api/2.0/mlflow/runs", false},
	}
	for _, tt := range tests {
		if got := isMLflowProxyPath(tt.path); got != tt.want {
			t.Errorf("isMLflowProxyPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestRequestPathForRouting(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"/v2/a/b", "/v2/a/b"},
		{"/v2/a/b?x=y", "/v2/a/b"},
		{"/v2/a/b#frag", "/v2/a/b"},
		{"/v2/a?b=c&d=e", "/v2/a"},
		{"/v2/foo%2Fbar/blobs?q=/v2/evil", "/v2/foo%2Fbar/blobs"},
	}
	for _, tt := range tests {
		if got := requestPathForRouting(tt.in); got != tt.want {
			t.Errorf("requestPathForRouting(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
