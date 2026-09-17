package local

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/runtimes/shared"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
)

const localLogContainerName = "local"

func (r *LocalRuntime) StreamEvaluationLogs(
	evaluation *api.EvaluationJobResource,
	benchmarks []api.EvaluationBenchmarkConfig,
	benchmarkIndex *int,
	opts api.EvaluationLogOptions,
	w io.Writer,
) error {
	if r.ctx == nil {
		return fmt.Errorf("local runtime: nil context — WithContext must be called before StreamEvaluationLogs")
	}
	if len(benchmarks) == 0 {
		return serviceerrors.NewServiceError(messages.EvaluationJobEmpty, "EvaluationJobID", evaluation.Resource.ID)
	}

	if benchmarkIndex != nil {
		if *benchmarkIndex < 0 || *benchmarkIndex >= len(benchmarks) {
			return serviceerrors.NewServiceError(
				messages.ResourceNotFound,
				"Type", "benchmark",
				"ResourceId", fmt.Sprintf("%d", *benchmarkIndex),
			)
		}
		return r.streamBenchmarkLogs(evaluation.Resource.ID, benchmarks[*benchmarkIndex], *benchmarkIndex, opts, false, w)
	}

	for i, bench := range benchmarks {
		if i > 0 {
			if _, err := fmt.Fprint(w, "\n"); err != nil {
				return err
			}
		}
		if err := r.streamBenchmarkLogs(evaluation.Resource.ID, bench, i, opts, true, w); err != nil {
			return err
		}
	}
	return nil
}

func (r *LocalRuntime) streamBenchmarkLogs(
	jobID string,
	bench api.EvaluationBenchmarkConfig,
	benchmarkIndex int,
	opts api.EvaluationLogOptions,
	includeHeader bool,
	w io.Writer,
) error {
	jobDir := filepath.Join(localJobsBaseDir, jobID, fmt.Sprintf("%d", benchmarkIndex), bench.ProviderID, bench.ID)
	logFilePath := filepath.Join(jobDir, "jobrun.log")

	if includeHeader {
		header := shared.FormatLogSectionHeader(
			fmt.Sprintf("%s-%d", jobID, benchmarkIndex),
			localLogContainerName,
			bench.ID,
		)
		if _, err := fmt.Fprint(w, header+"\n"); err != nil {
			return err
		}
	}

	if opts.TailLines == api.AllLogLines {
		return shared.StreamFileAll(logFilePath, w)
	}

	lines, err := shared.TailFileLines(logFilePath, opts.TailLines)
	if err != nil {
		return fmt.Errorf("read local benchmark logs: %w", err)
	}
	if lines != "" {
		_, err = fmt.Fprint(w, lines)
	}
	return err
}
