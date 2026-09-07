package local

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_hub/handlers"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/runtimes/shared"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// fakeStorage implements [abstractions.Storage] for testing.
type fakeStorage struct {
	logger            *slog.Logger
	called            bool
	ctx               context.Context
	runStatus         *api.StatusEvent
	runStatusChan     chan *api.StatusEvent
	updateErr         error
	providerConfigs   map[string]api.ProviderResource
	collectionConfigs map[string]api.CollectionResource
}

func TestReplaceEnvironmentVariable(t *testing.T) {
	env := []string{
		"KEEP=value",
		"TARGET=old",
		"TARGET=duplicate",
		"target=case-variant",
		"TARGET_SUFFIX=unrelated",
	}

	got := replaceEnvironmentVariable(env, "TARGET", "new")
	want := []string{"KEEP=value", "TARGET_SUFFIX=unrelated", "TARGET=new"}
	if runtime.GOOS != "windows" {
		want = []string{"KEEP=value", "target=case-variant", "TARGET_SUFFIX=unrelated", "TARGET=new"}
	}

	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("replaceEnvironmentVariable() = %v, want %v", got, want)
	}
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
func (f *fakeStorage) GetEvaluationJobs(_ *abstractions.QueryFilter) (*abstractions.QueryResults[api.EvaluationJobResource], error) {
	return nil, nil
}
func (f *fakeStorage) DeleteEvaluationJob(_ string) error { return nil }
func (f *fakeStorage) UpdateEvaluationJobStatus(_ string, _ api.OverallState, _ *api.MessageInfo) error {
	f.called = true
	return nil
}
func (f *fakeStorage) UpdateEvaluationJobResolvedSHA(_ string, _ int, _ string) error { return nil }
func (f *fakeStorage) CreateCollection(_ *api.CollectionResource) error               { return nil }
func (f *fakeStorage) GetCollection(id string) (*api.CollectionResource, error) {
	if cr, ok := f.collectionConfigs[id]; ok {
		return &cr, nil
	}
	return nil, fmt.Errorf("collection %q not found", id)
}
func (f *fakeStorage) GetCollections(_ *abstractions.QueryFilter) (*abstractions.QueryResults[api.CollectionResource], error) {
	return nil, nil
}
func (f *fakeStorage) PatchCollection(_ string, _ *api.Patch) (*api.CollectionResource, error) {
	return nil, nil
}
func (f *fakeStorage) UpdateCollection(_ string, _ *api.CollectionConfig) (*api.CollectionResource, error) {
	return nil, nil
}
func (f *fakeStorage) DeleteCollection(_ string) error { return nil }
func (f *fakeStorage) LoadSystemResources(_ map[string]api.CollectionResource, _ map[string]api.ProviderResource) error {
	return nil
}
func (f *fakeStorage) Close() error { return nil }

func (f *fakeStorage) CreateProvider(_ *api.ProviderResource) error { return nil }
func (f *fakeStorage) GetProvider(id string) (*api.ProviderResource, error) {
	if pr, ok := f.providerConfigs[id]; ok {
		return &pr, nil
	}
	return nil, fmt.Errorf("provider %q not found", id)
}
func (f *fakeStorage) DeleteProvider(_ string) error { return nil }
func (f *fakeStorage) GetProviders(_ *abstractions.QueryFilter) (*abstractions.QueryResults[api.ProviderResource], error) {
	return nil, nil
}
func (f *fakeStorage) UpdateProvider(_ string, _ *api.ProviderConfig) (*api.ProviderResource, error) {
	return nil, nil
}
func (f *fakeStorage) PatchProvider(_ string, _ *api.Patch) (*api.ProviderResource, error) {
	return nil, nil
}

func (f *fakeStorage) WithLogger(logger *slog.Logger) abstractions.Storage {
	return &fakeStorage{
		logger:        logger,
		ctx:           f.ctx,
		runStatusChan: f.runStatusChan,
		updateErr:     f.updateErr,
	}
}

func (f *fakeStorage) WithTenant(_ api.Tenant) abstractions.Storage {
	return f
}

func (f *fakeStorage) WithContext(ctx context.Context) abstractions.Storage {
	return &fakeStorage{
		logger:        f.logger,
		ctx:           ctx,
		runStatusChan: f.runStatusChan,
		updateErr:     f.updateErr,
	}
}

func (f *fakeStorage) WithOwner(owner api.User) abstractions.Storage {
	return f
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTracker() jobTracker {
	return &pidTracker{pids: make(map[string][]int), cancelled: make(map[string]bool)}
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
			Model: &api.ModelRef{
				URL:  "http://model.example",
				Name: "model-1",
			},
			Benchmarks: []api.EvaluationBenchmarkConfig{
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
			Resource: api.Resource{ID: providerID},
			ProviderConfig: api.ProviderConfig{
				Runtime: &api.Runtime{
					Local: &api.LocalRuntime{
						Command: command,
						Env: []api.EnvVar{
							{Name: "TEST_VAR", Value: "test_value"},
						},
					},
				},
			},
		},
	}
}

func localJobDir(jobID string, benchmarkIndex int, providerID, benchmarkID string) string {
	return filepath.Join(localJobsBaseDir, jobID, fmt.Sprintf("%d", benchmarkIndex), providerID, benchmarkID)
}

func cleanupDir(t *testing.T, jobID string) {
	t.Helper()
	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Join(localJobsBaseDir, jobID))
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
	rt := &LocalRuntime{tracker: newTracker()}
	if rt.Name() != "local" {
		t.Fatalf("expected Name() to return %q, got %q", "local", rt.Name())
	}
}

func TestLocalRuntimeValidateHardwareProfiles(t *testing.T) {
	rt := &LocalRuntime{tracker: newTracker()}
	err := rt.ValidateHardwareProfiles([]api.EvaluationBenchmarkConfig{{
		Ref:        api.Ref{ID: "bench-1"},
		ProviderID: "provider-1",
		HardwareConfig: &api.BenchmarkHardwareConfig{
			HardwareProfileName: "any-profile",
		},
	}})
	if err != nil {
		t.Fatalf("local runtime should skip hardware profile validation, got %v", err)
	}
}

