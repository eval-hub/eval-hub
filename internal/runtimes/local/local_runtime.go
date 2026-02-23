package local

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/constants"
	"github.com/eval-hub/eval-hub/internal/messages"
	"github.com/eval-hub/eval-hub/internal/runtimes/shared"
	"github.com/eval-hub/eval-hub/internal/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
)

const localJobsBaseDir = "/tmp/evalhub-jobs"
const maxBenchmarkWorkers = 5

type LocalRuntime struct {
	logger    *slog.Logger
	ctx       context.Context
	providers map[string]api.ProviderResource
}

func NewLocalRuntime(
	logger *slog.Logger,
	providerConfigs map[string]api.ProviderResource,
) (abstractions.Runtime, error) {
	return &LocalRuntime{logger: logger, providers: providerConfigs}, nil
}

func (r *LocalRuntime) WithLogger(logger *slog.Logger) abstractions.Runtime {
	return &LocalRuntime{
		logger:    logger,
		ctx:       r.ctx,
		providers: r.providers,
	}
}

func (r *LocalRuntime) WithContext(ctx context.Context) abstractions.Runtime {
	return &LocalRuntime{
		logger:    r.logger,
		ctx:       ctx,
		providers: r.providers,
	}
}

func (r *LocalRuntime) RunEvaluationJob(
	evaluation *api.EvaluationJobResource,
	storage *abstractions.Storage,
) error {
	if r.ctx == nil {
		r.logger.Error("RunEvaluationJob called with nil context; WithContext must be called before RunEvaluationJob")
		panic("local runtime: nil context in RunEvaluationJob — WithContext was not called")
	}

	if len(evaluation.Benchmarks) == 0 {
		return fmt.Errorf("no benchmarks configured for job %s", evaluation.Resource.ID)
	}

	// Capture job ID before launching goroutines to avoid a data race
	// on the shared evaluation pointer.
	jobID := evaluation.Resource.ID

	var callbackURL *string
	if serviceURL := os.Getenv("SERVICE_URL"); serviceURL != "" {
		callbackURL = &serviceURL
	}

	type indexedBenchmark struct {
		index int
		bench api.BenchmarkConfig
	}
	benchmarks := make(chan indexedBenchmark, len(evaluation.Benchmarks))
	for i, bench := range evaluation.Benchmarks {
		benchmarks <- indexedBenchmark{index: i, bench: bench}
	}
	close(benchmarks)

	workerCount := maxBenchmarkWorkers
	if len(evaluation.Benchmarks) < workerCount {
		workerCount = len(evaluation.Benchmarks)
	}

	for i := 0; i < workerCount; i++ {
		go func() {
			for ib := range benchmarks {
				select {
				case <-r.ctx.Done():
					r.logger.Warn(
						"benchmark processing canceled",
						"job_id", jobID,
						"benchmark_id", ib.bench.ID,
					)
					return
				default:
					if err := r.runBenchmark(jobID, ib.bench, ib.index, evaluation, callbackURL, storage); err != nil {
						r.logger.Error(
							"local runtime benchmark launch failed",
							"error", err,
							"job_id", jobID,
							"benchmark_id", ib.bench.ID,
							"provider_id", ib.bench.ProviderID,
						)
					}
				}
			}
		}()
	}

	return nil
}

