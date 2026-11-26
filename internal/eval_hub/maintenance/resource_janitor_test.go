package maintenance

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
)

type retentionTestStorage struct {
	abstractions.Storage
	called chan struct{}
}

func (s *retentionTestStorage) WithContext(context.Context) abstractions.Storage { return s }

func (s *retentionTestStorage) DeleteExpiredCollectionGenerationJobs(time.Time, int) (int, error) {
	close(s.called)
	return 0, nil
}

func TestStartResourceJanitorWiresGenerationCleanup(t *testing.T) {
	storage := &retentionTestStorage{called: make(chan struct{})}
	janitor, err := StartResourceJanitor(context.Background(), storage, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(janitor.Stop)
	if janitor.interval != config.DefaultResourceJanitorInterval {
		t.Fatalf("interval = %s, want default", janitor.interval)
	}
	select {
	case <-storage.called:
	case <-time.After(time.Second):
		t.Fatal("startup sweep did not invoke generation cleanup")
	}
}

func TestResourceJanitorRetriesAndStops(t *testing.T) {
	var logs bytes.Buffer
	calls := 0
	retried := make(chan struct{})
	sweepExited := make(chan struct{})
	janitor, err := newResourceJanitor(context.Background(), time.Millisecond, func(ctx context.Context) error {
		calls++
		if calls == 1 {
			return errors.New("temporary cleanup failure")
		}
		close(retried)
		<-ctx.Done()
		close(sweepExited)
		return ctx.Err()
	}, slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(janitor.Stop)
	select {
	case <-retried:
	case <-time.After(time.Second):
		t.Fatal("expected a retry after the first sweep failed")
	}
	janitor.Stop()
	select {
	case <-sweepExited:
	default:
		t.Fatal("Stop returned before the in-flight sweep exited")
	}
	if !strings.Contains(logs.String(), "temporary cleanup failure") {
		t.Fatal("initial sweep failure was not logged")
	}
}

func TestResourceJanitorRejectsInvalidConfiguration(t *testing.T) {
	if _, err := newResourceJanitor(context.Background(), time.Minute, nil, nil); err == nil {
		t.Fatal("accepted an absent sweep function")
	}
	if _, err := newResourceJanitor(context.Background(), 0, func(context.Context) error { return nil }, nil); err == nil {
		t.Fatal("accepted a zero interval")
	}
}

func TestResourceJanitorJitterIsBounded(t *testing.T) {
	janitor := &ResourceJanitor{interval: 10 * time.Second}
	for range 100 {
		got := janitor.jitteredInterval()
		if got < 8*time.Second || got > 12*time.Second {
			t.Fatalf("jittered interval %s is outside 8s..12s", got)
		}
	}
}