func TestBuildCallbackURL(t *testing.T) {
	tests := []struct {
		name   string
		config *config.Config
		want   *string
	}{
		{name: "nil config", config: nil, want: nil},
		{name: "nil service", config: &config.Config{}, want: nil},
		{name: "zero port", config: &config.Config{Service: &config.ServiceConfig{Port: 0}}, want: nil},
		{name: "host and port", config: &config.Config{Service: &config.ServiceConfig{Host: "myhost", Port: 9090}}, want: strPtr("http://myhost:9090")},
		{name: "empty host defaults to localhost", config: &config.Config{Service: &config.ServiceConfig{Port: 8080}}, want: strPtr("http://localhost:8080")},
		{name: "tls enabled", config: &config.Config{Service: &config.ServiceConfig{Host: "myhost", Port: 443, TLSCertFile: "/cert", TLSKeyFile: "/key"}}, want: strPtr("https://myhost:443")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildCallbackURL(tc.config)
			if tc.want == nil {
				if got != nil {
					t.Fatalf("expected nil, got %q", *got)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected %q, got nil", *tc.want)
			}
			if *got != *tc.want {
				t.Fatalf("expected %q, got %q", *tc.want, *got)
			}
		})
	}
}

func strPtr(s string) *string { return &s }

func TestNewLocalRuntime(t *testing.T) {
	rt, err := NewLocalRuntime(discardLogger(), nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if rt == nil {
		t.Fatal("expected non-nil runtime")
	}
}

func TestNewLocalRuntimeMLFlowTrackingURI(t *testing.T) {
	tests := []struct {
		name   string
		config *config.Config
		want   string
	}{
		{name: "nil config", config: nil},
		{name: "nil mlflow config", config: &config.Config{}},
		{
			name: "blank tracking URI",
			config: &config.Config{MLFlow: &config.MLFlowConfig{
				TrackingURI: " \t\n",
			}},
		},
		{
			name: "trimmed tracking URI",
			config: &config.Config{MLFlow: &config.MLFlowConfig{
				TrackingURI: "  http://mlflow.example:5000/  ",
			}},
			want: "http://mlflow.example:5000/",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rt, err := NewLocalRuntime(discardLogger(), tc.config)
			if err != nil {
				t.Fatalf("NewLocalRuntime failed: %v", err)
			}
			localRuntime, ok := rt.(*LocalRuntime)
			if !ok {
				t.Fatalf("runtime type = %T, want *LocalRuntime", rt)
			}
			if localRuntime.mlflowTrackingURI != tc.want {
				t.Fatalf("mlflowTrackingURI = %q, want %q", localRuntime.mlflowTrackingURI, tc.want)
			}
		})
	}
}

func TestLocalRuntimeMLFlowTrackingURISurvivesCloning(t *testing.T) {
	rt, err := NewLocalRuntime(discardLogger(), &config.Config{
		MLFlow: &config.MLFlowConfig{TrackingURI: "  http://mlflow.example:5000  "},
	})
	if err != nil {
		t.Fatalf("NewLocalRuntime failed: %v", err)
	}
	base := rt.(*LocalRuntime)

	loggerClone, ok := base.WithLogger(discardLogger()).(*LocalRuntime)
	if !ok {
		t.Fatal("WithLogger did not return *LocalRuntime")
	}
	if loggerClone.mlflowTrackingURI != "http://mlflow.example:5000" {
		t.Fatalf("WithLogger lost mlflowTrackingURI: %q", loggerClone.mlflowTrackingURI)
	}

	contextClone, ok := base.WithContext(context.Background()).(*LocalRuntime)
	if !ok {
		t.Fatal("WithContext did not return *LocalRuntime")
	}
	if contextClone.mlflowTrackingURI != "http://mlflow.example:5000" {
		t.Fatalf("WithContext lost mlflowTrackingURI: %q", contextClone.mlflowTrackingURI)
	}
}

func TestRunEvaluationJobMLFlowTrackingURIEnvironment(t *testing.T) {
	tests := []struct {
		name          string
		serviceConfig *config.Config
		inheritedURI  string
		providerURI   string
		wantURI       string
	}{
		{
			name:          "configured URI overrides inherited URI",
			serviceConfig: &config.Config{MLFlow: &config.MLFlowConfig{TrackingURI: "  http://configured:5000  "}},
			inheritedURI:  "http://inherited:5000",
			wantURI:       "http://configured:5000",
		},
		{
			name:          "nil MLflow configuration does not inject a URI",
			serviceConfig: &config.Config{},
			wantURI:       "",
		},
		{
			name:          "blank MLflow configuration does not inject a URI",
			serviceConfig: &config.Config{MLFlow: &config.MLFlowConfig{TrackingURI: " \t\n"}},
			wantURI:       "",
		},
		{
			name:          "inherited URI is preserved without service or provider value",
			serviceConfig: &config.Config{},
			inheritedURI:  "http://inherited:5000",
			wantURI:       "http://inherited:5000",
		},
		{
			name:          "provider URI overrides configured URI",
			serviceConfig: &config.Config{MLFlow: &config.MLFlowConfig{TrackingURI: "http://configured:5000"}},
			inheritedURI:  "http://inherited:5000",
			providerURI:   "http://provider:5000",
			wantURI:       "http://provider:5000",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			const providerID = "provider-1"
			t.Setenv(mlflowTrackingURIEnv, tc.inheritedURI)
			evaluation := sampleEvaluation(providerID)
			cleanupDir(t, "job-1")

			dirName := localJobDir("job-1", 0, providerID, "bench-1")
			outputFile := filepath.Join(dirName, "mlflow_env.txt")
			sentinelPath := filepath.Join(dirName, "done")
			command := fmt.Sprintf("env | grep '^%s=' > %s && touch %s", mlflowTrackingURIEnv, outputFile, sentinelPath)
			providers := sampleLocalProviders(providerID, command)
			if tc.providerURI != "" {
				provider := providers[providerID]
				provider.Runtime.Local.Env = append(provider.Runtime.Local.Env, api.EnvVar{
					Name:  mlflowTrackingURIEnv,
					Value: tc.providerURI,
				})
				providers[providerID] = provider
			}

			rt, err := NewLocalRuntime(discardLogger(), tc.serviceConfig)
			if err != nil {
				t.Fatalf("NewLocalRuntime failed: %v", err)
			}
			rt = rt.WithContext(testContext(t))
			storage := &fakeStorage{providerConfigs: providers}
			benchmarks, err := handlers.GetJobBenchmarks(evaluation, nil)
			if err != nil {
				t.Fatalf("failed to resolve benchmarks: %v", err)
			}
			if err := rt.RunEvaluationJob(evaluation, benchmarks, storage); err != nil {
				t.Fatalf("RunEvaluationJob failed: %v", err)
			}

			waitForFile(t, sentinelPath, 5*time.Second)
			data, err := os.ReadFile(outputFile)
			if err != nil {
				t.Fatalf("failed to read environment output: %v", err)
			}
			lines := strings.Split(strings.TrimSpace(string(data)), "\n")
			if len(lines) != 1 {
				t.Fatalf("got %d MLFLOW_TRACKING_URI entries, want 1: %q", len(lines), string(data))
			}
			if lines[0] != mlflowTrackingURIEnv+"="+tc.wantURI {
				t.Fatalf("MLFLOW_TRACKING_URI = %q, want %q", lines[0], mlflowTrackingURIEnv+"="+tc.wantURI)
			}
		})
	}
}

