package mlflow

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/mlflowclient"
)

func discardTestLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func mlflowServiceConfig(t *testing.T, trackingURI string, mutate func(*config.MLFlowConfig)) *config.Config {
	t.Helper()
	cfg := &config.Config{
		MLFlow: &config.MLFlowConfig{
			TrackingURI: trackingURI,
			TokenPath:   filepath.Join(t.TempDir(), "no-such-token"),
		},
	}
	if mutate != nil {
		mutate(cfg.MLFlow)
	}
	return cfg
}

func TestNewMLFlowClient(t *testing.T) {
	t.Parallel()
	logger := discardTestLogger()

	t.Run("no tracking URI", func(t *testing.T) {
		t.Parallel()
		client, _, err := NewMLFlowClient(&config.Config{MLFlow: &config.MLFlowConfig{}}, logger)
		if err != nil {
			t.Fatalf("NewMLFlowClient() err = %v", err)
		}
		if client != nil {
			t.Fatal("expected nil client when tracking URI is unset")
		}
	})

	t.Run("probes workspaces once at startup when available", func(t *testing.T) {
		t.Parallel()
		var probes atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/3.0/mlflow/server-info" {
				http.NotFound(w, r)
				return
			}
			probes.Add(1)
			_ = json.NewEncoder(w).Encode(mlflowclient.ServerInfoResponse{WorkspacesEnabled: true})
		}))
		t.Cleanup(srv.Close)

		cfg := mlflowServiceConfig(t, srv.URL, func(m *config.MLFlowConfig) {
			m.Workspace = "prod-ws"
			m.Token = "static-token"
		})
		client, workspaceSupport, err := NewMLFlowClient(cfg, logger)
		if err != nil {
			t.Fatalf("NewMLFlowClient() err = %v", err)
		}
		if probes.Load() != 1 {
			t.Fatalf("startup probes = %d, want 1", probes.Load())
		}
		if !client.WorkspacesEnabled() || !workspaceSupport.Resolved() {
			t.Fatal("expected workspaces enabled after startup probe")
		}
		if client.WorkspaceName() != "prod-ws" {
			t.Fatalf("workspace = %q, want prod-ws", client.WorkspaceName())
		}
	})

	t.Run("SetupMLFlowClient still fetches /version", func(t *testing.T) {
		t.Parallel()
		var versionHits atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/version":
				versionHits.Add(1)
				_, _ = w.Write([]byte("3.12.0"))
			case "/api/3.0/mlflow/server-info":
				_ = json.NewEncoder(w).Encode(mlflowclient.ServerInfoResponse{WorkspacesEnabled: false})
			default:
				http.NotFound(w, r)
			}
		}))
		t.Cleanup(srv.Close)

		cfg := mlflowServiceConfig(t, srv.URL, nil)
		client, _, uri, version, err := SetupMLFlowClient(cfg, logger)
		if err != nil {
			t.Fatalf("SetupMLFlowClient() err = %v", err)
		}
		if client == nil || uri != srv.URL {
			t.Fatalf("client/uri unexpected: client=%v uri=%q", client != nil, uri)
		}
		if version != "3.12.0" {
			t.Fatalf("version = %q, want 3.12.0", version)
		}
		if versionHits.Load() != 1 {
			t.Fatalf("/version hits = %d, want 1", versionHits.Load())
		}
	})

	t.Run("startup probe success skips EnsureWorkspace re-probe", func(t *testing.T) {
		t.Parallel()
		var probes atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/api/3.0/mlflow/server-info":
				probes.Add(1)
				_ = json.NewEncoder(w).Encode(mlflowclient.ServerInfoResponse{WorkspacesEnabled: true})
			case strings.HasPrefix(r.URL.Path, "/api/3.0/mlflow/workspaces/"):
				_ = json.NewEncoder(w).Encode(mlflowclient.GetWorkspaceResponse{
					Workspace: mlflowclient.Workspace{Name: "prod-ws"},
				})
			default:
				http.NotFound(w, r)
			}
		}))
		t.Cleanup(srv.Close)

		cfg := mlflowServiceConfig(t, srv.URL, func(m *config.MLFlowConfig) {
			m.Workspace = "prod-ws"
		})
		client, _, err := NewMLFlowClient(cfg, logger)
		if err != nil {
			t.Fatalf("NewMLFlowClient() err = %v", err)
		}
		if probes.Load() != 1 {
			t.Fatalf("startup probes = %d, want 1", probes.Load())
		}
		if err := client.EnsureWorkspace(); err != nil {
			t.Fatalf("EnsureWorkspace() = %v", err)
		}
		if probes.Load() != 1 {
			t.Fatalf("probes after EnsureWorkspace = %d, want 1 (no retry when resolved)", probes.Load())
		}
		if !client.WorkspacesEnabled() {
			t.Fatal("expected workspaces enabled")
		}
	})

	t.Run("failed startup probe leaves unknown and retries via PrepareClient", func(t *testing.T) {
		t.Parallel()
		var probes atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/3.0/mlflow/server-info" {
				http.NotFound(w, r)
				return
			}
			n := probes.Add(1)
			if n == 1 {
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
				return
			}
			_ = json.NewEncoder(w).Encode(mlflowclient.ServerInfoResponse{WorkspacesEnabled: true})
		}))
		t.Cleanup(srv.Close)

		cfg := mlflowServiceConfig(t, srv.URL, nil)
		client, workspaceSupport, err := NewMLFlowClient(cfg, logger)
		if err != nil {
			t.Fatalf("NewMLFlowClient() err = %v", err)
		}
		if probes.Load() != 1 {
			t.Fatalf("startup probes = %d, want 1 (no startup retries)", probes.Load())
		}
		if client.WorkspacesEnabled() || workspaceSupport.Resolved() {
			t.Fatal("expected unknown support after failed startup probe")
		}
		// EnsureWorkspace no longer probes; service PrepareClient retries resolution.
		if err := client.EnsureWorkspace(); err != nil {
			t.Fatalf("EnsureWorkspace() = %v", err)
		}
		if probes.Load() != 1 {
			t.Fatalf("probes after EnsureWorkspace = %d, want 1 (no client-side re-probe)", probes.Load())
		}
		prepared, err := workspaceSupport.PrepareClient(t.Context(), client)
		if err != nil {
			t.Fatalf("PrepareClient() = %v", err)
		}
		if !prepared.WorkspacesEnabled() || !workspaceSupport.Enabled() {
			t.Fatal("expected workspaces enabled after PrepareClient retry")
		}
		if probes.Load() < 2 {
			t.Fatalf("probes = %d, want at least 2 after PrepareClient retry", probes.Load())
		}
	})

	t.Run("unreachable startup probe leaves unknown", func(t *testing.T) {
		t.Parallel()
		cfg := mlflowServiceConfig(t, "http://127.0.0.1:1", func(m *config.MLFlowConfig) {
			m.Workspace = "pending-ws"
		})
		client, workspaceSupport, err := NewMLFlowClient(cfg, logger)
		if err != nil {
			t.Fatalf("NewMLFlowClient() err = %v", err)
		}
		if client == nil {
			t.Fatal("expected client")
		}
		if client.WorkspacesEnabled() || workspaceSupport.Resolved() {
			t.Fatal("expected unknown support after failed startup probe")
		}
	})

	t.Run("disabled server is resolved at startup", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(mlflowclient.ServerInfoResponse{WorkspacesEnabled: false})
		}))
		t.Cleanup(srv.Close)

		cfg := mlflowServiceConfig(t, srv.URL, func(m *config.MLFlowConfig) {
			m.Workspace = "ignored-ws"
		})
		client, workspaceSupport, err := NewMLFlowClient(cfg, logger)
		if err != nil {
			t.Fatalf("NewMLFlowClient() err = %v", err)
		}
		if !workspaceSupport.Resolved() {
			t.Fatal("expected resolved after definitive startup probe")
		}
		if client.WorkspacesEnabled() {
			t.Fatal("expected workspaces disabled")
		}
	})

	t.Run("unresolved probe returns mlflow_request_failed", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()
		client := mlflowclient.NewClient("http://127.0.0.1:1").WithContext(ctx)
		_, _, err := GetOrCreateExperimentID(client, nil, &api.EvaluationJobConfig{
			Experiment: &api.ExperimentConfig{Name: "demo"},
		}, "job-1")
		assertServiceErrorCode(t, err, messages.MLFlowRequestFailed)
	})

	t.Run("invalid CA certificate path", func(t *testing.T) {
		t.Parallel()
		cfg := mlflowServiceConfig(t, "http://localhost:5000", func(m *config.MLFlowConfig) {
			m.CACertPath = filepath.Join(t.TempDir(), "missing-ca.pem")
		})
		_, _, err := NewMLFlowClient(cfg, logger)
		if err == nil {
			t.Fatal("expected error for missing CA file")
		}
		if !strings.Contains(err.Error(), "failed to read MLflow CA certificate") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("invalid CA certificate PEM", func(t *testing.T) {
		t.Parallel()
		caPath := filepath.Join(t.TempDir(), "bad-ca.pem")
		if err := os.WriteFile(caPath, []byte("not a certificate"), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg := mlflowServiceConfig(t, "http://localhost:5000", func(m *config.MLFlowConfig) {
			m.CACertPath = caPath
		})
		_, _, err := NewMLFlowClient(cfg, logger)
		if err == nil {
			t.Fatal("expected error for invalid CA PEM")
		}
		if !strings.Contains(err.Error(), "no valid PEM certificates") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestHasExperimentName(t *testing.T) {
	t.Parallel()

	if HasExperimentName(&api.EvaluationJobConfig{}) {
		t.Fatal("missing experiment should be false")
	}
	if HasExperimentName(&api.EvaluationJobConfig{Experiment: &api.ExperimentConfig{Name: "  "}}) {
		t.Fatal("whitespace name should be false")
	}
	if !HasExperimentName(&api.EvaluationJobConfig{Experiment: &api.ExperimentConfig{Name: "demo"}}) {
		t.Fatal("expected true for non-empty name")
	}
}

func TestInjectEvaluationJobTags(t *testing.T) {
	t.Parallel()

	desc := "job description"
	tags := injectEvaluationJobTags("job-1", &api.EvaluationJobConfig{
		Name:        "eval-name",
		Description: &desc,
		Experiment:  &api.ExperimentConfig{Name: "exp", Tags: []api.ExperimentTag{{Key: "custom", Value: "v"}}},
	})
	if len(tags) != 5 {
		t.Fatalf("len(tags) = %d, want 5", len(tags))
	}
	tagMap := make(map[string]string, len(tags))
	for _, tag := range tags {
		tagMap[tag.Key] = tag.Value
	}
	if tagMap["custom"] != "v" {
		t.Fatalf("custom tag = %q", tagMap["custom"])
	}
	if tagMap["context"] != "eval-hub" {
		t.Fatalf("context tag = %q", tagMap["context"])
	}
	if tagMap["evaluation_job_name"] != "eval-name" {
		t.Fatalf("evaluation_job_name = %q", tagMap["evaluation_job_name"])
	}
	if tagMap["evaluation_job_id"] != "job-1" {
		t.Fatalf("evaluation_job_id = %q", tagMap["evaluation_job_id"])
	}
	if tagMap["evaluation_job_description"] != desc {
		t.Fatalf("evaluation_job_description = %q", tagMap["evaluation_job_description"])
	}
}

func TestGetOrCreateExperimentID(t *testing.T) {
	t.Parallel()
	logger := discardTestLogger()

	t.Run("no experiment name", func(t *testing.T) {
		t.Parallel()
		id, url, err := GetOrCreateExperimentID(mlflowclient.NewClient("http://example"), nil, &api.EvaluationJobConfig{}, "job-1")
		if err != nil || id != "" || url != "" {
			t.Fatalf("got id=%q url=%q err=%v", id, url, err)
		}
	})

	t.Run("nil client", func(t *testing.T) {
		t.Parallel()
		_, _, err := GetOrCreateExperimentID(nil, nil, &api.EvaluationJobConfig{
			Experiment: &api.ExperimentConfig{Name: "demo"},
		}, "job-1")
		assertServiceErrorCode(t, err, messages.MLFlowRequiredForExperiment)
	})

	t.Run("returns existing active experiment", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/2.0/mlflow/experiments/get-by-name" {
				_ = json.NewEncoder(w).Encode(mlflowclient.GetExperimentResponse{
					Experiment: mlflowclient.Experiment{
						ExperimentID:   "exp-1",
						Name:           "demo",
						LifecycleStage: "active",
					},
				})
				return
			}
			http.NotFound(w, r)
		}))
		t.Cleanup(srv.Close)

		client := mlflowclient.NewClient(srv.URL).WithContext(t.Context()).WithLogger(logger)
		id, url, err := GetOrCreateExperimentID(client, nil, &api.EvaluationJobConfig{
			Experiment: &api.ExperimentConfig{Name: "demo"},
		}, "job-1")
		if err != nil {
			t.Fatalf("GetOrCreateExperimentID() err = %v", err)
		}
		if id != "exp-1" {
			t.Fatalf("experiment id = %q, want exp-1", id)
		}
		if url != client.GetExperimentsURL() {
			t.Fatalf("url = %q, want %q", url, client.GetExperimentsURL())
		}
	})

	t.Run("creates experiment when missing", func(t *testing.T) {
		t.Parallel()
		var createBody mlflowclient.CreateExperimentRequest
		var getCalls int
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/2.0/mlflow/experiments/get-by-name":
				getCalls++
				if getCalls == 1 {
					http.Error(w, `{"error_code":"RESOURCE_DOES_NOT_EXIST","message":"not found"}`, http.StatusNotFound)
					return
				}
				_ = json.NewEncoder(w).Encode(mlflowclient.GetExperimentResponse{
					Experiment: mlflowclient.Experiment{
						ExperimentID:   "new-exp",
						Name:           "demo",
						LifecycleStage: "active",
					},
				})
			case "/api/2.0/mlflow/experiments/create":
				if err := json.NewDecoder(r.Body).Decode(&createBody); err != nil {
					t.Errorf("decode create body: %v", err)
				}
				_ = json.NewEncoder(w).Encode(mlflowclient.CreateExperimentResponse{ExperimentID: "new-exp"})
			default:
				http.NotFound(w, r)
			}
		}))
		t.Cleanup(srv.Close)

		client := mlflowclient.NewClient(srv.URL).WithContext(t.Context()).WithLogger(logger)
		id, _, err := GetOrCreateExperimentID(client, nil, &api.EvaluationJobConfig{
			Name:       "eval",
			Experiment: &api.ExperimentConfig{Name: "demo"},
		}, "job-99")
		if err != nil {
			t.Fatalf("GetOrCreateExperimentID() err = %v", err)
		}
		if id != "new-exp" {
			t.Fatalf("experiment id = %q, want new-exp", id)
		}
		if createBody.Name != "demo" {
			t.Fatalf("create name = %q", createBody.Name)
		}
		foundJobTag := false
		for _, tag := range createBody.Tags {
			if tag.Key == "evaluation_job_id" && tag.Value == "job-99" {
				foundJobTag = true
			}
		}
		if !foundJobTag {
			t.Fatal("expected evaluation_job_id tag on create request")
		}
		if getCalls != 2 {
			t.Fatalf("getCalls = %d, want 2", getCalls)
		}
	})

	t.Run("non-404 get error", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/2.0/mlflow/experiments/get-by-name" {
				http.Error(w, `{"error_code":"INTERNAL_ERROR","message":"boom"}`, http.StatusInternalServerError)
				return
			}
			http.NotFound(w, r)
		}))
		t.Cleanup(srv.Close)

		client := mlflowclient.NewClient(srv.URL).WithContext(t.Context()).WithLogger(logger)
		_, _, err := GetOrCreateExperimentID(client, nil, &api.EvaluationJobConfig{
			Experiment: &api.ExperimentConfig{Name: "demo"},
		}, "job-1")
		assertServiceErrorCode(t, err, messages.MLFlowRequestFailed)
	})
}

func assertServiceErrorCode(t *testing.T, err error, want *messages.MessageCode) {
	t.Helper()
	var se *serviceerrors.ServiceError
	if !errors.As(err, &se) {
		t.Fatalf("expected ServiceError, got %T: %v", err, err)
	}
	if se.MessageCode() != want {
		t.Fatalf("message code = %v, want %v", se.MessageCode(), want)
	}
}
