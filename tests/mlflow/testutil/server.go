// Package testutil starts and stops a local MLflow tracking server for integration tests.
// It shells out to the Makefile and scripts under tests/mlflow.
package testutil

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	defaultHost    = "127.0.0.1"
	defaultPort    = 5000
	startTimeout   = 5 * time.Minute
	stopTimeout    = 30 * time.Second
	healthTimeout  = 30 * time.Second
	healthInterval = 500 * time.Millisecond
)

// startMu serializes install/start/stop against the shared tests/mlflow/.venv.
// Script-level locking also applies cross-process; this covers same-process callers.
var startMu sync.Mutex

// ServerOptions configures MLflow server startup (passed to tests/mlflow Makefile targets).
type ServerOptions struct {
	Version          string
	EnableWorkspaces bool
	Host             string
	Port             int
	BackendStoreURI  string
	ArtifactRoot     string
	Logger           *slog.Logger
}

// Server represents a running MLflow tracking server started for a test.
type Server struct {
	TrackingURI string
	options     ServerOptions
	dir         string
}

// StartMLFlowServer installs (if needed) and starts MLflow via make start-mlflow.
// On failure it fails the test.
func StartMLFlowServer(t *testing.T, opts ServerOptions) *Server {
	t.Helper()

	dir, err := findMLFlowDir()
	if err != nil {
		opts.Logger.Error("Skipping tests: MLflow test directory not found", "error", err)
		t.Skipf("MLflow test directory not found: %v", err)
	}

	opts = opts.withDefaults()
	trackingURI := fmt.Sprintf("http://%s:%d", opts.Host, opts.Port)

	startMu.Lock()
	defer startMu.Unlock()

	if err := runMakeTarget(t, dir, startTimeout, "start-mlflow", opts); err != nil {
		opts.Logger.Error("Skipping tests: failed to start MLflow server", "error", err, "version", opts.Version, "workspaces", opts.EnableWorkspaces, "port", opts.Port)
		t.Fatalf(
			"failed to start MLflow server (version=%s workspaces=%t port=%d): %v",
			opts.Version, opts.EnableWorkspaces, opts.Port, err,
		)
	}

	if err := waitForHealth(trackingURI, healthTimeout); err != nil {
		_ = runMakeTarget(t, dir, stopTimeout, "stop-mlflow", opts)
		opts.Logger.Error("Skipping tests: MLflow server at %s did not become healthy", "error", err, "trackingURI", trackingURI)
		t.Fatalf("MLflow server at %s did not become healthy: %v", trackingURI, err)
	}

	srv := &Server{
		TrackingURI: trackingURI,
		options:     opts,
		dir:         dir,
	}
	t.Cleanup(func() {
		startMu.Lock()
		defer startMu.Unlock()
		srv.stop(t)
	})
	opts.Logger.Info("MLflow server ready", "trackingURI", trackingURI, "version", opts.Version, "workspaces", opts.EnableWorkspaces)
	return srv
}

func (s *Server) stop(t *testing.T) {
	t.Helper()
	if err := runMakeTarget(t, s.dir, stopTimeout, "stop-mlflow", s.options); err != nil {
		s.options.Logger.Error("failed to stop MLflow server", "error", err)
	}
}

func (opts ServerOptions) withDefaults() ServerOptions {
	if opts.Version == "" {
		opts.Version = "3.13.0"
	}
	if opts.Host == "" {
		opts.Host = defaultHost
	}
	if opts.Port == 0 {
		opts.Port = defaultPort
	}
	if opts.BackendStoreURI == "" {
		opts.BackendStoreURI = fmt.Sprintf("sqlite:///bin/mlflow_test_%d.db", opts.Port)
	}
	if opts.ArtifactRoot == "" {
		opts.ArtifactRoot = fmt.Sprintf("./bin/mlruns_test_%d", opts.Port)
	}
	return opts
}

func (opts ServerOptions) env() []string {
	opts = opts.withDefaults()
	enableWorkspaces := "false"
	if opts.EnableWorkspaces {
		enableWorkspaces = "true"
	}
	return []string{
		"MLFLOW_VERSION=" + opts.Version,
		"MLFLOW_HOST=" + opts.Host,
		"MLFLOW_PORT=" + strconv.Itoa(opts.Port),
		"MLFLOW_BACKEND_STORE_URI=" + opts.BackendStoreURI,
		"MLFLOW_DEFAULT_ARTIFACT_ROOT=" + opts.ArtifactRoot,
		"MLFLOW_ENABLE_WORKSPACES=" + enableWorkspaces,
		"MLFLOW_TRACKING_URI=" + fmt.Sprintf("http://%s:%d", opts.Host, opts.Port),
	}
}

func findMLFlowDir() (string, error) {
	return findDir(filepath.Join("tests", "mlflow"), ".", "..", "../..", "../../..", "../../../..")
}

func findDir(dirName string, dirs ...string) (string, error) {
	var tried []string
	for _, dir := range dirs {
		name, err := filepath.Abs(filepath.Join(dir, dirName))
		if err != nil {
			return "", fmt.Errorf("resolve path for %s: %w", filepath.Join(dir, dirName), err)
		}
		tried = append(tried, name)
		if info, err := os.Stat(name); err == nil && info.IsDir() {
			return name, nil
		}
	}
	return "", fmt.Errorf("directory %s not found (tried %v)", dirName, tried)
}

func runMakeTarget(t *testing.T, dir string, timeout time.Duration, target string, opts ServerOptions) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	logPath, err := makeOutputLogPath(dir, opts)
	if err != nil {
		return fmt.Errorf("resolve make output log path: %w", err)
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open make output log %s: %w", logPath, err)
	}
	defer logFile.Close()

	if _, err := fmt.Fprintf(logFile, "\n--- make %s (%s) at %s ---\n", target, opts.Version, time.Now().Format(time.RFC3339)); err != nil {
		return fmt.Errorf("write make log header: %w", err)
	}

	cmd := exec.CommandContext(ctx, "make", target)
	cmd.Dir = dir
	if os.Getenv("MLFLOW_SHOW_LOGS") == "true" {
		cmd.Stdout = io.MultiWriter(logFile, os.Stdout)
		cmd.Stderr = io.MultiWriter(logFile, os.Stderr)
	} else {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}
	cmd.Env = append(os.Environ(), opts.env()...)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("make %s in %s (log: %s): %w", target, dir, logPath, err)
	}
	return nil
}

func makeOutputLogPath(mlflowDir string, opts ServerOptions) (string, error) {
	opts = opts.withDefaults()
	binDir, err := repoBinDirFromMLFlowDir(mlflowDir)
	if err != nil {
		return "", err
	}
	name := "mlflow_" + strings.ReplaceAll(opts.Version, ".", "_")
	if opts.EnableWorkspaces {
		name += "_workspaces"
	}
	return filepath.Join(binDir, name+".log"), nil
}

func repoBinDirFromMLFlowDir(mlflowDir string) (string, error) {
	repoRoot := filepath.Clean(filepath.Join(mlflowDir, "..", ".."))
	binDir := filepath.Join(repoRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return "", fmt.Errorf("create bin directory at %s: %w", binDir, err)
	}
	return binDir, nil
}

func waitForHealth(trackingURI string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	healthURL := trackingURI + "/health"
	client := &http.Client{Timeout: 2 * time.Second}

	for time.Now().Before(deadline) {
		resp, err := client.Get(healthURL)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(healthInterval)
	}
	return fmt.Errorf("timed out after %s waiting for %s", timeout, healthURL)
}
