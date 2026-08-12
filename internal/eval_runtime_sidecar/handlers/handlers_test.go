package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestNewModelProxy_UsesServiceAccountAuthFileDefault(t *testing.T) {
	logger := slog.Default()
	cfg := &config.Config{
		Sidecar: &config.SidecarConfig{
			Model: &config.SidecarModelConfig{
				URL:                 "http://model.example:8080",
				InsecureSkipVerify:  true,
				AuthSecretMountPath: t.TempDir(),
			},
		},
	}
	rp, err := newModelProxy(cfg, logger)
	if err != nil {
		t.Fatalf("newModelProxy: %v", err)
	}
	if rp == nil {
		t.Fatal("expected non-nil model proxy")
	}
}

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
			"/api/3.0/mlflow/traces/search",
			"/api/3.0/mlflow/traces/tr-abc123",
			"/api/3.0/mlflow/server-info",
		} {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rw := httptest.NewRecorder()
			h.HandleProxyCall(rw, req)
			if strings.Contains(rw.Body.String(), "unknown proxy call") {
				t.Errorf("%q: expected mlflow route, got unknown proxy call", path)
			}
		}
	})

	t.Run("paths that look like mlflow but are not the MLflow API roots are unknown", func(t *testing.T) {
		for _, path := range []string{
			"/api/2.0/mlflowx/anything",
			"/api/3.0/mlflowx/anything",
			"/api/2.0/mlflow-custom/endpoint",
		} {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rw := httptest.NewRecorder()
			h.HandleProxyCall(rw, req)
			if rw.Code != http.StatusBadRequest {
				t.Errorf("%q: status = %d, want 400", path, rw.Code)
			}
			if !strings.Contains(rw.Body.String(), "unknown proxy call") {
				t.Errorf("%q: expected unknown proxy call", path)
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

	t.Run("registry path with nil OCI returns 400", func(t *testing.T) {
		// h has no ociRepository (as when job spec has no OCI); path without repository name does not match OCI -> unknown proxy call
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

func TestReadGitMetadata(t *testing.T) {
	t.Run("returns SHA from file", func(t *testing.T) {
		f, err := os.CreateTemp(t.TempDir(), "git-metadata-*")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.WriteString("abc123def456\n")
		_ = f.Close()
		got, err := readGitMetadata(f.Name())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "abc123def456" {
			t.Errorf("got %q, want %q", got, "abc123def456")
		}
	})
	t.Run("returns error when file absent", func(t *testing.T) {
		_, err := readGitMetadata(filepath.Join(t.TempDir(), "no-such-file"))
		if err == nil {
			t.Fatal("expected error for missing file")
		}
	})
}

func TestMaybeInjectGitSHA_InjectsBenchmarkStatusEvent(t *testing.T) {
	// Write .git-metadata to a temp path; override the constant via the handler directly.
	dir := t.TempDir()
	metaPath := filepath.Join(dir, ".git-metadata")
	if err := os.WriteFile(metaPath, []byte("cafebabe\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	h := &Handlers{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		serviceConfig: &config.Config{
			Sidecar: &config.SidecarConfig{
				InitContainer: &config.InitContainerConfig{IsGitJob: true},
			},
		},
		gitSHA: "cafebabe",
	}

	evt := api.StatusEvent{
		BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
			ProviderID: "p1", ID: "b1", Status: "running",
		},
	}
	body, _ := json.Marshal(evt)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/evaluations/jobs/job-1/events", bytes.NewReader(body))

	newReq := h.maybeInjectResolvedSHA(req)

	rawOut, _ := io.ReadAll(newReq.Body)
	var out api.StatusEvent
	if err := json.Unmarshal(rawOut, &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if out.BenchmarkStatusEvent == nil {
		t.Fatal("BenchmarkStatusEvent is nil")
	}
	if out.BenchmarkStatusEvent.JobMeta == nil || out.BenchmarkStatusEvent.JobMeta.ResolvedSHA != "cafebabe" {
		got := ""
		if out.BenchmarkStatusEvent.JobMeta != nil {
			got = out.BenchmarkStatusEvent.JobMeta.ResolvedSHA
		}
		t.Errorf("JobMeta.ResolvedSHA = %q, want %q", got, "cafebabe")
	}
}

func TestMaybeInjectGitSHA_SkipsNonBenchmarkBody(t *testing.T) {
	h := &Handlers{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		gitSHA: "cafebabe",
	}

	// Body parses as a StatusEvent but has no BenchmarkStatusEvent — injection must be skipped.
	body := []byte(`{"other_field":"value"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/evaluations/jobs/job-1/events", bytes.NewReader(body))

	newReq := h.maybeInjectResolvedSHA(req)

	rawOut, _ := io.ReadAll(newReq.Body)
	if !bytes.Equal(rawOut, body) {
		t.Errorf("body changed for non-benchmark body: got %s", rawOut)
	}
}

func TestMaybeInjectGitSHA_NoopWhenGitSHAEmpty(t *testing.T) {
	// When gitSHA is empty and metadata is missing, a body without job_meta.resolved_sha is unchanged.
	h := &Handlers{
		logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		gitSHA:          "",
		gitMetadataPath: filepath.Join(t.TempDir(), "missing-git-metadata"),
	}

	body := []byte(`{"benchmark_status_event":{"provider_id":"p","id":"b","status":"running"}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/evaluations/jobs/job-1/events", bytes.NewReader(body))

	newReq := h.maybeInjectResolvedSHA(req)

	rawOut, _ := io.ReadAll(newReq.Body)
	if !bytes.Equal(rawOut, body) {
		t.Errorf("body changed when gitSHA empty: got %s", rawOut)
	}
}

func TestMaybeInjectGitSHA_RetriesWhenMetadataAppears(t *testing.T) {
	dir := t.TempDir()
	metaPath := filepath.Join(dir, ".git-metadata")

	h := &Handlers{
		logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		gitSHA:          "",
		gitMetadataPath: metaPath,
	}

	evt := api.StatusEvent{
		BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
			ProviderID: "p1", ID: "b1", Status: "running",
		},
	}
	body, _ := json.Marshal(evt)

	// First call: file missing — no injection.
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/evaluations/jobs/job-1/events", bytes.NewReader(body))
	out1, _ := io.ReadAll(h.maybeInjectResolvedSHA(req1).Body)
	var ev1 api.StatusEvent
	_ = json.Unmarshal(out1, &ev1)
	if ev1.BenchmarkStatusEvent != nil && ev1.BenchmarkStatusEvent.JobMeta != nil && ev1.BenchmarkStatusEvent.JobMeta.ResolvedSHA != "" {
		t.Fatalf("first call: JobMeta.ResolvedSHA = %q, want empty", ev1.BenchmarkStatusEvent.JobMeta.ResolvedSHA)
	}
	if h.gitSHA != "" {
		t.Fatalf("first call: gitSHA = %q, want empty", h.gitSHA)
	}

	// File appears — second call should load and inject.
	if err := os.WriteFile(metaPath, []byte("retrydeadbeef\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/evaluations/jobs/job-1/events", bytes.NewReader(body))
	out2, _ := io.ReadAll(h.maybeInjectResolvedSHA(req2).Body)
	var ev2 api.StatusEvent
	if err := json.Unmarshal(out2, &ev2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if ev2.BenchmarkStatusEvent == nil || ev2.BenchmarkStatusEvent.JobMeta == nil || ev2.BenchmarkStatusEvent.JobMeta.ResolvedSHA != "retrydeadbeef" {
		got := ""
		if ev2.BenchmarkStatusEvent != nil && ev2.BenchmarkStatusEvent.JobMeta != nil {
			got = ev2.BenchmarkStatusEvent.JobMeta.ResolvedSHA
		}
		t.Errorf("second call: JobMeta.ResolvedSHA = %q, want %q", got, "retrydeadbeef")
	}
	if h.gitSHA != "retrydeadbeef" {
		t.Errorf("gitSHA = %q, want %q", h.gitSHA, "retrydeadbeef")
	}
}

func TestMaybeInjectGitSHA_StripsClientSHAWhenStillEmpty(t *testing.T) {
	h := &Handlers{
		logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		gitSHA:          "",
		gitMetadataPath: filepath.Join(t.TempDir(), "missing-git-metadata"),
	}

	evt := api.StatusEvent{
		BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
			ProviderID: "p1", ID: "b1", Status: "running", JobMeta: &api.JobMeta{ResolvedSHA: "forged"},
		},
	}
	body, _ := json.Marshal(evt)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/evaluations/jobs/job-1/events", bytes.NewReader(body))

	out, _ := io.ReadAll(h.maybeInjectResolvedSHA(req).Body)
	var got api.StatusEvent
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.BenchmarkStatusEvent == nil {
		t.Fatal("BenchmarkStatusEvent is nil")
	}
	if got.BenchmarkStatusEvent.JobMeta != nil && got.BenchmarkStatusEvent.JobMeta.ResolvedSHA != "" {
		t.Errorf("JobMeta.ResolvedSHA = %q, want stripped empty", got.BenchmarkStatusEvent.JobMeta.ResolvedSHA)
	}
}

func TestMaybeInjectGitSHA_InjectsWithPreSeededSHA(t *testing.T) {
	// Verifies injection when gitSHA is pre-populated (simulating the startup read).
	// readGitMetadata is unit-tested separately; here we confirm maybeInjectResolvedSHA
	// injects correctly given a SHA already set on the Handlers struct.
	h := &Handlers{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		serviceConfig: &config.Config{
			Sidecar: &config.SidecarConfig{
				InitContainer: &config.InitContainerConfig{IsGitJob: true},
			},
		},
		gitSHA: "aabbccdd",
	}

	evt := api.StatusEvent{
		BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
			ProviderID: "p1", ID: "b1", Status: "running",
		},
	}
	body, _ := json.Marshal(evt)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/evaluations/jobs/job-once/events", bytes.NewReader(body))

	newReq := h.maybeInjectResolvedSHA(req)

	rawOut, _ := io.ReadAll(newReq.Body)
	var out api.StatusEvent
	if err := json.Unmarshal(rawOut, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.BenchmarkStatusEvent == nil || out.BenchmarkStatusEvent.JobMeta == nil || out.BenchmarkStatusEvent.JobMeta.ResolvedSHA != "aabbccdd" {
		got := ""
		if out.BenchmarkStatusEvent != nil && out.BenchmarkStatusEvent.JobMeta != nil {
			got = out.BenchmarkStatusEvent.JobMeta.ResolvedSHA
		}
		t.Errorf("JobMeta.ResolvedSHA = %q, want %q", got, "aabbccdd")
	}
	// gitSHA must remain set for subsequent injections.
	if h.gitSHA != "aabbccdd" {
		t.Errorf("gitSHA should remain set after injection, got %q", h.gitSHA)
	}
}

func TestMaybeInjectGitSHA_InjectsOnEveryCall(t *testing.T) {
	// gitSHA is set at startup and injected on every BenchmarkStatusEvent so the server
	// can idempotently persist it (server skips if already set).
	h := &Handlers{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		gitSHA: "firstsha",
	}

	body := func() []byte {
		b, _ := json.Marshal(api.StatusEvent{
			BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
				ProviderID: "p", ID: "b", Status: "running",
			},
		})
		return b
	}

	for i, label := range []string{"first", "second", "third"} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/evaluations/jobs/job-z/events", bytes.NewReader(body()))
		out, _ := io.ReadAll(h.maybeInjectResolvedSHA(req).Body)
		var ev api.StatusEvent
		_ = json.Unmarshal(out, &ev)
		if ev.BenchmarkStatusEvent == nil || ev.BenchmarkStatusEvent.JobMeta == nil || ev.BenchmarkStatusEvent.JobMeta.ResolvedSHA != "firstsha" {
			t.Errorf("call %d (%s): JobMeta.ResolvedSHA = %q, want %q", i+1, label, ev.BenchmarkStatusEvent.JobMeta.ResolvedSHA, "firstsha")
		}
		if h.gitSHA != "firstsha" {
			t.Errorf("call %d (%s): gitSHA must remain set, got %q", i+1, label, h.gitSHA)
		}
	}
}

func TestIsEventsPath(t *testing.T) {
	tests := []struct {
		uri  string
		want bool
	}{
		{"/api/v1/evaluations/jobs/abc-123/events", true},
		{"/api/v1/evaluations/jobs/abc-123/events?foo=bar", true},
		{"/api/v1/evaluations/jobs/abc-123", false},
		{"/api/v1/evaluations/jobs", false},
		{"/api/v1/evaluations/", false},
		{"/api/v1/evaluations/jobs/abc-123/sub/events", false}, // extra path segment must not match
	}
	for _, tt := range tests {
		if got := isEventsPath(tt.uri); got != tt.want {
			t.Errorf("isEventsPath(%q) = %v, want %v", tt.uri, got, tt.want)
		}
	}
}

func TestHandleProxyCall_GitSHAInjectedOnEventsPost(t *testing.T) {
	// End-to-end: sidecar has GitTestData + pre-populated gitSHA; a POST to /events
	// should arrive at the upstream with JobMeta.ResolvedSHA set in the body.
	var receivedSHA string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var evt api.StatusEvent
		if json.Unmarshal(body, &evt) == nil && evt.BenchmarkStatusEvent != nil {
			if evt.BenchmarkStatusEvent.JobMeta != nil {
				receivedSHA = evt.BenchmarkStatusEvent.JobMeta.ResolvedSHA
			}
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	cfg := &config.Config{
		Sidecar: &config.SidecarConfig{
			EvalHub: &config.EvalHubClientConfig{
				BaseURL:            upstream.URL,
				InsecureSkipVerify: true,
			},
			InitContainer: &config.InitContainerConfig{IsGitJob: true},
		},
	}
	h, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	// Pre-populate gitSHA to avoid filesystem dependency on .git-metadata path.
	h.gitSHA = "deadbeef"

	evt := api.StatusEvent{
		BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
			ProviderID: "p1", ID: "b1", Status: "running",
		},
	}
	body, _ := json.Marshal(evt)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/evaluations/jobs/job-7/events", bytes.NewReader(body))
	rw := httptest.NewRecorder()
	h.HandleProxyCall(rw, req)

	if receivedSHA != "deadbeef" {
		t.Errorf("upstream received SHA %q, want %q", receivedSHA, "deadbeef")
	}
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
		{"/api/2.0/mlflowx", false},
		{"/api/2.0/mlflowx/", false},
		{"/api/2.0/mlflow-extra", false},
		{"/api/2.0/mlflow-artifacts", true},
		{"/api/2.0/mlflow-artifacts/", true},
		{"/api/2.0/mlflow-artifacts/get-artifact", true},
		{"/api/2.0/mlflow-artifactsmalicious", false},
		{"/api/2.0/ml", false},
		{"/prefix/api/2.0/mlflow/runs", false},
		{"/api/3.0/mlflow/server-info", true},
		{"/api/3.0/mlflow/traces/search", true},
		{"/api/3.0/mlflowx", false},
		{"/api/3.0/ml", false},
		{"/prefix/api/3.0/mlflow/server-info", false},
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