// runBenchmark launches a single benchmark process. It writes the job spec,
// starts the command, and waits for it to complete.
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
		r.failBenchmark(jobID, bench, storage, fmt.Sprintf("provider %q not found", bench.ProviderID))
		return fmt.Errorf("provider %q not found", bench.ProviderID)
	}
	if provider.Runtime == nil || provider.Runtime.Local == nil || provider.Runtime.Local.Command == "" {
		err := serviceerrors.NewServiceError(messages.LocalRuntimeNotEnabled, "ProviderID", bench.ProviderID)
		r.failBenchmark(jobID, bench, storage, err.Error())
		return err
	}

	// Build job spec JSON using shared logic
	specJSON, err := shared.BuildJobSpecJSON(evaluation, bench.ProviderID, bench.ID, benchmarkIndex, callbackURL)
	if err != nil {
		r.failBenchmark(jobID, bench, storage, fmt.Sprintf("build job spec: %s", err))
		return fmt.Errorf("build job spec: %w", err)
	}

	// Create output directory: /tmp/evalhub-jobs/<jobid>/<providerid>/<benchmarkid>/
	jobDir := filepath.Join(localJobsBaseDir, jobID, bench.ProviderID, bench.ID)
	metaDir := filepath.Join(jobDir, "meta")
	if err := os.MkdirAll(metaDir, 0755); err != nil {
		return fmt.Errorf("create meta directory: %w", err)
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
		"provider_id", bench.ProviderID,
		"job_spec_path", absJobSpecPath,
	)

	// Build command using shell interpretation
	command := provider.Runtime.Local.Command
	cmd := exec.CommandContext(r.ctx, "sh", "-c", command)

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
		"log_file", logFilePath,
	)

	// Start the process asynchronously
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("start local process: %w", err)
	}

	r.logger.Info(
		"local runtime process started",
		"job_id", jobID,
		"benchmark_id", bench.ID,
		"provider_id", bench.ProviderID,
		"pid", cmd.Process.Pid,
		"command", command,
	)

	// Wait for completion (already running in a goroutine via RunEvaluationJob)
	defer logFile.Close()
	if err := cmd.Wait(); err != nil {
		r.logger.Error(
			"local runtime process failed",
			"error", err,
			"job_id", jobID,
			"benchmark_id", bench.ID,
			"provider_id", bench.ProviderID,
		)

		// In local mode, fail the job if the process exits with error,
		//  unless a callback already reported failure.
		if !r.benchmarkHasAlreadyFailed(jobID, bench, storage) {
			r.failBenchmark(jobID, bench, storage, err.Error())
		} else {
			r.logger.Warn(
				"skipping failBenchmark: result already reported via callback",
				"job_id", jobID,
				"benchmark_id", bench.ID,
				"provider_id", bench.ProviderID,
			)
		}
	} else {
		r.logger.Info(
			"local runtime process completed",
			"job_id", jobID,
			"benchmark_id", bench.ID,
			"provider_id", bench.ProviderID,
		)
	}

	return nil
}

// benchmarkHasAlreadyFailed checks whether a benchmark has already been marked as failed
// (e.g. via a callback from the benchmark process itself).
func (r *LocalRuntime) benchmarkHasAlreadyFailed(
	jobID string,
	bench api.BenchmarkConfig,
	storage *abstractions.Storage,
) bool {
	if storage == nil || *storage == nil {
		return false
	}
	job, err := (*storage).GetEvaluationJob(jobID)
	if err != nil || job == nil || job.Status == nil {
		return false
	}
	for _, bs := range job.Status.Benchmarks {
		if bs.ID == bench.ID && bs.ProviderID == bench.ProviderID && bs.Status == api.StateFailed {
			return true
		}
	}
	return false
}

// failBenchmark updates storage to mark a benchmark as failed.
func (r *LocalRuntime) failBenchmark(
	jobID string,
	bench api.BenchmarkConfig,
	storage *abstractions.Storage,
	errMsg string,
) {
	if storage == nil || *storage == nil {
		return
	}
	runStatus := &api.StatusEvent{
		BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
			ProviderID: bench.ProviderID,
			ID:         bench.ID,
			Status:     api.StateFailed,
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
			"provider_id", bench.ProviderID,
		)
	}
}

func (r *LocalRuntime) DeleteEvaluationJobResources(evaluation *api.EvaluationJobResource) error {
	var deleteErr error
	for _, bench := range evaluation.Benchmarks {
		benchDir := filepath.Join(localJobsBaseDir, evaluation.Resource.ID, bench.ProviderID, bench.ID)
		if err := os.RemoveAll(benchDir); err != nil {
			deleteErr = errors.Join(deleteErr, err)
			r.logger.Error(
				"failed to remove local runtime directory",
				"error", err,
				"job_id", evaluation.Resource.ID,
				"benchmark_id", bench.ID,
				"provider_id", bench.ProviderID,
				"directory", benchDir,
			)
		} else {
			r.logger.Info(
				"removed local runtime directory",
				"job_id", evaluation.Resource.ID,
				"benchmark_id", bench.ID,
				"provider_id", bench.ProviderID,
				"directory", benchDir,
			)
		}
	}
	return deleteErr
}

func (r *LocalRuntime) Name() string {
	return "local"
}
