package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
)

type generationRetentionStorage struct {
	abstractions.Storage
	ctx     context.Context
	cutoffs []time.Time
	counts  []int
	err     error
}

func (s *generationRetentionStorage) WithContext(ctx context.Context) abstractions.Storage {
	s.ctx = ctx
	return s
}

func (s *generationRetentionStorage) DeleteExpiredCollectionGenerationJobs(cutoff time.Time, limit int) (int, error) {
	if limit != generationRetentionBatchSize {
		return 0, errors.New("unexpected batch size")
	}
	if s.err != nil {
		return 0, s.err
	}
	s.cutoffs = append(s.cutoffs, cutoff)
	if len(s.counts) == 0 {
		return 0, errors.New("sweep continued after a partial batch")
	}
	count := s.counts[0]
	s.counts = s.counts[1:]
	return count, nil
}

func TestGenerationRetentionSweepDrainsBatches(t *testing.T) {
	storage := &generationRetentionStorage{counts: []int{100, 100, 5}}
	sweep, err := NewGenerationRetentionSweep(storage, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := storage.ctx.Deadline(); !ok {
		t.Fatal("cleanup queries are missing a sweep deadline")
	}
	if len(storage.cutoffs) != 3 || !storage.cutoffs[0].Equal(storage.cutoffs[1]) || !storage.cutoffs[1].Equal(storage.cutoffs[2]) {
		t.Fatalf("batches used different cutoffs: %v", storage.cutoffs)
	}
}

func TestGenerationRetentionSweepStopsOnCancellationOrError(t *testing.T) {
	if _, err := NewGenerationRetentionSweep(nil, nil); err == nil {
		t.Fatal("accepted storage without a generation cleanup operation")
	}
	wantErr := errors.New("database unavailable")
	storage := &generationRetentionStorage{err: wantErr}
	sweep, err := NewGenerationRetentionSweep(storage, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := sweep(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("sweep error = %v, want %v", err, wantErr)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sweep(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled sweep = %v, want context.Canceled", err)
	}
}
