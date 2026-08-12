package abstractions

import (
	"context"
	"log/slog"

	"github.com/eval-hub/eval-hub/pkg/api"
)

// Runtime interface defines the methods for running evaluation jobs. Concrete implementations
// hold the specific aspects of various runtimes (i.e. K8s, local, etc.). No other places in the code should
// be pointing directly to K8s or other runtime specific details.

// RuntimeStorage interface is used to update the evaluation job status and benchmarks
// and query providers. This is required because we might need these operations to be
// in a transaction and we don't want to give direct access to the storage layer because
// this will shortcut certain checks that are needed for the operation to be successful.
type RuntimeStorage interface {
	GetProvider(id string) (*api.ProviderResource, error)
	UpdateEvaluationJob(id string, runStatus *api.StatusEvent) error
}

type Runtime interface {
	WithLogger(logger *slog.Logger) Runtime
	WithContext(ctx context.Context) Runtime
	Name() string
	RunEvaluationJob(evaluation *api.EvaluationJobResource, benchmarks []api.EvaluationBenchmarkConfig, storage RuntimeStorage) error
	DeleteEvaluationJobResources(evaluation *api.EvaluationJobResource) error
	// GetEvaluationLogs returns plain-text workload logs. When benchmarkIndex is nil, logs
	// for all benchmarks are concatenated with section headers; otherwise only that benchmark.
	GetEvaluationLogs(
		evaluation *api.EvaluationJobResource,
		benchmarks []api.EvaluationBenchmarkConfig,
		benchmarkIndex *int,
		opts api.EvaluationLogOptions,
	) (string, error)
	// ValidateHardwareProfiles validates HardwareProfile refs on create (exist, enabled,
	// namespace configured). No-op for runtimes that do not use cluster HardwareProfiles.
	ValidateHardwareProfiles(benchmarks []api.EvaluationBenchmarkConfig) error
	// NotifyJobPhaseTransition informs the runtime that a benchmark job has transitioned to a
	// new phase. Implementations emit runtime-native signals (e.g., Kubernetes Events and label
	// patches on the backing Job object). Errors are absorbed internally — lifecycle signals are
	// best-effort and must never propagate to the caller.
	NotifyJobPhaseTransition(ctx context.Context, evaluation *api.EvaluationJobResource, benchmarkIndex int, state api.State)
	// NotifyThresholdViolation informs the runtime that a benchmark result breached its configured
	// threshold. Implementations emit an EvaluationThresholdViolated Kubernetes Event enriched with
	// the metric name, actual measured value, and configured threshold, and patch the evaluation-phase
	// label to ThresholdViolated. Errors are absorbed internally — signals are best-effort.
	NotifyThresholdViolation(ctx context.Context, evaluation *api.EvaluationJobResource, benchmarkIndex int, metricName string, actualValue, threshold float32)
}

// This interface must be decoupled from the service HTTP layer
