package otel

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/pkg/api"
	otellog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/global"
)

const (
	jobLogsInstrumentationScope  = "github.com/eval-hub/eval-hub/job"
	jobContainerLogExportTimeout = 30 * time.Second
	sectionHeaderPrefix          = "=== pod="
)

// ExportJobContainerLogsAsync fetches runtime container logs after job completion and emits them to OTEL Logs.
func ExportJobContainerLogsAsync(
	parentCtx context.Context,
	runtime abstractions.Runtime,
	job *api.EvaluationJobResource,
	benchmarks []api.EvaluationBenchmarkConfig,
	logger *slog.Logger,
) {
	if runtime == nil || job == nil || len(benchmarks) == 0 {
		return
	}
	if global.GetLoggerProvider() == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(parentCtx), jobContainerLogExportTimeout)
		defer cancel()

		logs, err := runtime.WithContext(ctx).GetEvaluationLogs(
			job,
			benchmarks,
			nil,
			api.EvaluationLogOptions{TailLines: api.DefaultLogTailLines},
		)
		if err != nil {
			logger.WarnContext(ctx, "failed to fetch container logs for OTEL export",
				"job_id", job.Resource.ID,
				"error", err,
			)
			return
		}
		if strings.TrimSpace(logs) == "" {
			return
		}

		emitContainerLogs(ctx, job, logs)
	}()
}

func emitContainerLogs(ctx context.Context, job *api.EvaluationJobResource, logs string) {
	otelLogger := global.GetLoggerProvider().Logger(jobLogsInstrumentationScope)

	jobID := job.Resource.ID
	jobState := ""
	if job.Status != nil {
		jobState = string(job.Status.State)
	}

	var benchmarkID string
	for _, line := range strings.Split(logs, "\n") {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, sectionHeaderPrefix) {
			benchmarkID = parseBenchmarkIDFromSectionHeader(line)
			continue
		}

		var record otellog.Record
		record.SetBody(otellog.StringValue(line))
		record.SetSeverity(otellog.SeverityInfo)
		record.AddAttributes(
			otellog.String("evalhub.job.id", jobID),
			otellog.String("evalhub.log.source", "container"),
		)
		if jobState != "" {
			record.AddAttributes(otellog.String("evalhub.job.state", jobState))
		}
		if benchmarkID != "" {
			record.AddAttributes(otellog.String("evalhub.benchmark.id", benchmarkID))
		}
		otelLogger.Emit(ctx, record)
	}
}

func parseBenchmarkIDFromSectionHeader(header string) string {
	const key = "benchmark_id="
	idx := strings.Index(header, key)
	if idx < 0 {
		return ""
	}
	rest := header[idx+len(key):]
	if end := strings.Index(rest, " "); end >= 0 {
		rest = rest[:end]
	}
	return strings.TrimSuffix(rest, " ===")
}