func TestRunEvaluationJobWritesJobSpec(t *testing.T) {
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)
	dirName := localJobDir("job-1", 0, providerID, "bench-1")
	sentinelPath := filepath.Join(dirName, "done")
	providers := sampleLocalProviders(providerID, fmt.Sprintf("touch %s", sentinelPath))
	cleanupDir(t, "job-1")

	rt := &LocalRuntime{
		logger:  discardLogger(),
		ctx:     testContext(t),
		tracker: newTracker(),
	}

	storage := &fakeStorage{providerConfigs: providers}

	benchmarks, err := handlers.GetJobBenchmarks(evaluation, nil)
	if err != nil {
		t.Fatalf("RunEvaluationJob failed to resolve benchmarks: %v", err)
	}

	err = rt.RunEvaluationJob(evaluation, benchmarks, storage)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	waitForFile(t, sentinelPath, 5*time.Second)
	metaDir := filepath.Join(dirName, "meta")

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
	cleanupDir(t, "job-1")
	t.Setenv(evalHubJobSpecPathEnv, "inherited-job-spec-path")
	t.Setenv(evalHubModeEnv, "inherited-mode")
	t.Setenv("TEST_VAR", "inherited-test-value")

	dirName := localJobDir("job-1", 0, providerID, "bench-1")
	outputFile := filepath.Join(dirName, "env_output.txt")
	sentinelPath := filepath.Join(dirName, "done")

	// Command writes EVALHUB_JOB_SPEC_PATH, TEST_VAR, and EVALHUB_MODE to output file
	command := fmt.Sprintf("sh -c 'echo $EVALHUB_JOB_SPEC_PATH > %s && echo $TEST_VAR >> %s && echo $EVALHUB_MODE >> %s && touch %s'", outputFile, outputFile, outputFile, sentinelPath)
	providers := sampleLocalProviders(providerID, command)

	rt := &LocalRuntime{
		logger:  discardLogger(),
		ctx:     testContext(t),
		tracker: newTracker(),
	}

	storage := &fakeStorage{providerConfigs: providers}

	benchmarks, err := handlers.GetJobBenchmarks(evaluation, nil)
	if err != nil {
		t.Fatalf("RunEvaluationJob failed to resolve benchmarks: %v", err)
	}

	err = rt.RunEvaluationJob(evaluation, benchmarks, storage)
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
	expectedPath := filepath.Join(dirName, "meta", "job.json")
	absExpectedPath, _ := filepath.Abs(expectedPath)
	if len(output) == 0 {
		t.Fatal("expected env output, got empty file")
	}

	// Parse the three lines
	lines := strings.Split(output, "\n")
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines in env output, got %d: %q", len(lines), output)
	}
	if lines[0] != absExpectedPath {
		t.Fatalf("expected EVALHUB_JOB_SPEC_PATH=%q, got %q", absExpectedPath, lines[0])
	}
	if lines[1] != "test_value" {
		t.Fatalf("expected TEST_VAR=%q, got %q", "test_value", lines[1])
	}
	if lines[2] != "local" {
		t.Fatalf("EVALHUB_MODE = %q, want local", lines[2])
	}
}

func TestRunEvaluationJobNoBenchmarks(t *testing.T) {
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)
	evaluation.Benchmarks = nil

	rt := &LocalRuntime{
		logger:  discardLogger(),
		ctx:     context.Background(),
		tracker: newTracker(),
	}

	storage := &fakeStorage{providerConfigs: sampleLocalProviders(providerID, "true")}

	err := rt.RunEvaluationJob(evaluation, nil, storage)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "does not have any benchmarks") {
		t.Fatalf("expected error to contain %q, got %q", "does not have any benchmarks", err.Error())
	}
}

func TestRunEvaluationJobProviderNotFound(t *testing.T) {
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)

	tctx := testContext(t)
	logger := discardLogger()
	statusCh := make(chan *api.StatusEvent, 1)
	storage := &fakeStorage{logger: logger, ctx: tctx, runStatusChan: statusCh}
	var store abstractions.Storage = storage

	// Use empty providers map so provider is not found
	rt := &LocalRuntime{
		logger:  logger,
		ctx:     tctx,
		tracker: newTracker(),
	}

	benchmarks, err := handlers.GetJobBenchmarks(evaluation, nil)
	if err != nil {
		t.Fatalf("RunEvaluationJob failed to resolve benchmarks: %v", err)
	}

	err = rt.RunEvaluationJob(evaluation, benchmarks, store)
	if err != nil {
		t.Fatalf("expected no synchronous error, got %v", err)
	}

	select {
	case runStatus := <-statusCh:
		if runStatus == nil {
			t.Fatal("expected run status, got nil")
		}
		if runStatus.BenchmarkStatusEvent.Status != api.StateFailed {
			t.Fatalf("expected status %q, got %q", api.StateFailed, runStatus.BenchmarkStatusEvent.Status)
		}
		if !strings.Contains(runStatus.BenchmarkStatusEvent.ErrorMessage.Message, "not found") {
			t.Fatalf("expected error message to contain %q, got %q", "not found", runStatus.BenchmarkStatusEvent.ErrorMessage.Message)
		}
		if runStatus.BenchmarkStatusEvent.ErrorMessage.MessageOrigin != api.MessageOriginServer {
			t.Fatalf("expected server error origin, got %q", runStatus.BenchmarkStatusEvent.ErrorMessage.MessageOrigin)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for failed benchmark status update")
	}
}

