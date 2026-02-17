package local
// Assisted-by: claude code

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/constants"
	"github.com/eval-hub/eval-hub/internal/runtimes/shared"
	"github.com/eval-hub/eval-hub/pkg/api"
)

const localRunDir = "/tmp/evalhub-local"

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
	if len(evaluation.Benchmarks) == 0 {
		return fmt.Errorf("no benchmarks configured for job %s", evaluation.Resource.ID)
	}

	// TODO: Support multiple benchmarks per job
	if len(evaluation.Benchmarks) > 1 {
		r.logger.Warn(
			"local runtime only supports 1 benchmark per job, additional benchmarks will be skipped",
			"job_id", evaluation.Resource.ID,
			"total_benchmarks", len(evaluation.Benchmarks),
		)
	}

	bench := evaluation.Benchmarks[0]
	provider, ok := r.providers[bench.ProviderID]
	if !ok {
		return fmt.Errorf("provider %q not found", bench.ProviderID)
	}
	if provider.Runtime == nil || provider.Runtime.Local == nil || provider.Runtime.Local.Command == "" {
		return fmt.Errorf("provider %q missing local runtime command", bench.ProviderID)
	}

	// Build job spec JSON using shared logic
	var callbackURL *string
	if serviceURL := os.Getenv("SERVICE_URL"); serviceURL != "" {
		callbackURL = &serviceURL
	}
	specJSON, err := shared.BuildJobSpecJSON(evaluation, provider.ID, bench.ID, callbackURL)
	if err != nil {
		return fmt.Errorf("build job spec: %w", err)
	}

	// Create output directory: /tmp/evalhub-local/<jobname>/
	dirName := shared.JobName(evaluation.Resource.ID, bench.ProviderID, bench.ID)
	metaDir := filepath.Join(localRunDir, dirName, "meta")
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
		"job_id", evaluation.Resource.ID,
		"benchmark_id", bench.ID,
		"provider_id", bench.ProviderID,
		"job_spec_path", absJobSpecPath,
	)

	// Build command using shell interpretation
	command := provider.Runtime.Local.Command
	ctx := r.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", command)

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
	logFilePath := filepath.Join(localRunDir, dirName, "jobrun.log")
	logFile, err := os.Create(logFilePath)
	if err != nil {
		return fmt.Errorf("create log file: %w", err)
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	r.logger.Info(
		"local runtime log file created",
		"job_id", evaluation.Resource.ID,
		"log_file", logFilePath,
	)

	// Start the process asynchronously
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("start local process: %w", err)
	}

	r.logger.Info(
		"local runtime process started",
		"job_id", evaluation.Resource.ID,
		"benchmark_id", bench.ID,
		"provider_id", bench.ProviderID,
		"pid", cmd.Process.Pid,
		"command", command,
	)

	// Wait for completion in background goroutine
	go func() {
		defer logFile.Close()
		if err := cmd.Wait(); err != nil {
			r.logger.Error(
				"local runtime process failed",
				"error", err,
				"job_id", evaluation.Resource.ID,
				"benchmark_id", bench.ID,
				"provider_id", bench.ProviderID,
			)

			if storage != nil && *storage != nil {
				runStatus := &api.StatusEvent{
					BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
						ProviderID:   bench.ProviderID,
						ID:           bench.ID,
						Status:       api.StateFailed,
						ErrorMessage: &api.MessageInfo{
							Message: err.Error(),
							MessageCode: constants.MESSAGE_CODE_EVALUATION_JOB_FAILED},
					},
				}
				if updateErr := (*storage).UpdateEvaluationJob(evaluation.Resource.ID, runStatus); updateErr != nil {
					r.logger.Error(
						"failed to update benchmark status",
						"error", updateErr,
						"job_id", evaluation.Resource.ID,
						"benchmark_id", bench.ID,
					)
				}
			}
		} else {
			r.logger.Info(
				"local runtime process completed",
				"job_id", evaluation.Resource.ID,
				"benchmark_id", bench.ID,
				"provider_id", bench.ProviderID,
			)
		}
	}()

	return nil
}

func (r *LocalRuntime) DeleteEvaluationJobResources(evaluation *api.EvaluationJobResource) error {
	for _, bench := range evaluation.Benchmarks {
		dirName := shared.JobName(evaluation.Resource.ID, bench.ProviderID, bench.ID)
		metaDir := filepath.Join(localRunDir, dirName)
		if err := os.RemoveAll(metaDir); err != nil {
			r.logger.Error(
				"failed to remove local runtime directory",
				"error", err,
				"job_id", evaluation.Resource.ID,
				"benchmark_id", bench.ID,
				"directory", metaDir,
			)
		} else {
			r.logger.Info(
				"removed local runtime directory",
				"job_id", evaluation.Resource.ID,
				"benchmark_id", bench.ID,
				"directory", metaDir,
			)
		}
	}
	return nil
}

func (r *LocalRuntime) Name() string {
	return "local"
}
