package local

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_hub/constants"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/metrics"
	"github.com/eval-hub/eval-hub/internal/eval_hub/runtimes/shared"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/internal/safefile"
	"github.com/eval-hub/eval-hub/pkg/api"
)

const (
	localJobsBaseDir      = "/tmp/evalhub-jobs"
	evalHubJobSpecPathEnv = "EVALHUB_JOB_SPEC_PATH"
	evalHubModeEnv        = "EVALHUB_MODE"
	mlflowTrackingURIEnv  = "MLFLOW_TRACKING_URI"
)

// jobTracker manages subprocess tracking per job for cancellation.
type jobTracker interface {
	registerJob(jobID string)
	addPID(jobID string, pid int)
	cancelJob(jobID string)
	isCancelled(jobID string) bool
}

// pidTracker tracks running subprocess PIDs per job so they can be killed on cancel.
type pidTracker struct {
	mu        sync.Mutex
	pids      map[string][]int // jobID -> list of PIDs
	cancelled map[string]bool  // jobs cancelled before all PIDs arrived
}

func (jr *pidTracker) registerJob(jobID string) {
	jr.mu.Lock()
	defer jr.mu.Unlock()
	jr.pids[jobID] = nil
}

func (jr *pidTracker) addPID(jobID string, pid int) {
	jr.mu.Lock()
	defer jr.mu.Unlock()
	if jr.cancelled[jobID] {
		_ = killProcessGroup(pid)
		return
	}
	jr.pids[jobID] = append(jr.pids[jobID], pid)
}

// cancelJob sends SIGKILL to the process group of every tracked PID for the
// job and removes the job's entry from the tracker. Any PIDs registered after
// this call via addPID will be killed immediately. Calling cancelJob for an
// unknown or already-cancelled job is a no-op.
func (jr *pidTracker) cancelJob(jobID string) {
	jr.mu.Lock()
	defer jr.mu.Unlock()
	if pids, ok := jr.pids[jobID]; ok {
		for _, pid := range pids {
			_ = killProcessGroup(pid)
		}
		delete(jr.pids, jobID)
	}
	jr.cancelled[jobID] = true
}

func (jr *pidTracker) isCancelled(jobID string) bool {
	jr.mu.Lock()
	defer jr.mu.Unlock()
	return jr.cancelled[jobID]
}

type LocalRuntime struct {
	logger               *slog.Logger
	ctx                  context.Context
	tracker              jobTracker
	callbackURL          *string
	mlflowTrackingURI    string
	sidecarBaseURL       string                     // non-empty when local sidecar mode is active
	sidecarModelDefaults *config.SidecarModelConfig // model proxy defaults from config; may be nil
}

func NewLocalRuntime(
	logger *slog.Logger,
	serviceConfig *config.Config,
) (abstractions.Runtime, error) {
	var sidecarBaseURL string
	var sidecarModelDefaults *config.SidecarModelConfig
	var mlflowTrackingURI string
	if serviceConfig != nil && serviceConfig.MLFlow != nil {
		mlflowTrackingURI = serviceConfig.MLFlow.EffectiveTrackingURI()
	}
	if serviceConfig != nil && serviceConfig.Sidecar != nil && serviceConfig.Sidecar.LocalMode && serviceConfig.Sidecar.BaseURL != "" {
		sidecarBaseURL = serviceConfig.Sidecar.BaseURL
		sidecarModelDefaults = serviceConfig.Sidecar.Model
	}
	return &LocalRuntime{
		logger:               logger,
		callbackURL:          buildCallbackURL(serviceConfig),
		mlflowTrackingURI:    mlflowTrackingURI,
		sidecarBaseURL:       sidecarBaseURL,
		sidecarModelDefaults: sidecarModelDefaults,
		tracker: &pidTracker{
			pids:      make(map[string][]int),
			cancelled: make(map[string]bool),
		},
	}, nil
}

func buildCallbackURL(serviceConfig *config.Config) *string {
	if serviceConfig == nil || serviceConfig.Service == nil || serviceConfig.Service.Port <= 0 {
		return nil
	}
	host := serviceConfig.Service.Host
	if host == "" {
		host = "localhost"
	}
	scheme := "http"
	if serviceConfig.Service.TLSEnabled() {
		scheme = "https"
	}
	u := fmt.Sprintf("%s://%s:%d", scheme, host, serviceConfig.Service.Port)
	return &u
}

func (r *LocalRuntime) WithLogger(logger *slog.Logger) abstractions.Runtime {
	return &LocalRuntime{
		logger:               logger,
		ctx:                  r.ctx,
		tracker:              r.tracker,
		callbackURL:          r.callbackURL,
		mlflowTrackingURI:    r.mlflowTrackingURI,
		sidecarBaseURL:       r.sidecarBaseURL,
		sidecarModelDefaults: r.sidecarModelDefaults,
	}
}

