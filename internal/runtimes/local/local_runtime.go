package local

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/constants"
	"github.com/eval-hub/eval-hub/internal/messages"
	"github.com/eval-hub/eval-hub/internal/runtimes/shared"
	"github.com/eval-hub/eval-hub/internal/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
)

const localJobsBaseDir = "/tmp/evalhub-jobs"

// jobTracker manages subprocess tracking per job for cancellation.
type jobTracker interface {
	registerJob(jobID string)
	addPID(jobID string, pid int)
	cancelJob(jobID string)
}

// pidTracker tracks running subprocess PIDs per job so they can be killed on cancel.
type pidTracker struct {
	mu   sync.Mutex
	pids map[string][]int // jobID -> list of PIDs
}

func (jr *pidTracker) registerJob(jobID string) {
	jr.mu.Lock()
	defer jr.mu.Unlock()
	jr.pids[jobID] = nil
}

func (jr *pidTracker) addPID(jobID string, pid int) {
	jr.mu.Lock()
	defer jr.mu.Unlock()
	if _, ok := jr.pids[jobID]; ok {
		jr.pids[jobID] = append(jr.pids[jobID], pid)
	}
}

// cancelJob sends SIGKILL to the process group of every tracked PID for the
// job and removes the job's entry from the tracker. It is idempotent: calling
// it for an unknown or already-cancelled job is a no-op.
func (jr *pidTracker) cancelJob(jobID string) {
	jr.mu.Lock()
	defer jr.mu.Unlock()
	if pids, ok := jr.pids[jobID]; ok {
		for _, pid := range pids {
			// Kill the entire process group (negative PID).
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		}
		delete(jr.pids, jobID)
	}
}

type LocalRuntime struct {
	logger    *slog.Logger
	ctx       context.Context
	providers map[string]api.ProviderResource
	tracker   jobTracker
}

func NewLocalRuntime(
	logger *slog.Logger,
	providerConfigs map[string]api.ProviderResource,
) (abstractions.Runtime, error) {
	return &LocalRuntime{
		logger:    logger,
		providers: providerConfigs,
		tracker: &pidTracker{pids: make(map[string][]int)},
	}, nil
}

func (r *LocalRuntime) WithLogger(logger *slog.Logger) abstractions.Runtime {
	return &LocalRuntime{
		logger:    logger,
		ctx:       r.ctx,
		providers: r.providers,
		tracker:   r.tracker,
	}
}

func (r *LocalRuntime) WithContext(ctx context.Context) abstractions.Runtime {
	return &LocalRuntime{
		logger:    r.logger,
		ctx:       ctx,
		providers: r.providers,
		tracker:   r.tracker,
	}
}

