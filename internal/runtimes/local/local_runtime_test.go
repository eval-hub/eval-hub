package local

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/constants"
	"github.com/eval-hub/eval-hub/internal/runtimes/shared"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// fakeStorage implements [abstractions.Storage] for testing.
type fakeStorage struct {
	logger        *slog.Logger
	called        bool
	ctx           context.Context
	runStatus     *api.StatusEvent
	runStatusChan chan *api.StatusEvent
	updateErr     error
}

func (f *fakeStorage) UpdateEvaluationJob(id string, runStatus *api.StatusEvent) error {
	f.called = true
	f.runStatus = runStatus
	if f.runStatusChan != nil {
		select {
		case f.runStatusChan <- runStatus:
		default:
		}
	}
	return f.updateErr
}

func (f *fakeStorage) Ping(_ time.Duration) error                             { return nil }
func (f *fakeStorage) CreateEvaluationJob(_ *api.EvaluationJobResource) error { return nil }
func (f *fakeStorage) GetEvaluationJob(_ string) (*api.EvaluationJobResource, error) {
	return nil, nil
}
func (f *fakeStorage) GetEvaluationJobs(_ int, _ int, _ string) (*abstractions.QueryResults[api.EvaluationJobResource], error) {
	return nil, nil
}
func (f *fakeStorage) DeleteEvaluationJob(_ string) error { return nil }
func (f *fakeStorage) UpdateEvaluationJobStatus(_ string, _ api.OverallState, _ *api.MessageInfo) error {
	f.called = true
	return nil
}
func (f *fakeStorage) CreateCollection(_ *api.CollectionResource) error { return nil }
func (f *fakeStorage) GetCollection(_ string, _ bool) (*api.CollectionResource, error) {
	return nil, nil
}
func (f *fakeStorage) GetCollections(_ int, _ int) (*abstractions.QueryResults[api.CollectionResource], error) {
	return nil, nil
}
func (f *fakeStorage) UpdateCollection(_ *api.CollectionResource) error { return nil }
func (f *fakeStorage) DeleteCollection(_ string) error                  { return nil }
func (f *fakeStorage) Close() error                                     { return nil }

func (f *fakeStorage) WithLogger(logger *slog.Logger) abstractions.Storage {
	return &fakeStorage{
		logger:        logger,
		ctx:           f.ctx,
		runStatusChan: f.runStatusChan,
		updateErr:     f.updateErr,
	}
}

func (f *fakeStorage) WithContext(ctx context.Context) abstractions.Storage {
	return &fakeStorage{
		logger:        f.logger,
		ctx:           ctx,
		runStatusChan: f.runStatusChan,
		updateErr:     f.updateErr,
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// testContext returns a context with a 10-second deadline tied to t.Cleanup.
// All process-spawning tests should use this to prevent hangs.
func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func sampleEvaluation(providerID string) *api.EvaluationJobResource {
	return &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{ID: "job-1"},
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{
				URL:  "http://model.example",
				Name: "model-1",
			},
			Benchmarks: []api.BenchmarkConfig{
				{
					Ref:        api.Ref{ID: "bench-1"},
					ProviderID: providerID,
					Parameters: map[string]any{
						"foo":          "bar",
						"num_examples": 5,
					},
				},
			},
			Experiment: &api.ExperimentConfig{
				Name: "exp-1",
			},
		},
	}
}

func sampleLocalProviders(providerID, command string) map[string]api.ProviderResource {
	return map[string]api.ProviderResource{
		providerID: {
			ID: providerID,
			Runtime: &api.Runtime{
				Local: &api.LocalRuntime{
					Command: command,
					Env: []api.EnvVar{
						{Name: "TEST_VAR", Value: "test_value"},
					},
				},
			},
		},
	}
}

func cleanupDir(t *testing.T, jobID, providerID, benchmarkID string) {
	t.Helper()
	dirName := shared.JobName(jobID, providerID, benchmarkID)
	t.Cleanup(func() {
		os.RemoveAll(filepath.Join(localRunDir, dirName))
	})
}

func waitForFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for file %s", path)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestLocalRuntimeName(t *testing.T) {
	rt := &LocalRuntime{}
	if rt.Name() != "local" {
		t.Fatalf("expected Name() to return %q, got %q", "local", rt.Name())
	}
}