func (r *LocalRuntime) WithContext(ctx context.Context) abstractions.Runtime {
	return &LocalRuntime{
		logger:               r.logger,
		ctx:                  ctx,
		tracker:              r.tracker,
		callbackURL:          r.callbackURL,
		mlflowTrackingURI:    r.mlflowTrackingURI,
		sidecarBaseURL:       r.sidecarBaseURL,
		sidecarModelDefaults: r.sidecarModelDefaults,
	}
}

func (r *LocalRuntime) RunEvaluationJob(
	evaluation *api.EvaluationJobResource,
	benchmarks []api.EvaluationBenchmarkConfig,
	storage abstractions.RuntimeStorage,
) error {
	if r.ctx == nil {
		r.logger.Error("RunEvaluationJob called with nil context; WithContext must be called before RunEvaluationJob")
		return fmt.Errorf("local runtime: nil context — WithContext must be called before RunEvaluationJob")
	}

	if len(benchmarks) == 0 {
		return serviceerrors.NewServiceError(messages.EvaluationJobEmpty, "EvaluationJobID", evaluation.Resource.ID)
	}

	// Capture job ID before launching goroutine to avoid a data race
	// on the shared evaluation pointer.
	jobID := evaluation.Resource.ID

	r.tracker.registerJob(jobID)

	callbackURL := r.callbackURL
	if r.sidecarEnabled() {
		if evaluation.Model != nil && evaluation.Model.Auth != nil && evaluation.Model.Auth.SecretRef != "" {
			parsed, err := url.Parse(evaluation.Model.Auth.SecretRef)
			if err != nil {
				return serviceerrors.NewServiceError(messages.InvalidSecretRefURIParse,
					"SecretRef", evaluation.Model.Auth.SecretRef, "Detail", err.Error())
			}
			// Only the file:/// form (empty authority, non-empty path) is accepted.
			if parsed.Scheme != "file" || parsed.Host != "" || parsed.Opaque != "" || parsed.Path == "" || parsed.OmitHost {
				return serviceerrors.NewServiceError(messages.InvalidSecretRefURI, "SecretRef", evaluation.Model.Auth.SecretRef)
			}
		}
		if err := r.writeSidecarJobInfo(evaluation); err != nil {
			return fmt.Errorf("write sidecar job info: %w", err)
		}
		callbackURL = &r.sidecarBaseURL
	}

	for i, bench := range benchmarks {
		go func() {
			if err := r.runBenchmark(jobID, bench, i, evaluation, callbackURL, storage); err != nil {
				metrics.RecordBenchmarkRuntimeError(r.ctx, r.Name())
				r.logger.Error(
					"local runtime benchmark launch failed",
					"error", err,
					"job_id", jobID,
					"benchmark_id", bench.ID,
					"benchmark_index", i,
					"provider_id", bench.ProviderID,
				)
				r.failBenchmark(jobID, bench, i, storage, err.Error())
			}
		}()
	}

	return nil
}

