package watchdog

import (
	"context"
	"log/slog"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_hub/constants"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// BenchmarkWatchdog periodically scans for evaluation jobs with stuck
// benchmarks and fails them after the configured timeout.
type BenchmarkWatchdog struct {
	logger   *slog.Logger
	storage  abstractions.Storage
	timeout  time.Duration
	interval time.Duration
	nowFunc  func() time.Time
}

// New creates a BenchmarkWatchdog with the given configuration.
func New(logger *slog.Logger, storage abstractions.Storage, serviceConfig *config.ServiceConfig) *BenchmarkWatchdog {
	return &BenchmarkWatchdog{
		logger:   logger.With("component", "benchmark-watchdog"),
		storage:  storage,
		timeout:  serviceConfig.EffectiveBenchmarkTimeout(),
		interval: serviceConfig.EffectiveBenchmarkWatchdogInterval(),
		nowFunc:  time.Now,
	}
}

// Run starts the watchdog loop. It blocks until ctx is cancelled.
func (w *BenchmarkWatchdog) Run(ctx context.Context) {
	w.logger.Info("Benchmark watchdog started",
		"timeout", w.timeout,
		"interval", w.interval,
	)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("Benchmark watchdog stopping")
			return
		case <-ticker.C:
			w.scan(ctx)
		}
	}
}

func (w *BenchmarkWatchdog) scan(ctx context.Context) {
	filter := &abstractions.QueryFilter{
		Limit:  1000,
		Offset: 0,
		Params: map[string]any{
			"status": string(api.OverallStateRunning),
		},
	}

	results, err := w.storage.WithContext(ctx).GetEvaluationJobs(filter)
	if err != nil {
		w.logger.Error("Failed to query running evaluation jobs", "error", err)
		return
	}

	now := w.nowFunc()
	for i := range results.Items {
		w.checkJob(ctx, &results.Items[i], now)
	}
}

func (w *BenchmarkWatchdog) checkJob(ctx context.Context, job *api.EvaluationJobResource, now time.Time) {
	if job.Status == nil || len(job.Status.Benchmarks) == 0 {
		return
	}

	for _, benchmark := range job.Status.Benchmarks {
		if api.IsBenchmarkTerminalState(benchmark.Status) {
			continue
		}

		lastUpdate := w.benchmarkLastUpdate(job, &benchmark)
		if lastUpdate.IsZero() {
			continue
		}

		if now.Sub(lastUpdate) <= w.timeout {
			continue
		}

		w.logger.Warn("Benchmark exceeded timeout, marking as failed",
			"job_id", job.Resource.ID,
			"benchmark_id", benchmark.ID,
			"benchmark_index", benchmark.BenchmarkIndex,
			"last_update", lastUpdate,
			"timeout", w.timeout,
		)

		w.failBenchmark(ctx, job, &benchmark, now)
	}
}

// benchmarkLastUpdate returns the most recent timestamp for staleness detection.
// Prefers the per-benchmark UpdatedAt; falls back to StartedAt, then the
// job-level UpdatedAt.
func (w *BenchmarkWatchdog) benchmarkLastUpdate(job *api.EvaluationJobResource, b *api.BenchmarkStatus) time.Time {
	if b.UpdatedAt != "" {
		if t, err := api.DateTimeFromString(b.UpdatedAt); err == nil {
			return t
		}
	}
	if b.StartedAt != "" {
		if t, err := api.DateTimeFromString(b.StartedAt); err == nil {
			return t
		}
	}
	if !job.Resource.UpdatedAt.IsZero() {
		return job.Resource.UpdatedAt
	}
	return time.Time{}
}

func (w *BenchmarkWatchdog) failBenchmark(ctx context.Context, job *api.EvaluationJobResource, benchmark *api.BenchmarkStatus, now time.Time) {
	statusEvent := &api.StatusEvent{
		BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
			ProviderID:     benchmark.ProviderID,
			ID:             benchmark.ID,
			BenchmarkIndex: benchmark.BenchmarkIndex,
			Status:         api.StateFailed,
			Phase:          benchmark.Phase,
			ErrorMessage: &api.MessageInfo{
				Message:       "Benchmark timed out: no status update received within " + w.timeout.String(),
				MessageCode:   constants.MESSAGE_CODE_BENCHMARK_TIMEOUT,
				MessageOrigin: api.MessageOriginServer,
			},
			CompletedAt: api.DateTimeToString(now),
		},
	}

	scoped := w.storage.WithContext(ctx).WithTenant(job.Resource.Tenant).WithOwner(job.Resource.Owner)
	if err := scoped.UpdateEvaluationJob(job.Resource.ID, statusEvent); err != nil {
		w.logger.Error("Failed to fail timed-out benchmark",
			"job_id", job.Resource.ID,
			"benchmark_id", benchmark.ID,
			"error", err,
		)
	}
}

// SetupWatchdog starts the benchmark watchdog in a goroutine and returns a
// done channel and cancel function, following the same pattern as
// config.SetupWatcher.
func SetupWatchdog(logger *slog.Logger, storage abstractions.Storage, serviceConfig *config.ServiceConfig) (chan struct{}, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan struct{})

	wd := New(logger, storage, serviceConfig)
	go func() {
		defer close(doneCh)
		wd.Run(ctx)
	}()

	return doneCh, cancel
}