func TestNewLocalRuntime(t *testing.T) {
	rt, err := NewLocalRuntime(discardLogger(), nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if rt == nil {
		t.Fatal("expected non-nil runtime")
	}
}

func TestRunEvaluationJobWritesJobSpec(t *testing.T) {
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)
	dirName := shared.JobName("job-1", providerID, "bench-1")
	sentinelPath := filepath.Join(localRunDir, dirName, "done")
	providers := sampleLocalProviders(providerID, fmt.Sprintf("touch %s", sentinelPath))
	cleanupDir(t, "job-1", providerID, "bench-1")

	rt := &LocalRuntime{
		logger:    discardLogger(),
		ctx:       testContext(t),
		providers: providers,
	}

	err := rt.RunEvaluationJob(evaluation, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	waitForFile(t, sentinelPath, 5*time.Second)
	metaDir := filepath.Join(localRunDir, dirName, "meta")

	// Verify directory exists
	if _, err := os.Stat(metaDir); os.IsNotExist(err) {
		t.Fatalf("expected meta directory to exist at %s", metaDir)
	}

	// Verify job.json exists and is valid
	jobSpecPath := filepath.Join(metaDir, "job.json")
	data, err := os.ReadFile(jobSpecPath)
	if err != nil {
		t.Fatalf("expected job.json to exist, got %v", err)
	}

	var spec shared.JobSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatalf("expected valid JSON, got %v", err)
	}

	if spec.JobID != "job-1" {
		t.Fatalf("expected JobID %q, got %q", "job-1", spec.JobID)
	}
	if spec.ProviderID != providerID {
		t.Fatalf("expected ProviderID %q, got %q", providerID, spec.ProviderID)
	}
	if spec.BenchmarkID != "bench-1" {
		t.Fatalf("expected BenchmarkID %q, got %q", "bench-1", spec.BenchmarkID)
	}
	if spec.Model.Name != "model-1" {
		t.Fatalf("expected Model.Name %q, got %q", "model-1", spec.Model.Name)
	}
}

func TestRunEvaluationJobPassesEnvVar(t *testing.T) {
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)
	cleanupDir(t, "job-1", providerID, "bench-1")

	dirName := shared.JobName("job-1", providerID, "bench-1")
	outputFile := filepath.Join(localRunDir, dirName, "env_output.txt")
	sentinelPath := filepath.Join(localRunDir, dirName, "done")

	// Command writes EVALHUB_JOB_SPEC_PATH and TEST_VAR to output file
	command := fmt.Sprintf("sh -c 'echo $EVALHUB_JOB_SPEC_PATH > %s && echo $TEST_VAR >> %s && touch %s'", outputFile, outputFile, sentinelPath)
	providers := sampleLocalProviders(providerID, command)

	rt := &LocalRuntime{
		logger:    discardLogger(),
		ctx:       testContext(t),
		providers: providers,
	}

	err := rt.RunEvaluationJob(evaluation, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	waitForFile(t, sentinelPath, 5*time.Second)

	data, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("expected output file to exist, got %v", err)
	}

	output := string(data)
	// Verify EVALHUB_JOB_SPEC_PATH was set
	expectedPath := filepath.Join(localRunDir, dirName, "meta", "job.json")
	absExpectedPath, _ := filepath.Abs(expectedPath)
	if len(output) == 0 {
		t.Fatal("expected env output, got empty file")
	}

	// Parse the two lines
	lines := strings.Split(output, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines in env output, got %d: %q", len(lines), output)
	}
	if lines[0] != absExpectedPath {
		t.Fatalf("expected EVALHUB_JOB_SPEC_PATH=%q, got %q", absExpectedPath, lines[0])
	}
	if lines[1] != "test_value" {
		t.Fatalf("expected TEST_VAR=%q, got %q", "test_value", lines[1])
	}
}

func TestRunEvaluationJobNoBenchmarks(t *testing.T) {
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)
	evaluation.Benchmarks = nil

	rt := &LocalRuntime{
		logger:    discardLogger(),
		providers: sampleLocalProviders(providerID, "true"),
	}

	err := rt.RunEvaluationJob(evaluation, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no benchmarks configured") {
		t.Fatalf("expected error to contain %q, got %q", "no benchmarks configured", err.Error())
	}
}

