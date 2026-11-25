package maintenance

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestResourceJanitorRetriesAndStops(t *testing.T) {
	var logs bytes.Buffer
	calls := 0
	retried := make(chan struct{})
	sweepExited := make(chan struct{})
	janitor, err := NewResourceJanitor(context.Background(), time.Millisecond, func(ctx context.Context) error {
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
	if _, err := NewResourceJanitor(context.Background(), time.Minute, nil, nil); err == nil {
		t.Fatal("accepted an absent sweep function")
	}
	if _, err := NewResourceJanitor(context.Background(), 0, func(context.Context) error { return nil }, nil); err == nil {
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
