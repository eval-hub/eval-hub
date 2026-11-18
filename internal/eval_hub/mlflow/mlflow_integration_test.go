package mlflow

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

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

// firstIntegrationPort is the port handed to the first integration case.
const firstIntegrationPort = 5010

type mlflowIntegrationCase struct {
	name             string
	version          string
	enableWorkspaces bool
	port             int
}

// mlflowIntegrationCases resolves the MLflow releases to exercise at run time:
// every minor release line from testutil.MinVersion (3.8.1) to the newest stable
// release published on GitHub, using that line's newest patch. Each release runs
// twice — workspaces disabled and enabled — starting at
// testutil.WorkspacesMinVersion (3.10.0), the first release whose
// `mlflow server` accepts --enable-workspaces.
//
// When the GitHub API cannot be reached (offline, CI, rate limited) the pinned
// fallback list is used instead of failing.
func mlflowIntegrationCases(t *testing.T, logger *slog.Logger) []mlflowIntegrationCase {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	versions, err := testutil.Versions(ctx, testutil.MinVersion)
	switch {
	case err != nil:
		logger.Warn("Could not resolve MLflow versions from GitHub; using pinned fallback list", "error", err.Error())
		t.Logf("Could not resolve MLflow versions from GitHub (%v); using pinned fallback list", err)
		versions = testutil.FallbackVersions()
	case len(versions) == 0:
		logger.Warn("GitHub returned no MLflow versions; using pinned fallback list")
		t.Logf("GitHub returned no MLflow versions; using pinned fallback list")
		versions = testutil.FallbackVersions()
	}

	var cases []mlflowIntegrationCase
	for _, version := range versions {
		cases = append(cases, mlflowIntegrationCase{
			name:    "v" + version + "-default",
			version: version,
			port:    firstIntegrationPort + len(cases),
		})
		if testutil.VersionAtLeast(version, testutil.WorkspacesMinVersion) {
			cases = append(cases, mlflowIntegrationCase{
				name:             "v" + version + "-workspaces",
				version:          version,
				enableWorkspaces: true,
				port:             firstIntegrationPort + len(cases),
			})
		}
	}

	logger.Info("Resolved MLflow integration versions", "versions", versions, "cases", len(cases))
	t.Logf("Running %d MLflow integration cases for versions %v", len(cases), versions)
	return cases
}

func TestMLFlowIntegration(t *testing.T) {
	for _, tc := range mlflowIntegrationCases(t, testLogger()) {
		logger := testLogger()
		logger.Info("Starting mlflow test", "name", tc.name, "version", tc.version, "enableWorkspaces", tc.enableWorkspaces, "port", tc.port)
		t.Run(tc.name, func(t *testing.T) {
			srv := testutil.StartMLFlowServer(t, testutil.ServerOptions{
				Version:          tc.version,
				EnableWorkspaces: tc.enableWorkspaces,
				Port:             tc.port,
				Logger:           logger,
			})

			t.Run("NewMLFlowClient probes once then EnsureWorkspace uses result", func(t *testing.T) {
				cfg := mlflowServiceConfig(t, srv.TrackingURI, func(m *config.MLFlowConfig) {
					if tc.enableWorkspaces {
						m.Workspace = "integration-workspace"
					}
				})
				client, workspaceSupport, err := NewMLFlowClient(cfg, logger)
				if err != nil {
					t.Fatalf("NewMLFlowClient() err = %v", err)
				}
				if client == nil {
					t.Fatal("expected non-nil client")
				}
				if !workspaceSupport.Resolved() {
					t.Fatal("expected workspace support resolved by startup probe")
				}
				if client.WorkspacesEnabled() != tc.enableWorkspaces {
					t.Fatalf("WorkspacesEnabled() = %t, want %t", client.WorkspacesEnabled(), tc.enableWorkspaces)
				}
				if err := client.EnsureWorkspace(); err != nil {
					t.Fatalf("EnsureWorkspace() = %v", err)
				}
				t.Logf("workspaces_enabled=%t", client.WorkspacesEnabled())
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
				id, url, err := GetOrCreateExperimentID(client, nil, &api.EvaluationJobConfig{
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
					id, url, err := GetOrCreateExperimentID(client, nil, &api.EvaluationJobConfig{
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