func TestRunEvaluationJobMissingLocalCommand(t *testing.T) {
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)

	tctx := testContext(t)
	logger := discardLogger()
	statusCh := make(chan *api.StatusEvent, 1)

	// Provider with nil Local runtime
	providers := map[string]api.ProviderResource{
		providerID: {
			Resource: api.Resource{ID: providerID},
			ProviderConfig: api.ProviderConfig{
				Runtime: &api.Runtime{Local: nil},
			},
		},
	}

	storage := &fakeStorage{logger: logger, ctx: tctx, runStatusChan: statusCh, providerConfigs: providers}

	rt := &LocalRuntime{
		logger:  logger,
		ctx:     tctx,
		tracker: newTracker(),
	}

	benchmarks, err := handlers.GetJobBenchmarks(evaluation, nil)
	if err != nil {
		t.Fatalf("RunEvaluationJob failed to resolve benchmarks: %v", err)
	}

	err = rt.RunEvaluationJob(evaluation, benchmarks, storage)
	if err != nil {
		t.Fatalf("expected no synchronous error, got %v", err)
	}

	select {
	case runStatus := <-statusCh:
		if runStatus == nil {
			t.Fatal("expected run status, got nil")
		}
		if runStatus.BenchmarkStatusEvent.Status != api.StateFailed {
			t.Fatalf("expected status %q, got %q", api.StateFailed, runStatus.BenchmarkStatusEvent.Status)
		}
		if !strings.Contains(runStatus.BenchmarkStatusEvent.ErrorMessage.Message, "Local runtime is not enabled") {
			t.Fatalf("expected error message to contain %q, got %q", "Local runtime is not enabled", runStatus.BenchmarkStatusEvent.ErrorMessage.Message)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for failed benchmark status update")
	}

	// Also test with empty command
	statusCh2 := make(chan *api.StatusEvent, 1)

	providers[providerID] = api.ProviderResource{
		Resource: api.Resource{ID: providerID},
		ProviderConfig: api.ProviderConfig{
			Runtime: &api.Runtime{
				Local: &api.LocalRuntime{Command: ""},
			},
		},
	}

	storage2 := &fakeStorage{logger: logger, ctx: tctx, runStatusChan: statusCh2, providerConfigs: providers}

	benchmarks, err = handlers.GetJobBenchmarks(evaluation, nil)
	if err != nil {
		t.Fatalf("RunEvaluationJob failed to resolve benchmarks: %v", err)
	}

	err = rt.RunEvaluationJob(evaluation, benchmarks, storage2)
	if err != nil {
		t.Fatalf("expected no synchronous error for empty command, got %v", err)
	}

	select {
	case runStatus := <-statusCh2:
		if runStatus == nil {
			t.Fatal("expected run status, got nil")
		}
		if !strings.Contains(runStatus.BenchmarkStatusEvent.ErrorMessage.Message, "Local runtime is not enabled") {
			t.Fatalf("expected error message to contain %q, got %q", "Local runtime is not enabled", runStatus.BenchmarkStatusEvent.ErrorMessage.Message)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for failed benchmark status update (empty command)")
	}
}

// TestRunEvaluationJobProcessFailureNoCallback verifies that when a subprocess
// exits with a non-zero status, the server does not call failBenchmark. The
// subprocess is responsible for reporting its own status via the callback URL;
// the server only reaps the child process to prevent zombies.
func TestRunEvaluationJobProcessFailureNoCallback(t *testing.T) {
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)
	cleanupDir(t, "job-1")

	providers := sampleLocalProviders(providerID, "exit 1")

	tctx := testContext(t)
	logger := discardLogger()
	statusCh := make(chan *api.StatusEvent, 1)

	rt := &LocalRuntime{
		logger:  logger,
		ctx:     tctx,
		tracker: newTracker(),
	}

	storage := &fakeStorage{logger: logger, ctx: tctx, runStatusChan: statusCh, providerConfigs: providers}
	var store abstractions.Storage = storage

	benchmarks, err := handlers.GetJobBenchmarks(evaluation, nil)
	if err != nil {
		t.Fatalf("RunEvaluationJob failed to resolve benchmarks: %v", err)
	}

	err = rt.RunEvaluationJob(evaluation, benchmarks, store)
	if err != nil {
		t.Fatalf("expected no synchronous error, got %v", err)
	}

	// The server should NOT report a failed status for a subprocess exit;
	// the subprocess itself is responsible for status reporting.
	select {
	case runStatus := <-statusCh:
		t.Fatalf("expected no status update for process failure, got %+v", runStatus)
	case <-time.After(1 * time.Second):
		// Expected: no status update
	}
}

func TestRunEvaluationJobCancelledNoFailure(t *testing.T) {
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)
	cleanupDir(t, "job-1")

	dirName := localJobDir("job-1", 0, providerID, "bench-1")
	sentinelPath := filepath.Join(dirName, "started")

	// Command signals readiness then sleeps
	command := fmt.Sprintf("touch %s && sleep 60", sentinelPath)
	providers := sampleLocalProviders(providerID, command)

	tctx := testContext(t)
	logger := discardLogger()
	statusCh := make(chan *api.StatusEvent, 1)

	rt := &LocalRuntime{
		logger:  logger,
		ctx:     tctx,
		tracker: newTracker(),
	}

	storage := &fakeStorage{logger: logger, ctx: tctx, runStatusChan: statusCh, providerConfigs: providers}
	var store abstractions.Storage = storage

	benchmarks, err := handlers.GetJobBenchmarks(evaluation, nil)
	if err != nil {
		t.Fatalf("RunEvaluationJob failed to resolve benchmarks: %v", err)
	}

	err = rt.RunEvaluationJob(evaluation, benchmarks, store)
	if err != nil {
		t.Fatalf("expected no synchronous error, got %v", err)
	}

	// Wait for the process to start
	waitForFile(t, sentinelPath, 5*time.Second)

	// Cancel the job
	if err := rt.DeleteEvaluationJobResources(evaluation); err != nil {
		t.Fatalf("expected no error on cancel, got %v", err)
	}

	// Storage should NOT receive a failed status
	select {
	case runStatus := <-statusCh:
		t.Fatalf("expected no status update after cancellation, got %+v", runStatus)
	case <-time.After(500 * time.Millisecond):
		// Expected: no status update
	}
}