func TestRunEvaluationJobProviderNotFound(t *testing.T) {
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)

	// Use empty providers map so provider is not found
	rt := &LocalRuntime{
		logger:    discardLogger(),
		providers: map[string]api.ProviderResource{},
	}

	err := rt.RunEvaluationJob(evaluation, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected error to contain %q, got %q", "not found", err.Error())
	}
}

func TestRunEvaluationJobMissingLocalCommand(t *testing.T) {
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)

	// Provider with nil Local runtime
	providers := map[string]api.ProviderResource{
		providerID: {
			ID:      providerID,
			Runtime: &api.Runtime{Local: nil},
		},
	}

	rt := &LocalRuntime{
		logger:    discardLogger(),
		providers: providers,
	}

	err := rt.RunEvaluationJob(evaluation, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "Local runtime is not enabled") {
		t.Fatalf("expected error to contain %q, got %q", "Local runtime is not enabled", err.Error())
	}

	// Also test with empty command
	providers[providerID] = api.ProviderResource{
		ID: providerID,
		Runtime: &api.Runtime{
			Local: &api.LocalRuntime{Command: ""},
		},
	}

	err = rt.RunEvaluationJob(evaluation, nil)
	if err == nil {
		t.Fatal("expected error for empty command, got nil")
	}
	if !strings.Contains(err.Error(), "Local runtime is not enabled") {
		t.Fatalf("expected error to contain %q, got %q", "Local runtime is not enabled", err.Error())
	}
}

func TestRunEvaluationJobProcessFailureUpdatesStorage(t *testing.T) {
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)
	providers := sampleLocalProviders(providerID, "exit 1")
	cleanupDir(t, "job-1", providerID, "bench-1")

	tctx := testContext(t)
	logger := discardLogger()
	statusCh := make(chan *api.StatusEvent, 1)
	storage := &fakeStorage{logger: logger, ctx: tctx, runStatusChan: statusCh}
	var store abstractions.Storage = storage

	rt := &LocalRuntime{
		logger:    logger,
		ctx:       tctx,
		providers: providers,
	}

	err := rt.RunEvaluationJob(evaluation, &store)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	select {
	case runStatus := <-statusCh:
		if runStatus == nil {
			t.Fatal("expected run status, got nil")
		}
		if runStatus.BenchmarkStatusEvent == nil {
			t.Fatal("expected BenchmarkStatusEvent, got nil")
		}
		if runStatus.BenchmarkStatusEvent.Status != api.StateFailed {
			t.Fatalf("expected status %q, got %q", api.StateFailed, runStatus.BenchmarkStatusEvent.Status)
		}
		if runStatus.BenchmarkStatusEvent.ID != "bench-1" {
			t.Fatalf("expected benchmark ID %q, got %q", "bench-1", runStatus.BenchmarkStatusEvent.ID)
		}
		if runStatus.BenchmarkStatusEvent.ProviderID != providerID {
			t.Fatalf("expected provider ID %q, got %q", providerID, runStatus.BenchmarkStatusEvent.ProviderID)
		}
		if runStatus.BenchmarkStatusEvent.ErrorMessage == nil {
			t.Fatal("expected ErrorMessage, got nil")
		}
		if runStatus.BenchmarkStatusEvent.ErrorMessage.MessageCode != constants.MESSAGE_CODE_EVALUATION_JOB_FAILED {
			t.Fatalf("expected message code %q, got %q",
				constants.MESSAGE_CODE_EVALUATION_JOB_FAILED,
				runStatus.BenchmarkStatusEvent.ErrorMessage.MessageCode)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for UpdateEvaluationJob to be called")
	}
}

func TestRunEvaluationJobProcessSuccess(t *testing.T) {
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)
	providers := sampleLocalProviders(providerID, "true")
	cleanupDir(t, "job-1", providerID, "bench-1")

	tctx := testContext(t)
	logger := discardLogger()
	statusCh := make(chan *api.StatusEvent, 1)
	storage := &fakeStorage{logger: logger, ctx: tctx, runStatusChan: statusCh}
	var store abstractions.Storage = storage

	rt := &LocalRuntime{
		logger:    logger,
		ctx:       tctx,
		providers: providers,
	}

	err := rt.RunEvaluationJob(evaluation, &store)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Wait briefly — storage should NOT be called with failure
	select {
	case runStatus := <-statusCh:
		t.Fatalf("expected no storage update, but got %+v", runStatus)
	case <-time.After(1 * time.Second):
		// Success: no failure status was sent
	}
}

