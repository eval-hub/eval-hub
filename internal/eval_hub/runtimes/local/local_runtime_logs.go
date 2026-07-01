package local

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/runtimes/shared"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
)

const localLogContainerName = "local"

func (r *LocalRuntime) GetEvaluationLogs(
	evaluation *api.EvaluationJobResource,
	benchmarks []api.EvaluationBenchmarkConfig,
	benchmarkIndex *int,
	opts api.EvaluationLogOptions,
) (string, error) {
	if r.ctx == nil {
		return "", fmt.Errorf("local runtime: nil context — WithContext must be called before GetEvaluationLogs")
	}
	if len(benchmarks) == 0 {
		return "", serviceerrors.NewServiceError(messages.EvaluationJobEmpty, "EvaluationJobID", evaluation.Resource.ID)
	}

	if benchmarkIndex != nil {
		if *benchmarkIndex < 0 || *benchmarkIndex >= len(benchmarks) {
			return "", serviceerrors.NewServiceError(
				messages.ResourceNotFound,
				"Type", "benchmark",
				"ResourceId", fmt.Sprintf("%d", *benchmarkIndex),
			)
		}
		return r.readBenchmarkLogs(evaluation.Resource.ID, benchmarks[*benchmarkIndex], *benchmarkIndex, opts, false)
	}

	var sections []string
	for i, bench := range benchmarks {
		section, err := r.readBenchmarkLogs(evaluation.Resource.ID, bench, i, opts, true)
		if err != nil {
			return "", err
		}
		if section != "" {
			sections = append(sections, section)
		}
	}
	return strings.Join(sections, "\n"), nil
}

func (r *LocalRuntime) readBenchmarkLogs(
	jobID string,
	bench api.EvaluationBenchmarkConfig,
	benchmarkIndex int,
	opts api.EvaluationLogOptions,
	includeHeader bool,
) (string, error) {
	jobDir := filepath.Join(localJobsBaseDir, jobID, fmt.Sprintf("%d", benchmarkIndex), bench.ProviderID, bench.ID)
	logFilePath := filepath.Join(jobDir, "jobrun.log")
	lines, err := shared.TailFileLines(logFilePath, opts.TailLines)
	if err != nil {
		return "", fmt.Errorf("read local benchmark logs: %w", err)
	}
	if !includeHeader {
		return lines, nil
	}
	header := shared.FormatLogSectionHeader(
		fmt.Sprintf("%s-%d", jobID, benchmarkIndex),
		localLogContainerName,
		bench.ID,
	)
	if lines == "" {
		return header, nil
	}
	return header + "\n" + lines, nil
}