func TestRunEvaluationJobMultipleBenchmarks(t *testing.T) {
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)
	// Add a second benchmark
	evaluation.Benchmarks = append(evaluation.Benchmarks, api.EvaluationBenchmarkConfig{
		Ref:        api.Ref{ID: "bench-2"},
		ProviderID: providerID,
		Parameters: map[string]any{"baz": "qux"},
	})

	dir1 := localJobDir("job-1", 0, providerID, "bench-1")
	dir2 := localJobDir("job-1", 1, providerID, "bench-2")
	sentinel1 := filepath.Join(dir1, "done")
	sentinel2 := filepath.Join(dir2, "done")

	// The command creates a sentinel in the benchmark's own directory via EVALHUB_JOB_SPEC_PATH.
	// Since each benchmark gets its own spec path, use dirname to derive the job dir.
	command := "touch $(dirname $(dirname $EVALHUB_JOB_SPEC_PATH))/done"
	providers := sampleLocalProviders(providerID, command)
	cleanupDir(t, "job-1")

	tctx := testContext(t)
	logger := discardLogger()

	rt := &LocalRuntime{
		logger:  logger,
		ctx:     tctx,
		tracker: newTracker(),
	}

	storage := &fakeStorage{logger: logger, ctx: tctx, providerConfigs: providers}

	benchmarks, err := handlers.GetJobBenchmarks(evaluation, nil)
	if err != nil {
		t.Fatalf("RunEvaluationJob failed to resolve benchmarks: %v", err)
	}

	err = rt.RunEvaluationJob(evaluation, benchmarks, storage)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	waitForFile(t, sentinel1, 5*time.Second)
	waitForFile(t, sentinel2, 5*time.Second)

	// Both benchmark directories should exist
	if _, err := os.Stat(dir1); os.IsNotExist(err) {
		t.Fatal("expected directory for first benchmark to exist")
	}
	if _, err := os.Stat(dir2); os.IsNotExist(err) {
		t.Fatal("expected directory for second benchmark to exist")
	}

	// Verify each has its own job spec
	spec1Path := filepath.Join(dir1, "meta", "job.json")
	spec2Path := filepath.Join(dir2, "meta", "job.json")

	data1, err := os.ReadFile(spec1Path)
	if err != nil {
		t.Fatalf("expected job.json for bench-1, got %v", err)
	}
	data2, err := os.ReadFile(spec2Path)
	if err != nil {
		t.Fatalf("expected job.json for bench-2, got %v", err)
	}

	var s1, s2 shared.JobSpec
	if err := json.Unmarshal(data1, &s1); err != nil {
		t.Fatalf("invalid JSON for bench-1: %v", err)
	}
	if err := json.Unmarshal(data2, &s2); err != nil {
		t.Fatalf("invalid JSON for bench-2: %v", err)
	}
	if s1.BenchmarkID != "bench-1" {
		t.Fatalf("expected bench-1 spec BenchmarkID=%q, got %q", "bench-1", s1.BenchmarkID)
	}
	if s2.BenchmarkID != "bench-2" {
		t.Fatalf("expected bench-2 spec BenchmarkID=%q, got %q", "bench-2", s2.BenchmarkID)
	}
}

func TestRunEvaluationJobMultipleBenchmarksPartialFailure(t *testing.T) {
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)
	// Add a second benchmark with a non-existent provider
	evaluation.Benchmarks = append(evaluation.Benchmarks, api.EvaluationBenchmarkConfig{
		Ref:        api.Ref{ID: "bench-2"},
		ProviderID: "no-such-provider",
	})

	dir1 := localJobDir("job-1", 0, providerID, "bench-1")
	sentinel1 := filepath.Join(dir1, "done")
	providers := sampleLocalProviders(providerID, fmt.Sprintf("touch %s", sentinel1))
	cleanupDir(t, "job-1")

	tctx := testContext(t)
	logger := discardLogger()
	statusCh := make(chan *api.StatusEvent, 2)
	storage := &fakeStorage{logger: logger, ctx: tctx, runStatusChan: statusCh, providerConfigs: providers}
	var store abstractions.Storage = storage

	rt := &LocalRuntime{
		logger:  logger,
		ctx:     tctx,
		tracker: newTracker(),
	}

	benchmarks, err := handlers.GetJobBenchmarks(evaluation, nil)
	if err != nil {
		t.Fatalf("RunEvaluationJob failed to resolve benchmarks: %v", err)
	}

	err = rt.RunEvaluationJob(evaluation, benchmarks, store)
	if err != nil {
		t.Fatalf("expected no synchronous error, got %v", err)
	}

	// First benchmark should still have run successfully
	waitForFile(t, sentinel1, 5*time.Second)

	// Storage should have been called for the failed benchmark
	select {
	case runStatus := <-statusCh:
		if runStatus.BenchmarkStatusEvent.ID != "bench-2" {
			t.Fatalf("expected failed benchmark ID %q, got %q", "bench-2", runStatus.BenchmarkStatusEvent.ID)
		}
		if runStatus.BenchmarkStatusEvent.Status != api.StateFailed {
			t.Fatalf("expected status %q, got %q", api.StateFailed, runStatus.BenchmarkStatusEvent.Status)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for failed benchmark status update")
	}
}

func TestRunEvaluationJobCallbackURL(t *testing.T) {
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)
	dirName := localJobDir("job-1", 0, providerID, "bench-1")
	sentinelPath := filepath.Join(dirName, "done")
	providers := sampleLocalProviders(providerID, fmt.Sprintf("touch %s", sentinelPath))
	cleanupDir(t, "job-1")

	tctx := testContext(t)
	logger := discardLogger()

	callbackURL := "http://localhost:8080"
	rt := &LocalRuntime{
		logger:      logger,
		ctx:         tctx,
		tracker:     newTracker(),
		callbackURL: &callbackURL,
	}

	storage := &fakeStorage{logger: logger, ctx: tctx, providerConfigs: providers}

	benchmarks, err := handlers.GetJobBenchmarks(evaluation, nil)
	if err != nil {
		t.Fatalf("RunEvaluationJob failed to resolve benchmarks: %v", err)
	}

	err = rt.RunEvaluationJob(evaluation, benchmarks, storage)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	waitForFile(t, sentinelPath, 5*time.Second)
	jobSpecPath := filepath.Join(dirName, "meta", "job.json")
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
	dirName := localJobDir("job-1", 0, providerID, "bench-1")
	sentinelPath := filepath.Join(dirName, "done")
	providers := sampleLocalProviders(providerID, fmt.Sprintf("touch %s", sentinelPath))
	cleanupDir(t, "job-1")

	tctx := testContext(t)
	logger := discardLogger()

	rt := &LocalRuntime{
		logger:  logger,
		ctx:     tctx,
		tracker: newTracker(),
	}

	storage := &fakeStorage{logger: logger, ctx: tctx, providerConfigs: providers}

	benchmarks, err := handlers.GetJobBenchmarks(evaluation, nil)
	if err != nil {
		t.Fatalf("RunEvaluationJob failed to resolve benchmarks: %v", err)
	}

	err = rt.RunEvaluationJob(evaluation, benchmarks, storage)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	waitForFile(t, sentinelPath, 5*time.Second)
	jobSpecPath := filepath.Join(dirName, "meta", "job.json")
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
	dirName := localJobDir("job-1", 0, providerID, "bench-1")
	sentinelPath := filepath.Join(dirName, "done")
	providers := sampleLocalProviders(providerID, fmt.Sprintf("echo hello-stdout && echo hello-stderr >&2 && touch %s", sentinelPath))
	cleanupDir(t, "job-1")

	tctx := testContext(t)
	logger := discardLogger()

	rt := &LocalRuntime{
		logger:  logger,
		ctx:     tctx,
		tracker: newTracker(),
	}

	storage := &fakeStorage{logger: logger, ctx: tctx, providerConfigs: providers}

	benchmarks, err := handlers.GetJobBenchmarks(evaluation, nil)
	if err != nil {
		t.Fatalf("RunEvaluationJob failed to resolve benchmarks: %v", err)
	}

	err = rt.RunEvaluationJob(evaluation, benchmarks, storage)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	waitForFile(t, sentinelPath, 5*time.Second)
	logFilePath := filepath.Join(dirName, "jobrun.log")

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