func (r *LocalRuntime) RunEvaluationJob(
	evaluation *api.EvaluationJobResource,
	storage *abstractions.Storage,
) error {
	if r.ctx == nil {
		r.logger.Error("RunEvaluationJob called with nil context; WithContext must be called before RunEvaluationJob")
		return fmt.Errorf("local runtime: nil context — WithContext must be called before RunEvaluationJob")
	}

	if len(evaluation.Benchmarks) == 0 {
		return fmt.Errorf("no benchmarks configured for job %s", evaluation.Resource.ID)
	}

	// Capture job ID before launching goroutine to avoid a data race
	// on the shared evaluation pointer.
	jobID := evaluation.Resource.ID

	r.tracker.registerJob(jobID)

	var callbackURL *string
	if serviceURL := os.Getenv("SERVICE_URL"); serviceURL != "" {
		callbackURL = &serviceURL
	}

	go func() {
		for i, bench := range evaluation.Benchmarks {
			if err := r.runBenchmark(jobID, bench, i, evaluation, callbackURL, storage); err != nil {
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
		}
	}()

	return nil
}

// runBenchmark launches a single benchmark process. It writes the job spec,
// starts the command, and returns immediately. A background goroutine calls
// cmd.Wait() to reap the child process and prevent zombies.
func (r *LocalRuntime) runBenchmark(
	jobID string,
	bench api.BenchmarkConfig,
	benchmarkIndex int,
	evaluation *api.EvaluationJobResource,
	callbackURL *string,
	storage *abstractions.Storage,
) error {
	provider, ok := r.providers[bench.ProviderID]
	if !ok {
		return fmt.Errorf("provider %q not found", bench.ProviderID)
	}
	if provider.Runtime == nil || provider.Runtime.Local == nil || provider.Runtime.Local.Command == "" {
		return serviceerrors.NewServiceError(messages.LocalRuntimeNotEnabled, "ProviderID", bench.ProviderID)
	}

	// Build job spec JSON using shared logic
	spec, err := shared.BuildJobSpec(evaluation, bench.ProviderID, bench.ID, benchmarkIndex, callbackURL)
	if err != nil {
		return fmt.Errorf("build job spec: %w", err)
	}

	// Create output directory: /tmp/evalhub-jobs/<job_id>/<benchmark_index>/<provider_id>/<benchmark_id>/
	jobDir := filepath.Join(localJobsBaseDir, jobID, fmt.Sprintf("%d", benchmarkIndex), bench.ProviderID, bench.ID)
	metaDir := filepath.Join(jobDir, "meta")
	if err := os.MkdirAll(metaDir, 0755); err != nil {
		return fmt.Errorf("create meta directory: %w", err)
	}

	specJSON, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal job spec: %w", err)
	}

	// Write job.json
	jobSpecPath := filepath.Join(metaDir, "job.json")
	if err := os.WriteFile(jobSpecPath, []byte(specJSON), 0644); err != nil {
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
	cmd := exec.Command("sh", "-c", command)
	// Setpgid places the child in its own process group (PGID = child PID).
	// This is critical for two reasons:
	//   1. cancelJob calls Kill(-PID, SIGKILL) which targets the entire process
	//      group. Without Setpgid the child inherits the Go process's group, so
	//      Kill(-PID) would kill the Go process itself.
	//   2. The negative PID ensures the entire subprocess tree is killed (sh +
	//      its children). Without it, only the direct child would be signalled
	//      and grandchildren could survive as orphans reparented to PID 1.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Set environment variables
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("EVALHUB_JOB_SPEC_PATH=%s", absJobSpecPath),
	)
	for _, envVar := range provider.Runtime.Local.Env {
		if envVar.Name != "" {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", envVar.Name, envVar.Value))
		}
	}

	// Capture stdout/stderr to log file
	logFilePath := filepath.Join(jobDir, "jobrun.log")
	logFile, err := os.Create(logFilePath)
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
		logFile.Close()
		return fmt.Errorf("start local process: %w", err)
	}

	pid := cmd.Process.Pid
	r.tracker.addPID(jobID, pid)

	// Close the log file — the child process has its own fd copy.
	logFile.Close()

	r.logger.Info(
		"local runtime process started",
		"job_id", jobID,
		"benchmark_id", bench.ID,
		"benchmark_index", benchmarkIndex,
		"provider_id", bench.ProviderID,
		"pid", pid,
		"command", command,
	)

	// Stopgap fix: reap the child process in the background to prevent zombies.
	// Any cmd errors should be debugged from the logs; no action taken here.
	//
	// Ideally we would not need cmd.Wait() at all — the double-fork (fork/exec,
	// setsid, fork again) trick on Unix fully detaches the child and lets init
	// (PID 1) reap it, eliminating the need for a dedicated goroutine. However,
	// Windows does not support fork or setsid — processes are managed differently
	// via the Win32 API (CreateProcess) and there is no concept of zombie processes
	// in the same way. Until a common cross-platform approach is found for Linux,
	// macOS, and Windows, this goroutine calling cmd.Wait() serves as the portable
	// stopgap solution.
	go func() {
		_ = cmd.Wait()
	}()

	return nil
}

// failBenchmark updates storage to mark a benchmark as failed.
func (r *LocalRuntime) failBenchmark(
	jobID string,
	bench api.BenchmarkConfig,
	benchmarkIndex int,
	storage *abstractions.Storage,
	errMsg string,
) {
	if storage == nil || *storage == nil {
		return
	}
	runStatus := &api.StatusEvent{
		BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
			ProviderID:     bench.ProviderID,
			ID:             bench.ID,
			BenchmarkIndex: benchmarkIndex,
			Status:         api.StateFailed,
			ErrorMessage: &api.MessageInfo{
				Message:     errMsg,
				MessageCode: constants.MESSAGE_CODE_EVALUATION_JOB_FAILED,
			},
		},
	}
	if updateErr := (*storage).UpdateEvaluationJob(jobID, runStatus); updateErr != nil {
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
