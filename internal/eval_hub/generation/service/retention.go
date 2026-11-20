package service

import (
	"context"
	"errors"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
)

// ExpiredJobCleaner deletes terminal generation records with an expiry at or
// before the supplied time. Implementations must bound each call by limit.
type ExpiredJobCleaner interface {
	DeleteExpiredCollectionGenerationJobs(now time.Time, limit int) (int, error)
}

// RetentionJanitor performs bounded, idempotent cleanup on every service replica.
// It currently runs inside each EvalHub service process as a temporary placement
// until maintenance work that needs one active owner (such as coordinated garbage
// collection or Kubernetes Job watching) moves to an external process. This
// database cleanup is bounded and idempotent, so it does not require a singleton.
type RetentionJanitor struct {
	storage      abstractions.Storage
	interval     time.Duration
	batch        int
	logger       *slog.Logger
	now          func() time.Time
	nextInterval func() time.Duration
	cancel       context.CancelFunc
	done         chan struct{}
	stopOnce     sync.Once
}

// NewRetentionJanitor validates and starts the service-local cleanup loop. Stop it
// before closing storage during service shutdown.
func NewRetentionJanitor(parent context.Context, storage abstractions.Storage, cfg *config.ServiceGenerationConfig, logger *slog.Logger) (*RetentionJanitor, error) {
	if storage == nil {
		return nil, errors.New("retention cleanup storage is required")
	}
	if cfg == nil {
		cfg = &config.ServiceGenerationConfig{}
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if _, ok := storage.(ExpiredJobCleaner); !ok {
		return nil, errors.New("storage does not support collection-generation retention cleanup")
	}
	if logger == nil {
		logger = slog.Default()
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	janitor := &RetentionJanitor{
		storage: storage, interval: cfg.EffectiveCleanupInterval(),
		batch: cfg.EffectiveCleanupBatchSize(), logger: logger, now: time.Now,
		cancel: cancel, done: make(chan struct{}),
	}
	janitor.nextInterval = janitor.jitteredInterval
	go func() {
		defer close(janitor.done)
		janitor.run(ctx)
	}()
	return janitor, nil
}

// Stop terminates the background loop and waits for any in-flight sweep to return.
func (j *RetentionJanitor) Stop() {
	if j == nil {
		return
	}
	j.stopOnce.Do(j.cancel)
	<-j.done
}

func (j *RetentionJanitor) run(ctx context.Context) {
	j.sweep(ctx)
	for {
		timer := time.NewTimer(j.nextInterval())
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			j.sweep(ctx)
		}
	}
}

func (j *RetentionJanitor) jitteredInterval() time.Duration {
	// Spread replicas over 80%-120% of the configured interval while keeping each
	// wait bounded around the deployment's requested cleanup cadence.
	factor := 0.8 + rand.Float64()*0.4
	return time.Duration(float64(j.interval) * factor)
}

// Sweep performs one bounded cleanup pass and returns the number of deleted records.
func (j *RetentionJanitor) Sweep(ctx context.Context) (int, error) {
	if j == nil || j.storage == nil {
		return 0, errors.New("retention janitor is not configured")
	}
	cleaner, ok := j.storage.WithContext(ctx).(ExpiredJobCleaner)
	if !ok {
		return 0, errors.New("storage does not support collection-generation retention cleanup")
	}
	return cleaner.DeleteExpiredCollectionGenerationJobs(j.now().UTC(), j.batch)
}

func (j *RetentionJanitor) sweep(ctx context.Context) {
	deleted, err := j.Sweep(ctx)
	if err != nil {
		j.logger.Error("Failed to clean expired collection-generation jobs", "error", err)
		return
	}
	if deleted > 0 {
		j.logger.Info("Cleaned expired collection-generation jobs", "deleted", deleted)
	}
}