// runSidecarEvalJob sets up a local runtime with sidecar enabled, runs an evaluation
// job with model auth, and returns the benchmark output directory path.
func runSidecarEvalJob(t *testing.T) string {
	t.Helper()
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)
	evaluation.Model.Auth = &api.ModelAuth{SecretRef: "file:///home/user1/model-auth"}
	dirName := localJobDir("job-1", 0, providerID, "bench-1")
	sentinelPath := filepath.Join(dirName, "done")
	providers := sampleLocalProviders(providerID, fmt.Sprintf("touch %s", sentinelPath))
	cleanupDir(t, "job-1")

	cfg := &config.Config{
		Service: &config.ServiceConfig{Port: 8080},
		Sidecar: &config.SidecarConfig{LocalMode: true, BaseURL: "http://localhost:8082"},
	}
	rt, _ := NewLocalRuntime(discardLogger(), cfg)
	rt = rt.WithContext(testContext(t))

	storage := &fakeStorage{providerConfigs: providers}

	benchmarks, err := handlers.GetJobBenchmarks(evaluation, nil)
	if err != nil {
		t.Fatalf("failed to resolve benchmarks: %v", err)
	}

	err = rt.RunEvaluationJob(evaluation, benchmarks, storage)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	waitForFile(t, sentinelPath, 5*time.Second)
	return dirName
}

func TestRunEvaluationJobWithSidecarWritesSidecarJobInfo(t *testing.T) {
	runSidecarEvalJob(t)

	sidecarInfoPath := filepath.Join(localJobsBaseDir, "job-1", shared.SidecarJobInfoFileName)
	data, err := os.ReadFile(sidecarInfoPath)
	if err != nil {
		t.Fatalf("expected sidecar-job-info.json to exist, got %v", err)
	}

	var info shared.SidecarJobInfo
	if err := json.Unmarshal(data, &info); err != nil {
		t.Fatalf("expected valid JSON, got %v", err)
	}

	if info.Model == nil {
		t.Fatal("expected model config, got nil")
	}
	if info.Model.URL != "http://model.example" {
		t.Fatalf("expected model URL %q, got %q", "http://model.example", info.Model.URL)
	}
	if info.Model.AuthSecretMountPath != "file:///home/user1/model-auth" {
		t.Fatalf("expected auth path %q, got %q", "file:///home/user1/model-auth", info.Model.AuthSecretMountPath)
	}
	if info.Model.HTTPTimeout != shared.DefaultModelHTTPTimeout {
		t.Fatalf("expected timeout %v, got %v", shared.DefaultModelHTTPTimeout, info.Model.HTTPTimeout)
	}
}

func TestRunEvaluationJobWithSidecarRewritesJobSpec(t *testing.T) {
	dirName := runSidecarEvalJob(t)

	jobSpecPath := filepath.Join(dirName, "meta", "job.json")
	data, err := os.ReadFile(jobSpecPath)
	if err != nil {
		t.Fatalf("expected job.json to exist, got %v", err)
	}

	var spec shared.JobSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatalf("expected valid JSON, got %v", err)
	}

	expectedModelURL := "http://localhost:8082/model/job-1"
	if spec.Model.URL != expectedModelURL {
		t.Fatalf("expected model URL %q, got %q", expectedModelURL, spec.Model.URL)
	}

	sidecarURL := "http://localhost:8082"
	if spec.CallbackURL == nil {
		t.Fatal("expected callback_url to be set, got nil")
	}
	if *spec.CallbackURL != sidecarURL {
		t.Fatalf("expected callback_url %q, got %q", sidecarURL, *spec.CallbackURL)
	}

	if spec.Model.Auth == nil {
		t.Fatal("expected model auth to be set, got nil")
	}
	if spec.Model.Auth.SecretRef != "file:///home/user1/model-auth" {
		t.Fatalf("expected auth secret_ref %q, got %q", "file:///home/user1/model-auth", spec.Model.Auth.SecretRef)
	}
}

func TestRunEvaluationJobWithSidecarModelDefaults(t *testing.T) {
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)
	evaluation.Model.Auth = &api.ModelAuth{SecretRef: "file:///home/user1/model-auth"}
	dirName := localJobDir("job-1", 0, providerID, "bench-1")
	sentinelPath := filepath.Join(dirName, "done")
	providers := sampleLocalProviders(providerID, fmt.Sprintf("touch %s", sentinelPath))
	cleanupDir(t, "job-1")

	customTimeout := 120 * time.Second
	cfg := &config.Config{
		Service: &config.ServiceConfig{Port: 8080},
		Sidecar: &config.SidecarConfig{
			LocalMode: true,
			BaseURL:   "http://localhost:8082",
			Model:     &config.SidecarModelConfig{HTTPTimeout: customTimeout},
		},
	}
	rt, err := NewLocalRuntime(discardLogger(), cfg)
	if err != nil {
		t.Fatalf("NewLocalRuntime failed: %v", err)
	}
	rt = rt.WithContext(testContext(t))

	storage := &fakeStorage{providerConfigs: providers}
	benchmarks, err := handlers.GetJobBenchmarks(evaluation, nil)
	if err != nil {
		t.Fatalf("failed to resolve benchmarks: %v", err)
	}

	err = rt.RunEvaluationJob(evaluation, benchmarks, storage)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	waitForFile(t, sentinelPath, 5*time.Second)

	sidecarInfoPath := filepath.Join(localJobsBaseDir, "job-1", shared.SidecarJobInfoFileName)
	data, err := os.ReadFile(sidecarInfoPath)
	if err != nil {
		t.Fatalf("expected sidecar-job-info.json to exist, got %v", err)
	}

	var info shared.SidecarJobInfo
	if err := json.Unmarshal(data, &info); err != nil {
		t.Fatalf("expected valid JSON, got %v", err)
	}

	if info.Model == nil {
		t.Fatal("expected model config, got nil")
	}
	if info.Model.HTTPTimeout != customTimeout {
		t.Fatalf("expected timeout %v, got %v", customTimeout, info.Model.HTTPTimeout)
	}
}

