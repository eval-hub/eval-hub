package mlflow

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/mlflowclient"
	"github.com/eval-hub/eval-hub/tests/mlflow/testutil"
	"github.com/google/uuid"
)

var (
	integrationTestLoggerOnce sync.Once
	integrationTestLogger     *slog.Logger
)

func testLogger() *slog.Logger {
	integrationTestLoggerOnce.Do(func() {
		logPath := filepath.Join(repoBinDir(), "mlflow_tests.log")
		logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			panic(fmt.Sprintf("failed to create MLflow integration test log at %s: %v", logPath, err))
		}
		integrationTestLogger = slog.New(slog.NewJSONHandler(logFile, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		}))
	})
	return integrationTestLogger
}

func repoBinDir() string {
	dir, err := os.Getwd()
	if err != nil {
		panic(fmt.Sprintf("failed to get working directory: %v", err))
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			binDir := filepath.Join(dir, "bin")
			if err := os.MkdirAll(binDir, 0o755); err != nil {
				panic(fmt.Sprintf("failed to create bin directory at %s: %v", binDir, err))
			}
			return binDir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			panic("go.mod not found while resolving bin directory for MLflow integration tests")
		}
		dir = parent
	}
}

// MLflow versions and startup options exercised against a real tracking server.
var mlflowIntegrationCases = []struct {
	name             string
	version          string
	enableWorkspaces bool
	port             int
}{
	{name: "v3.8.1-default", version: "3.8.1", enableWorkspaces: false, port: 5010},
	{name: "v3.9.0-default", version: "3.9.0", enableWorkspaces: false, port: 5011},
	{name: "v3.10.1-default", version: "3.10.1", enableWorkspaces: false, port: 5012},
	{name: "v3.11.1-default", version: "3.11.1", enableWorkspaces: false, port: 5013},
	{name: "v3.12.0-default", version: "3.12.0", enableWorkspaces: false, port: 5014},
	{name: "v3.12.0-workspaces", version: "3.12.0", enableWorkspaces: true, port: 5015},
	{name: "v3.13.0-default", version: "3.13.0", enableWorkspaces: false, port: 5016},
	{name: "v3.13.0-workspaces", version: "3.13.0", enableWorkspaces: true, port: 5017},
}

func TestMLFlowIntegration(t *testing.T) {
	for _, tc := range mlflowIntegrationCases {
		logger := testLogger()
		logger.Info("Starting mlflow test", "name", tc.name, "version", tc.version, "enableWorkspaces", tc.enableWorkspaces, "port", tc.port)
		t.Run(tc.name, func(t *testing.T) {
			srv := testutil.StartMLFlowServer(t, testutil.ServerOptions{
				Version:          tc.version,
				EnableWorkspaces: tc.enableWorkspaces,
				Port:             tc.port,
				Logger:           logger,
			})

			t.Run("NewMLFlowClient probes server", func(t *testing.T) {
				cfg := mlflowServiceConfig(t, srv.TrackingURI, func(m *config.MLFlowConfig) {
					if tc.enableWorkspaces {
						m.Workspace = "integration-workspace"
					}
				})
				client, err := NewMLFlowClient(cfg, logger)
				if err != nil {
					t.Fatalf("NewMLFlowClient() err = %v", err)
				}
				if client == nil {
					t.Fatal("expected non-nil client")
				}
				if client.WorkspacesEnabled() != tc.enableWorkspaces {
					t.Fatalf("WorkspacesEnabled() = %t, want %t", client.WorkspacesEnabled(), tc.enableWorkspaces)
				}
				t.Logf("NewMLFlowClient: workspaces_enabled=%t", client.WorkspacesEnabled())
				// TODO client.GetVersion()
			})

			t.Run("GetOrCreateExperimentID without workspace", func(t *testing.T) {
				client := mlflowclient.NewClient(srv.TrackingURI).
					WithContext(t.Context()).
					WithLogger(logger)
				workspacesEnabled, err := client.ProbeWorkspacesEnabled()
				if err != nil {
					t.Logf("workspace probe failed: %v", err)
					workspacesEnabled = false
				}
				client = client.WithWorkspacesSupport(workspacesEnabled)

				expName := fmt.Sprintf("test-exp-no-ws-%s-%s", tc.name, uuid.New().String())
				id, url, err := GetOrCreateExperimentID(client, &api.EvaluationJobConfig{
					Name:       "eval-job",
					Experiment: &api.ExperimentConfig{Name: expName},
				}, "job-1")
				if err != nil || id == "" || url == "" {
					t.Fatalf("got id=%q url=%q err=%v", id, url, err)
				}
				t.Logf("created experiment: id=%q url=%q", id, url)
			})

			if tc.enableWorkspaces {
				t.Run("GetOrCreateExperimentID with workspace", func(t *testing.T) {
					client := mlflowclient.NewClient(srv.TrackingURI).
						WithContext(t.Context()).
						WithLogger(logger)
					workspacesEnabled, err := client.ProbeWorkspacesEnabled()
					if err != nil {
						t.Fatalf("workspace probe failed: %v", err)
					}
					if !workspacesEnabled {
						t.Fatal("expected workspaces to be enabled on server")
					}
					client = client.WithWorkspacesSupport(true).WithWorkspace("integration-workspace")

					expName := fmt.Sprintf("test-exp-ws-%s-%s", tc.name, uuid.New().String())
					id, url, err := GetOrCreateExperimentID(client, &api.EvaluationJobConfig{
						Name:       "eval-job-ws",
						Experiment: &api.ExperimentConfig{Name: expName},
					}, "job-2")
					if err != nil || id == "" || url == "" {
						t.Fatalf("got id=%q url=%q err=%v", id, url, err)
					}
					t.Logf("created workspace experiment: id=%q url=%q", id, url)
				})
			}
		})
		logger.Info("Finished mlflow test", "name", tc.name, "version", tc.version, "enableWorkspaces", tc.enableWorkspaces, "port", tc.port)
	}
}
