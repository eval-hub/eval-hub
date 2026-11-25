package maintenance

import (
	"context"
	"errors"
	"log/slog"
	"math/rand"
	"time"
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

// NewResourceJanitor starts a sweep at startup and schedules subsequent sweeps.
// Stop the janitor before closing dependencies used by the sweep.
func NewResourceJanitor(parent context.Context, interval time.Duration, sweep func(context.Context) error, logger *slog.Logger) (*ResourceJanitor, error) {
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