func TestRunEvaluationJobSidecarRewriteError(t *testing.T) {
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)
	cleanupDir(t, "job-1")

	tctx := testContext(t)
	logger := discardLogger()
	statusCh := make(chan *api.StatusEvent, 1)
	providers := sampleLocalProviders(providerID, "true")

	rt := &LocalRuntime{
		logger:         logger,
		ctx:            tctx,
		tracker:        newTracker(),
		sidecarBaseURL: "/no-host",
	}

	storage := &fakeStorage{logger: logger, ctx: tctx, runStatusChan: statusCh, providerConfigs: providers}

	benchmarks, err := handlers.GetJobBenchmarks(evaluation, nil)
	if err != nil {
		t.Fatalf("failed to resolve benchmarks: %v", err)
	}

	err = rt.RunEvaluationJob(evaluation, benchmarks, storage)
	if err != nil {
		t.Fatalf("expected no synchronous error, got %v", err)
	}

	select {
	case runStatus := <-statusCh:
		if runStatus == nil {
			t.Fatal("expected run status, got nil")
		}
		if runStatus.BenchmarkStatusEvent.Status != api.StateFailed {
			t.Fatalf("expected status %q, got %q", api.StateFailed, runStatus.BenchmarkStatusEvent.Status)
		}
		if !strings.Contains(runStatus.BenchmarkStatusEvent.ErrorMessage.Message, "rewrite model URL for sidecar") {
			t.Fatalf("expected error about sidecar rewrite, got %q", runStatus.BenchmarkStatusEvent.ErrorMessage.Message)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for failed benchmark status update")
	}
}

func TestRunEvaluationJobSidecarRejectsInvalidSecretRef(t *testing.T) {
	tests := []struct {
		name      string
		secretRef string
	}{
		{"raw path", "/home/user1/model-auth"},
		{"host without path", "file://path"},
		{"host with path", "file://host/path"},
		{"single slash", "file:/tmp/key"},
		{"opaque relative", "file:tmp/key"},
		{"empty path", "file://"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			providerID := "provider-1"
			evaluation := sampleEvaluation(providerID)
			evaluation.Model.Auth = &api.ModelAuth{SecretRef: tc.secretRef}

			providers := sampleLocalProviders(providerID, "true")
			cleanupDir(t, "job-1")

			cfg := &config.Config{
				Service: &config.ServiceConfig{Port: 8080},
				Sidecar: &config.SidecarConfig{LocalMode: true, BaseURL: "http://localhost:8082"},
			}
			rt, err := NewLocalRuntime(discardLogger(), cfg)
			if err != nil {
				t.Fatalf("NewLocalRuntime failed: %v", err)
			}
			rt = rt.WithContext(testContext(t))

			storage := &fakeStorage{providerConfigs: providers}

			benchmarks, err := handlers.GetJobBenchmarks(evaluation, nil)
			if err != nil {
				t.Fatalf("failed to resolve benchmarks: %v", err)
			}

			err = rt.RunEvaluationJob(evaluation, benchmarks, storage)
			if err == nil {
				t.Fatalf("expected error for secret_ref %q, got nil", tc.secretRef)
			}
			svcErr, ok := err.(abstractions.ServiceError)
			if !ok {
				t.Fatalf("expected ServiceError, got %T: %v", err, err)
			}
			if svcErr.MessageCode() != messages.InvalidSecretRefURI {
				t.Fatalf("expected message code %q, got %q", messages.InvalidSecretRefURI.GetCode(), svcErr.MessageCode().GetCode())
			}
		})
	}
}

func TestRunEvaluationJobSidecarRejectsMalformedURI(t *testing.T) {
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)
	evaluation.Model.Auth = &api.ModelAuth{SecretRef: "file:///%zz"}

	providers := sampleLocalProviders(providerID, "true")
	cleanupDir(t, "job-1")

	cfg := &config.Config{
		Service: &config.ServiceConfig{Port: 8080},
		Sidecar: &config.SidecarConfig{LocalMode: true, BaseURL: "http://localhost:8082"},
	}
	rt, err := NewLocalRuntime(discardLogger(), cfg)
	if err != nil {
		t.Fatalf("NewLocalRuntime failed: %v", err)
	}
	rt = rt.WithContext(testContext(t))

	storage := &fakeStorage{providerConfigs: providers}

	benchmarks, err := handlers.GetJobBenchmarks(evaluation, nil)
	if err != nil {
		t.Fatalf("failed to resolve benchmarks: %v", err)
	}

	err = rt.RunEvaluationJob(evaluation, benchmarks, storage)
	if err == nil {
		t.Fatal("expected error for malformed URI, got nil")
	}
	svcErr, ok := err.(abstractions.ServiceError)
	if !ok {
		t.Fatalf("expected ServiceError, got %T: %v", err, err)
	}
	if svcErr.MessageCode() != messages.InvalidSecretRefURIParse {
		t.Fatalf("expected message code %q, got %q", messages.InvalidSecretRefURIParse.GetCode(), svcErr.MessageCode().GetCode())
	}
}

func TestRunEvaluationJobWithoutSidecarNoRewrite(t *testing.T) {
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)
	dirName := localJobDir("job-1", 0, providerID, "bench-1")
	sentinelPath := filepath.Join(dirName, "done")
	providers := sampleLocalProviders(providerID, fmt.Sprintf("touch %s", sentinelPath))
	cleanupDir(t, "job-1")

	callbackURL := "http://localhost:8080"
	rt := &LocalRuntime{
		logger:      discardLogger(),
		ctx:         testContext(t),
		tracker:     newTracker(),
		callbackURL: &callbackURL,
	}

	storage := &fakeStorage{providerConfigs: providers}

	benchmarks, err := handlers.GetJobBenchmarks(evaluation, nil)
	if err != nil {
		t.Fatalf("failed to resolve benchmarks: %v", err)
	}

	err = rt.RunEvaluationJob(evaluation, benchmarks, storage)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	waitForFile(t, sentinelPath, 5*time.Second)

	// Verify no sidecar-job-info.json was written
	sidecarInfoPath := filepath.Join(localJobsBaseDir, "job-1", shared.SidecarJobInfoFileName)
	if _, err := os.Stat(sidecarInfoPath); !os.IsNotExist(err) {
		t.Fatalf("expected no sidecar-job-info.json when sidecar is disabled, but file exists")
	}

	// Verify job.json model URL is unchanged
	jobSpecPath := filepath.Join(dirName, "meta", "job.json")
	data, err := os.ReadFile(jobSpecPath)
	if err != nil {
		t.Fatalf("expected job.json to exist, got %v", err)
	}

	var spec shared.JobSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatalf("expected valid JSON, got %v", err)
	}

	if spec.Model.URL != "http://model.example" {
		t.Fatalf("expected original model URL %q, got %q", "http://model.example", spec.Model.URL)
	}

	if spec.CallbackURL == nil || *spec.CallbackURL != callbackURL {
		t.Fatalf("expected callback_url %q, got %v", callbackURL, spec.CallbackURL)
	}
}

