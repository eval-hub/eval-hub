package mlflow

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/mlflowclient"
)

func testLogger() *slog.Logger {
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
	logger := testLogger()

	t.Run("no tracking URI", func(t *testing.T) {
		t.Parallel()
		client, err := NewMLFlowClient(&config.Config{MLFlow: &config.MLFlowConfig{}}, logger)
		if err != nil {
			t.Fatalf("NewMLFlowClient() err = %v", err)
		}
		if client != nil {
			t.Fatal("expected nil client when tracking URI is unset")
		}
	})

	t.Run("probes workspaces and configures workspace", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/3.0/mlflow/server-info":
				_ = json.NewEncoder(w).Encode(mlflowclient.ServerInfoResponse{WorkspacesEnabled: true})
			default:
				http.NotFound(w, r)
			}
		}))
		t.Cleanup(srv.Close)

		cfg := mlflowServiceConfig(t, srv.URL, func(m *config.MLFlowConfig) {
			m.Workspace = "prod-ws"
			m.Token = "static-token"
		})
		client, err := NewMLFlowClient(cfg, logger)
		if err != nil {
			t.Fatalf("NewMLFlowClient() err = %v", err)
		}
		if !client.WorkspacesEnabled() {
			t.Fatal("expected workspaces enabled after probe")
		}
	})

	t.Run("probe failure still returns client", func(t *testing.T) {
		t.Parallel()
		cfg := mlflowServiceConfig(t, "http://127.0.0.1:1", nil)
		client, err := NewMLFlowClient(cfg, logger)
		if err != nil {
			t.Fatalf("NewMLFlowClient() err = %v", err)
		}
		if client == nil {
			t.Fatal("expected client when probe fails")
		}
		if client.WorkspacesEnabled() {
			t.Fatal("expected workspaces disabled when probe fails")
		}
	})

	t.Run("workspace ignored when server disables workspaces", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(mlflowclient.ServerInfoResponse{WorkspacesEnabled: false})
		}))
		t.Cleanup(srv.Close)

		cfg := mlflowServiceConfig(t, srv.URL, func(m *config.MLFlowConfig) {
			m.Workspace = "ignored-ws"
		})
		client, err := NewMLFlowClient(cfg, logger)
		if err != nil {
			t.Fatalf("NewMLFlowClient() err = %v", err)
		}
		if client.WorkspacesEnabled() {
			t.Fatal("expected workspaces disabled")
		}
	})

	t.Run("invalid CA certificate path", func(t *testing.T) {
		t.Parallel()
		cfg := mlflowServiceConfig(t, "http://localhost:5000", func(m *config.MLFlowConfig) {
			m.CACertPath = filepath.Join(t.TempDir(), "missing-ca.pem")
		})
		_, err := NewMLFlowClient(cfg, logger)
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
		_, err := NewMLFlowClient(cfg, logger)
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
	logger := testLogger()

	t.Run("no experiment name", func(t *testing.T) {
		t.Parallel()
		id, url, err := GetOrCreateExperimentID(mlflowclient.NewClient("http://example"), &api.EvaluationJobConfig{}, "job-1")
		if err != nil || id != "" || url != "" {
			t.Fatalf("got id=%q url=%q err=%v", id, url, err)
		}
	})

	t.Run("nil client", func(t *testing.T) {
		t.Parallel()
		_, _, err := GetOrCreateExperimentID(nil, &api.EvaluationJobConfig{
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
		id, url, err := GetOrCreateExperimentID(client, &api.EvaluationJobConfig{
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
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/2.0/mlflow/experiments/get-by-name":
				http.Error(w, `{"error_code":"RESOURCE_DOES_NOT_EXIST","message":"not found"}`, http.StatusNotFound)
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
		id, _, err := GetOrCreateExperimentID(client, &api.EvaluationJobConfig{
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
		_, _, err := GetOrCreateExperimentID(client, &api.EvaluationJobConfig{
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