func TestRunEvaluationJobContextCancellation(t *testing.T) {
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)
	providers := sampleLocalProviders(providerID, "sleep 10")
	cleanupDir(t, "job-1", providerID, "bench-1")

	tctx := testContext(t)
	logger := discardLogger()
	statusCh := make(chan *api.StatusEvent, 1)
	storage := &fakeStorage{logger: logger, ctx: tctx, runStatusChan: statusCh}
	var store abstractions.Storage = storage

	ctx, cancel := context.WithCancel(tctx)

	rt := &LocalRuntime{
		logger:    logger,
		ctx:       ctx,
		providers: providers,
	}

	err := rt.RunEvaluationJob(evaluation, &store)
	if err != nil {
		t.Fatalf("expected no synchronous error, got %v", err)
	}

	// Cancel after process has started
	cancel()

	select {
	case runStatus := <-statusCh:
		if runStatus == nil {
			t.Fatal("expected run status, got nil")
		}
		if runStatus.BenchmarkStatusEvent.Status != api.StateFailed {
			t.Fatalf("expected status %q, got %q", api.StateFailed, runStatus.BenchmarkStatusEvent.Status)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for process cancellation to update storage")
	}
}

func TestRunEvaluationJobMultipleBenchmarksWarning(t *testing.T) {
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)
	// Add a second benchmark
	evaluation.Benchmarks = append(evaluation.Benchmarks, api.BenchmarkConfig{
		Ref:        api.Ref{ID: "bench-2"},
		ProviderID: providerID,
		Parameters: map[string]any{"baz": "qux"},
	})
	dirName := shared.JobName("job-1", providerID, "bench-1")
	sentinelPath := filepath.Join(localRunDir, dirName, "done")
	providers := sampleLocalProviders(providerID, fmt.Sprintf("touch %s", sentinelPath))
	cleanupDir(t, "job-1", providerID, "bench-1")

	rt := &LocalRuntime{
		logger:    discardLogger(),
		ctx:       testContext(t),
		providers: providers,
	}

	err := rt.RunEvaluationJob(evaluation, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	waitForFile(t, sentinelPath, 5*time.Second)

	// Only the first benchmark directory should exist
	dir1 := filepath.Join(localRunDir, shared.JobName("job-1", providerID, "bench-1"))
	if _, err := os.Stat(dir1); os.IsNotExist(err) {
		t.Fatal("expected directory for first benchmark to exist")
	}

	dir2 := filepath.Join(localRunDir, shared.JobName("job-1", providerID, "bench-2"))
	if _, err := os.Stat(dir2); !os.IsNotExist(err) {
		os.RemoveAll(dir2) // Clean up before failing
		t.Fatal("expected directory for second benchmark to NOT exist")
	}
}

func TestRunEvaluationJobCallbackURL(t *testing.T) {
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)
	dirName := shared.JobName("job-1", providerID, "bench-1")
	sentinelPath := filepath.Join(localRunDir, dirName, "done")
	providers := sampleLocalProviders(providerID, fmt.Sprintf("touch %s", sentinelPath))
	cleanupDir(t, "job-1", providerID, "bench-1")

	t.Setenv("SERVICE_URL", "http://localhost:8080")

	rt := &LocalRuntime{
		logger:    discardLogger(),
		ctx:       testContext(t),
		providers: providers,
	}

	err := rt.RunEvaluationJob(evaluation, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	waitForFile(t, sentinelPath, 5*time.Second)
	jobSpecPath := filepath.Join(localRunDir, dirName, "meta", "job.json")
	data, err := os.ReadFile(jobSpecPath)
	if err != nil {
		t.Fatalf("expected job.json to exist, got %v", err)
	}

	var spec shared.JobSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatalf("expected valid JSON, got %v", err)
	}

	if spec.CallbackURL == nil {
		t.Fatal("expected callback_url to be set, got nil")
	}
	if *spec.CallbackURL != "http://localhost:8080" {
		t.Fatalf("expected callback_url %q, got %q", "http://localhost:8080", *spec.CallbackURL)
	}
}