func TestNewLocalRuntimeWithSidecarConfig(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Service: &config.ServiceConfig{Port: 8080},
		Sidecar: &config.SidecarConfig{
			LocalMode: true,
			BaseURL:   "http://localhost:8082",
		},
	}
	rt, err := NewLocalRuntime(discardLogger(), cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	lr := rt.(*LocalRuntime)
	if lr.sidecarBaseURL != "http://localhost:8082" {
		t.Fatalf("expected sidecarBaseURL %q, got %q", "http://localhost:8082", lr.sidecarBaseURL)
	}
	if !lr.sidecarEnabled() {
		t.Fatal("expected sidecar to be enabled")
	}
}

func TestNewLocalRuntimeWithoutSidecarConfig(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Service: &config.ServiceConfig{Port: 8080},
	}
	rt, err := NewLocalRuntime(discardLogger(), cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	lr := rt.(*LocalRuntime)
	if lr.sidecarBaseURL != "" {
		t.Fatalf("expected empty sidecarBaseURL, got %q", lr.sidecarBaseURL)
	}
	if lr.sidecarEnabled() {
		t.Fatal("expected sidecar to be disabled")
	}
}

func TestNewLocalRuntimeSidecarEmptyBaseURL(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Service: &config.ServiceConfig{Port: 8080},
		Sidecar: &config.SidecarConfig{LocalMode: true},
	}
	rt, err := NewLocalRuntime(discardLogger(), cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	lr := rt.(*LocalRuntime)
	if lr.sidecarEnabled() {
		t.Fatal("expected sidecar to be disabled when base_url is empty")
	}
}

// TestNewLocalRuntimeSidecarMalformedBaseURL documents that malformed sidecar
// BaseURLs are rejected at config load time by SidecarConfig.ResolvePort, so
// they never reach NewLocalRuntime in production. The cases below would pass
// ResolvePort validation only with a well-formed URL; a bare string like
// "not-a-url" or a schemeless "localhost:8082" is caught before any runtime
// is constructed. See config/sidecar_config_test.go TestResolvePort and
// config/loader_test.go "sidecar ResolvePort error propagated".
func TestNewLocalRuntimeSidecarMalformedBaseURL(t *testing.T) {
	t.Parallel()

	malformed := []string{
		"not-a-url",
		"://missing-scheme",
		"ftp://localhost:8082",
		"http://:8082",
	}
	for _, baseURL := range malformed {
		sc := &config.SidecarConfig{BaseURL: baseURL}
		err := sc.ResolvePort()
		if err == nil {
			t.Errorf("expected ResolvePort to reject %q, but it passed", baseURL)
		}
	}

	// Valid URL passes ResolvePort and NewLocalRuntime accepts it.
	sc := &config.SidecarConfig{LocalMode: true, BaseURL: "http://localhost:8082"}
	if err := sc.ResolvePort(); err != nil {
		t.Fatalf("ResolvePort unexpectedly failed for valid URL: %v", err)
	}
	cfg := &config.Config{Sidecar: sc}
	rt, err := NewLocalRuntime(discardLogger(), cfg)
	if err != nil {
		t.Fatalf("NewLocalRuntime failed for valid sidecar config: %v", err)
	}
	lr := rt.(*LocalRuntime)
	if lr.sidecarBaseURL != "http://localhost:8082" {
		t.Fatalf("expected sidecarBaseURL %q, got %q", "http://localhost:8082", lr.sidecarBaseURL)
	}
}

func TestNewLocalRuntimeSidecarWithoutLocalMode(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Service: &config.ServiceConfig{Port: 8080},
		Sidecar: &config.SidecarConfig{
			BaseURL: "http://localhost:8080",
		},
	}
	rt, err := NewLocalRuntime(discardLogger(), cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	lr := rt.(*LocalRuntime)
	if lr.sidecarEnabled() {
		t.Fatal("expected sidecar to be disabled when local_mode is not set")
	}
}

func TestDeleteEvaluationJobResources(t *testing.T) {
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)

	// Pre-create the directory structure
	dirName := localJobDir("job-1", 0, providerID, "bench-1")
	metaDir := filepath.Join(dirName, "meta")
	if err := os.MkdirAll(metaDir, 0755); err != nil {
		t.Fatalf("failed to create test directory: %v", err)
	}
	jobSpecPath := filepath.Join(metaDir, "job.json")
	if err := os.WriteFile(jobSpecPath, []byte(`{}`), 0644); err != nil {
		t.Fatalf("failed to write test job.json: %v", err)
	}

	rt := &LocalRuntime{
		logger:  discardLogger(),
		tracker: newTracker(),
	}

	err := rt.DeleteEvaluationJobResources(evaluation)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify the benchmark directory was removed
	if _, err := os.Stat(dirName); !os.IsNotExist(err) {
		_ = os.RemoveAll(dirName) // Clean up before failing
		t.Fatalf("expected directory %s to be removed", dirName)
	}
}

func TestDeleteEvaluationJobResourcesNonExistent(t *testing.T) {
	providerID := "provider-nonexistent"
	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{ID: "job-nonexistent"},
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Benchmarks: []api.EvaluationBenchmarkConfig{
				{
					Ref:        api.Ref{ID: "bench-nonexistent"},
					ProviderID: providerID,
				},
			},
		},
	}

	rt := &LocalRuntime{
		logger:  discardLogger(),
		tracker: newTracker(),
	}

	err := rt.DeleteEvaluationJobResources(evaluation)
	if err != nil {
		t.Fatalf("expected no error for non-existent directory, got %v", err)
	}
}
