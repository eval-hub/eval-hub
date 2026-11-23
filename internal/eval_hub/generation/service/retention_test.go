package service

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
)

type fakeCleanerStorage struct {
	abstractions.Storage
	mu        sync.Mutex
	calls     int
	successes int
	now       time.Time
	limit     int
	deleted   int
	err       error
	called    chan struct{}
}

func (s *fakeCleanerStorage) WithContext(context.Context) abstractions.Storage { return s }

func (s *fakeCleanerStorage) DeleteExpiredCollectionGenerationJobs(now time.Time, limit int) (int, error) {
	s.mu.Lock()
	s.calls++
	s.now = now
	s.limit = limit
	deleted, err := s.deleted, s.err
	if err == nil {
		s.successes++
	}
	s.mu.Unlock()
	if s.called != nil {
		select {
		case s.called <- struct{}{}:
		default:
		}
	}
	return deleted, err
}

type unsupportedCleanerStorage struct{ abstractions.Storage }

func (s *unsupportedCleanerStorage) WithContext(context.Context) abstractions.Storage { return s }

func TestNewRetentionJanitorOwnsBackgroundLoopAndStop(t *testing.T) {
	storage := &fakeCleanerStorage{called: make(chan struct{}, 2)}
	cfg := &config.ServiceGenerationConfig{CleanupInterval: time.Second, CleanupBatchSize: 9}
	janitor, err := NewRetentionJanitor(context.Background(), storage, cfg, nil)
	if err != nil {
		t.Fatalf("NewRetentionJanitor(): %v", err)
	}
	select {
	case <-storage.called:
	case <-time.After(time.Second):
		t.Fatal("constructor did not start the initial retention sweep")
	}
	janitor.Stop()
	select {
	case <-janitor.done:
	default:
		t.Fatal("Stop() returned before the background loop exited")
	}
	janitor.Stop() // Stop is idempotent.
}

func TestNewRetentionJanitorRejectsInvalidDependencies(t *testing.T) {
	if _, err := NewRetentionJanitor(context.Background(), nil, nil, nil); err == nil {
		t.Fatal("constructor accepted nil storage")
	}
	if _, err := NewRetentionJanitor(context.Background(), &unsupportedCleanerStorage{}, nil, nil); err == nil {
		t.Fatal("constructor accepted storage without the retention operation")
	}
	invalid := &config.ServiceGenerationConfig{CleanupBatchSize: config.MaxGenerationCleanupBatch + 1}
	if _, err := NewRetentionJanitor(context.Background(), &fakeCleanerStorage{}, invalid, nil); err == nil {
		t.Fatal("constructor accepted invalid cleanup configuration")
	}
}

func TestRetentionJanitorSweepUsesContextTimeAndBound(t *testing.T) {
	fixed := time.Date(2026, 11, 20, 12, 0, 0, 0, time.FixedZone("EST", -5*60*60))
	storage := &fakeCleanerStorage{deleted: 4}
	janitor := &RetentionJanitor{storage: storage, batch: 23, now: func() time.Time { return fixed }}
	deleted, err := janitor.Sweep(context.Background())
	if err != nil || deleted != 4 {
		t.Fatalf("Sweep() = (%d, %v), want (4, nil)", deleted, err)
	}
	storage.mu.Lock()
	defer storage.mu.Unlock()
	if !storage.now.Equal(fixed.UTC()) || storage.limit != 23 {
		t.Fatalf("cleanup arguments = (%s, %d), want (%s, 23)", storage.now, storage.limit, fixed.UTC())
	}
}

func TestRetentionJanitorSweepPropagatesStorageErrors(t *testing.T) {
	wantErr := errors.New("database unavailable")
	janitor := &RetentionJanitor{storage: &fakeCleanerStorage{err: wantErr}, batch: 100, now: time.Now}
	if _, err := janitor.Sweep(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("Sweep() error = %v, want %v", err, wantErr)
	}
	var nilJanitor *RetentionJanitor
	if _, err := nilJanitor.Sweep(context.Background()); err == nil {
		t.Fatal("nil janitor Sweep() succeeded")
	}
}

func TestRetentionJanitorContinuesAfterSweepFailure(t *testing.T) {
	storage := &fakeCleanerStorage{err: errors.New("database unavailable"), called: make(chan struct{}, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	var logs bytes.Buffer
	janitor := &RetentionJanitor{
		storage: storage, batch: 10, now: time.Now,
		logger: slog.New(slog.NewTextHandler(&logs, nil)),
		cancel: cancel, done: make(chan struct{}),
		nextInterval: func() time.Duration { return time.Millisecond },
	}
	go func() {
		defer close(janitor.done)
		janitor.run(ctx)
	}()
	t.Cleanup(janitor.Stop)
	select {
	case <-storage.called:
	case <-time.After(time.Second):
		t.Fatal("initial retention sweep did not run")
	}
	storage.mu.Lock()
	storage.err, storage.deleted = nil, 1
	storage.mu.Unlock()

	deadline := time.After(time.Second)
	for {
		select {
		case <-storage.called:
			storage.mu.Lock()
			successes := storage.successes
			storage.mu.Unlock()
			if successes > 0 {
				janitor.Stop()
				if !strings.Contains(logs.String(), "database unavailable") || !strings.Contains(logs.String(), "deleted=1") {
					t.Fatalf("expected logged failure and successful retry, got %q", logs.String())
				}
				return
			}
		case <-deadline:
			t.Fatal("retention janitor did not retry after the storage error")
		}
	}
}

func TestRetentionJanitorJitterIsBounded(t *testing.T) {
	janitor := &RetentionJanitor{interval: 10 * time.Second}
	for range 100 {
		got := janitor.jitteredInterval()
		if got < 8*time.Second || got > 12*time.Second {
			t.Fatalf("jittered interval %s is outside 8s..12s", got)
		}
	}
}