func TestRunEvaluationJobCallbackURLNotSet(t *testing.T) {
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)
	dirName := shared.JobName("job-1", providerID, "bench-1")
	sentinelPath := filepath.Join(localRunDir, dirName, "done")
	providers := sampleLocalProviders(providerID, fmt.Sprintf("touch %s", sentinelPath))
	cleanupDir(t, "job-1", providerID, "bench-1")

	// Ensure SERVICE_URL is not set
	t.Setenv("SERVICE_URL", "")

	rt := &LocalRuntime{
		logger:    discardLogger(),
		ctx:       testContext(t),
		providers: providers,
	}

	err := rt.RunEvaluationJob(evaluation, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	waitForFile(t, sentinelPath, 5*time.Second)
	jobSpecPath := filepath.Join(localRunDir, dirName, "meta", "job.json")
	data, err := os.ReadFile(jobSpecPath)
	if err != nil {
		t.Fatalf("expected job.json to exist, got %v", err)
	}

	var spec shared.JobSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatalf("expected valid JSON, got %v", err)
	}

	if spec.CallbackURL != nil {
		t.Fatalf("expected callback_url to be nil, got %q", *spec.CallbackURL)
	}
}

func TestRunEvaluationJobCreatesLogFile(t *testing.T) {
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)
	dirName := shared.JobName("job-1", providerID, "bench-1")
	sentinelPath := filepath.Join(localRunDir, dirName, "done")
	providers := sampleLocalProviders(providerID, fmt.Sprintf("echo hello-stdout && echo hello-stderr >&2 && touch %s", sentinelPath))
	cleanupDir(t, "job-1", providerID, "bench-1")

	rt := &LocalRuntime{
		logger:    discardLogger(),
		ctx:       testContext(t),
		providers: providers,
	}

	err := rt.RunEvaluationJob(evaluation, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	waitForFile(t, sentinelPath, 5*time.Second)
	logFilePath := filepath.Join(localRunDir, dirName, "jobrun.log")

	data, err := os.ReadFile(logFilePath)
	if err != nil {
		t.Fatalf("expected jobrun.log to exist, got %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "hello-stdout") {
		t.Fatalf("expected log file to contain stdout output, got %q", content)
	}
	if !strings.Contains(content, "hello-stderr") {
		t.Fatalf("expected log file to contain stderr output, got %q", content)
	}
}

func TestDeleteEvaluationJobResources(t *testing.T) {
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)

	// Pre-create the directory structure
	dirName := shared.JobName("job-1", providerID, "bench-1")
	metaDir := filepath.Join(localRunDir, dirName, "meta")
	if err := os.MkdirAll(metaDir, 0755); err != nil {
		t.Fatalf("failed to create test directory: %v", err)
	}
	jobSpecPath := filepath.Join(metaDir, "job.json")
	if err := os.WriteFile(jobSpecPath, []byte(`{}`), 0644); err != nil {
		t.Fatalf("failed to write test job.json: %v", err)
	}

	rt := &LocalRuntime{
		logger: discardLogger(),
	}

	err := rt.DeleteEvaluationJobResources(evaluation)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify directory was removed
	fullDir := filepath.Join(localRunDir, dirName)
	if _, err := os.Stat(fullDir); !os.IsNotExist(err) {
		os.RemoveAll(fullDir) // Clean up before failing
		t.Fatalf("expected directory %s to be removed", fullDir)
	}
}

func TestDeleteEvaluationJobResourcesNonExistent(t *testing.T) {
	providerID := "provider-nonexistent"
	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{ID: "job-nonexistent"},
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Benchmarks: []api.BenchmarkConfig{
				{
					Ref:        api.Ref{ID: "bench-nonexistent"},
					ProviderID: providerID,
				},
			},
		},
	}

	rt := &LocalRuntime{
		logger: discardLogger(),
	}

	err := rt.DeleteEvaluationJobResources(evaluation)
	if err != nil {
		t.Fatalf("expected no error for non-existent directory, got %v", err)
	}
}