// runBenchmark launches a single benchmark process. It writes the job spec,
// starts the command, and waits for it to finish. The caller is expected to
// invoke this from its own goroutine. cmd.Wait() reaps the child process to
// prevent zombies.
func (r *LocalRuntime) runBenchmark(
	jobID string,
	bench api.EvaluationBenchmarkConfig,
	benchmarkIndex int,
	evaluation *api.EvaluationJobResource,
	callbackURL *string,
	storage abstractions.RuntimeStorage,
) error {
	provider, err := storage.GetProvider(bench.ProviderID)
	if err != nil {
		return err
	}
	if provider.Runtime == nil || provider.Runtime.Local == nil || provider.Runtime.Local.Command == "" {
		return serviceerrors.NewServiceError(messages.LocalRuntimeNotEnabled, "ProviderID", bench.ProviderID)
	}

	if r.tracker.isCancelled(jobID) {
		return nil
	}

	// Build job spec JSON using shared logic
	spec, err := shared.BuildJobSpec(evaluation, bench.ProviderID, &bench, benchmarkIndex, callbackURL)
	if err != nil {
		return fmt.Errorf("build job spec: %w", err)
	}

	if r.sidecarEnabled() && spec.Model != nil && spec.Model.URL != "" {
		modelCopy := *spec.Model
		rewrittenURL, err := shared.RewriteModelURLForLocalSidecar(r.sidecarBaseURL, jobID, modelCopy.URL)
		if err != nil {
			return fmt.Errorf("rewrite model URL for sidecar: %w", err)
		}
		modelCopy.URL = rewrittenURL
		spec.Model = &modelCopy
	}

	// Create output directory: /tmp/evalhub-jobs/<job_id>/<benchmark_index>/<provider_id>/<benchmark_id>/
	jobDir := filepath.Join(localJobsBaseDir, jobID, fmt.Sprintf("%d", benchmarkIndex), bench.ProviderID, bench.ID)
	metaDir := filepath.Join(jobDir, "meta")
	if err := os.MkdirAll(metaDir, 0o750); err != nil {
		return fmt.Errorf("create meta directory: %w", err)
	}

	specJSON, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal job spec: %w", err)
	}

	// Write job.json
	jobSpecPath := filepath.Join(metaDir, "job.json")
	if err := os.WriteFile(jobSpecPath, []byte(specJSON), 0o600); err != nil {
		return fmt.Errorf("write job spec: %w", err)
	}

	absJobSpecPath, err := filepath.Abs(jobSpecPath)
	if err != nil {
		return fmt.Errorf("resolve job spec path: %w", err)
	}

	r.logger.Info(
		"local runtime job spec written",
		"job_id", jobID,
		"benchmark_id", bench.ID,
		"benchmark_index", benchmarkIndex,
		"provider_id", bench.ProviderID,
		"job_spec_path", absJobSpecPath,
	)

	// Build command using shell interpretation
	command := provider.Runtime.Local.Command
	// G204 -- local runtime executes provider-defined commands by design
	cmd := exec.Command("sh", "-c", command)
	// Setpgid places the child in its own process group (PGID = child PID).
	// This is critical for two reasons:
	//   1. cancelJob calls Kill(-PID, SIGKILL) which targets the entire process
	//      group. Without Setpgid the child inherits the Go process's group, so
	//      Kill(-PID) would kill the Go process itself.
	//   2. The negative PID ensures the entire subprocess tree is killed (sh +
	//      its children). Without it, only the direct child would be signalled
	//      and grandchildren could survive as orphans reparented to PID 1.
	setSysProcAttr(cmd)

	// Start with the inherited environment, then apply EvalHub/configuration
	// values, and finally provider values. Each later layer replaces matching
	// keys so the most specific configuration wins.
	cmd.Env = os.Environ()
	cmd.Env = replaceEnvironmentVariable(cmd.Env, evalHubJobSpecPathEnv, absJobSpecPath)
	cmd.Env = replaceEnvironmentVariable(cmd.Env, evalHubModeEnv, "local")
	if r.mlflowTrackingURI != "" {
		cmd.Env = replaceEnvironmentVariable(cmd.Env, mlflowTrackingURIEnv, r.mlflowTrackingURI)
	}
	for _, envVar := range provider.Runtime.Local.Env {
		if envVar.Name != "" {
			cmd.Env = replaceEnvironmentVariable(cmd.Env, envVar.Name, envVar.Value)
		}
	}

	// Capture stdout/stderr to log file
	logFilePath := filepath.Join(jobDir, "jobrun.log")
	logFile, err := safefile.Create(jobDir, "jobrun.log")
	if err != nil {
		return fmt.Errorf("create log file: %w", err)
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	r.logger.Info(
		"local runtime log file created",
		"job_id", jobID,
		"benchmark_id", bench.ID,
		"benchmark_index", benchmarkIndex,
		"provider_id", bench.ProviderID,
		"log_file", logFilePath,
	)

	// Start the process
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("start local process: %w", err)
	}

	pid := cmd.Process.Pid
	r.tracker.addPID(jobID, pid)

	// Close the log file — the child process has its own fd copy.
	_ = logFile.Close()

	r.logger.Info(
		"local runtime process started",
		"job_id", jobID,
		"benchmark_id", bench.ID,
		"benchmark_index", benchmarkIndex,
		"provider_id", bench.ProviderID,
		"pid", pid,
		"command", command,
	)

	// Reap the child process to prevent zombies. Each benchmark runs in its
	// own goroutine, so this blocks only this benchmark's goroutine.
	// Any cmd errors should be debugged from the logs; no action taken here.
	//
	// Ideally we would not need cmd.Wait() at all — the double-fork (fork/exec,
	// setsid, fork again) trick on Unix fully detaches the child and lets init
	// (PID 1) reap it, eliminating the need for a dedicated goroutine. However,
	// Windows does not support fork or setsid — processes are managed differently
	// via the Win32 API (CreateProcess) and there is no concept of zombie processes
	// in the same way. Until a common cross-platform approach is found for Linux,
	// macOS, and Windows, cmd.Wait() serves as the portable solution.
	_ = cmd.Wait()

	// If the job was cancelled while this goroutine was running, the directory
	// may have been recreated after DeleteEvaluationJobResources already
	// cleaned it up. Remove it now to prevent orphaned directories.
	if r.tracker.isCancelled(jobID) {
		_ = os.RemoveAll(filepath.Join(localJobsBaseDir, jobID))
	}

	return nil
}

