package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
)

const (
	generationRetentionBatchSize    = 100
	generationRetentionSweepTimeout = time.Minute
)

// ExpiredJobCleaner deletes up to limit generation records eligible for
// physical removal at the supplied time.
type ExpiredJobCleaner interface {
	DeleteExpiredCollectionGenerationJobs(now time.Time, limit int) (int, error)
}

// NewGenerationRetentionSweep builds the generation cleanup task. Each batch
// runs in a separate transaction so a backlog does not require one large delete.
func NewGenerationRetentionSweep(storage abstractions.Storage, logger *slog.Logger) (func(context.Context) error, error) {
	if _, ok := storage.(ExpiredJobCleaner); !ok {
		return nil, errors.New("storage does not support collection-generation retention cleanup")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, generationRetentionSweepTimeout)
		defer cancel()
		cleaner, ok := storage.WithContext(ctx).(ExpiredJobCleaner)
		if !ok {
			return errors.New("scoped storage does not support collection-generation retention cleanup")
		}
		deleted, err := deleteExpiredGenerationJobs(ctx, cleaner, time.Now().UTC())
		if deleted > 0 {
			logger.Info("Cleaned expired collection-generation jobs", "deleted", deleted)
		}
		if err != nil {
			return fmt.Errorf("clean expired collection-generation jobs: %w", err)
		}
		return nil
	}, nil
}

func deleteExpiredGenerationJobs(ctx context.Context, cleaner ExpiredJobCleaner, cutoff time.Time) (int, error) {
	total := 0
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		deleted, err := cleaner.DeleteExpiredCollectionGenerationJobs(cutoff, generationRetentionBatchSize)
		if err != nil {
			return total, err
		}
		total += deleted
		if deleted < generationRetentionBatchSize {
			return total, nil
		}
	}
}
