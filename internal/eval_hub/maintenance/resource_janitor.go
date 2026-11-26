package maintenance

import (
	"context"
	"errors"
	"log/slog"
	"math/rand"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	generationservice "github.com/eval-hub/eval-hub/internal/eval_hub/generation/service"
)

// ResourceJanitor schedules periodic maintenance. Its tasks must be safe to run
// concurrently on different API replicas while the janitor runs in EvalHub.
type ResourceJanitor struct {
	interval time.Duration
	sweep    func(context.Context) error
	logger   *slog.Logger
	cancel   context.CancelFunc
	done     chan struct{}
}

// StartResourceJanitor wires the service's maintenance tasks and starts their
// periodic execution. Stop the janitor before closing storage.
func StartResourceJanitor(parent context.Context, storage abstractions.Storage, cfg *config.ResourceJanitorConfig, logger *slog.Logger) (*ResourceJanitor, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	sweep, err := generationservice.NewGenerationRetentionSweep(storage, logger)
	if err != nil {
		return nil, err
	}
	return newResourceJanitor(parent, cfg.EffectiveInterval(), sweep, logger)
}

func newResourceJanitor(parent context.Context, interval time.Duration, sweep func(context.Context) error, logger *slog.Logger) (*ResourceJanitor, error) {
	if interval <= 0 || sweep == nil {
		return nil, errors.New("resource janitor requires a positive interval and a sweep function")
	}
	if logger == nil {
		logger = slog.Default()
	}
	ctx, cancel := context.WithCancel(parent)
	j := &ResourceJanitor{interval: interval, sweep: sweep, logger: logger, cancel: cancel, done: make(chan struct{})}
	go j.run(ctx)
	return j, nil
}

// Stop cancels the janitor and waits for its current sweep to return.
func (j *ResourceJanitor) Stop() {
	j.cancel()
	<-j.done
}

func (j *ResourceJanitor) run(ctx context.Context) {
	defer close(j.done)
	for ctx.Err() == nil {
		if err := j.sweep(ctx); err != nil && ctx.Err() == nil {
			j.logger.Error("Resource janitor sweep failed", "error", err)
		}
		timer := time.NewTimer(j.jitteredInterval())
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (j *ResourceJanitor) jitteredInterval() time.Duration {
	// Spread recurring sweeps across replicas using 80%-120% of the interval.
	return time.Duration(float64(j.interval) * (0.8 + rand.Float64()*0.4))
}