// replaceEnvironmentVariable removes all existing entries for name before
// appending the replacement. Removing all entries matters because exec.Cmd
// uses the last matching entry, while subprocesses may observe duplicates.
func replaceEnvironmentVariable(env []string, name, value string) []string {
	prefix := name + "="
	filtered := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if !environmentVariableEntryMatches(entry, prefix) {
			filtered = append(filtered, entry)
		}
	}
	return append(filtered, prefix+value)
}

// environmentVariableEntryMatches applies the case rules used by the host
// operating system. Environment variable names are case-sensitive on Unix
// systems and case-insensitive on Windows.
func environmentVariableEntryMatches(entry, prefix string) bool {
	if len(entry) < len(prefix) {
		return false
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(entry[:len(prefix)], prefix)
	}
	return strings.HasPrefix(entry, prefix)
}

// failBenchmark updates storage to mark a benchmark as failed.
func (r *LocalRuntime) failBenchmark(
	jobID string,
	bench api.EvaluationBenchmarkConfig,
	benchmarkIndex int,
	storage abstractions.RuntimeStorage,
	errMsg string,
) {
	if storage == nil {
		return
	}
	runStatus := &api.StatusEvent{
		BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
			ProviderID:     bench.ProviderID,
			ID:             bench.ID,
			BenchmarkIndex: benchmarkIndex,
			Status:         api.StateFailed,
			ErrorMessage: api.WithMessageOrigin(&api.MessageInfo{
				Message:     errMsg,
				MessageCode: constants.MessageCodeEvaluationJobFailed,
			}, api.MessageOriginServer),
		},
	}
	if updateErr := storage.UpdateEvaluationJob(jobID, runStatus); updateErr != nil {
		r.logger.Error(
			"failed to update benchmark status",
			"error", updateErr,
			"job_id", jobID,
			"benchmark_id", bench.ID,
			"benchmark_index", benchmarkIndex,
			"provider_id", bench.ProviderID,
		)
	}
}

func (r *LocalRuntime) sidecarEnabled() bool {
	return r.sidecarBaseURL != ""
}

func (r *LocalRuntime) writeSidecarJobInfo(evaluation *api.EvaluationJobResource) error {
	var modelConfig *config.SidecarModelConfig
	modelURL := strings.TrimSpace(evaluation.Model.URL)
	if modelURL != "" {
		modelConfig = &config.SidecarModelConfig{
			URL:         modelURL,
			HTTPTimeout: shared.DefaultModelHTTPTimeout,
		}
		if r.sidecarModelDefaults != nil {
			if r.sidecarModelDefaults.HTTPTimeout > 0 {
				modelConfig.HTTPTimeout = r.sidecarModelDefaults.HTTPTimeout
			}
			modelConfig.InsecureSkipVerify = r.sidecarModelDefaults.InsecureSkipVerify
		}
		if evaluation.Model.Auth != nil && evaluation.Model.Auth.SecretRef != "" {
			modelConfig.AuthSecretMountPath = evaluation.Model.Auth.SecretRef
		}
	}

	info := &shared.SidecarJobInfo{Model: modelConfig}
	infoPath, err := shared.WriteSidecarJobInfo(localJobsBaseDir, evaluation.Resource.ID, info)
	if err != nil {
		return err
	}
	r.logger.Info(
		"sidecar job info written",
		"job_id", evaluation.Resource.ID,
		"path", infoPath,
	)
	return nil
}

func (r *LocalRuntime) DeleteEvaluationJobResources(evaluation *api.EvaluationJobResource) error {
	r.tracker.cancelJob(evaluation.Resource.ID)
	jobDir := filepath.Join(localJobsBaseDir, evaluation.Resource.ID)
	if err := os.RemoveAll(jobDir); err != nil {
		r.logger.Error(
			"failed to remove local runtime job directory",
			"error", err,
			"job_id", evaluation.Resource.ID,
			"directory", jobDir,
		)
		return err
	}
	r.logger.Info(
		"removed local runtime job directory",
		"job_id", evaluation.Resource.ID,
		"directory", jobDir,
	)
	return nil
}

func (r *LocalRuntime) Name() string {
	return "local"
}

func (r *LocalRuntime) ValidateHardwareProfiles(_ []api.EvaluationBenchmarkConfig) error {
	return nil
}
