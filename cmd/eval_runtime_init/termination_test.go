package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFormatInitFailure_Timeout(t *testing.T) {
	t.Parallel()
	err := wrapInitTimeout("git clone", 5*time.Second, context.DeadlineExceeded)
	got := formatInitFailure(err)
	if !strings.Contains(got, "timed out") {
		t.Fatalf("formatInitFailure(%v) = %q, want timeout wording", err, got)
	}
	if !strings.Contains(got, "git clone") {
		t.Fatalf("formatInitFailure(%v) = %q, want operation name", err, got)
	}
}

func TestFormatInitFailure_NonTimeout(t *testing.T) {
	t.Parallel()
	err := errors.New("bucket missing")
	if got := formatInitFailure(err); got != "bucket missing" {
		t.Fatalf("formatInitFailure = %q, want raw error", got)
	}
}

func TestFormatInitFailure_Nil(t *testing.T) {
	t.Parallel()
	if got := formatInitFailure(nil); got != "" {
		t.Fatalf("formatInitFailure(nil) = %q, want empty", got)
	}
}

func TestWrapInitTimeout_Passthrough(t *testing.T) {
	t.Parallel()
	base := errors.New("clone failed")
	if got := wrapInitTimeout("git clone", time.Second, base); !errors.Is(got, base) || got.Error() != base.Error() {
		t.Fatalf("wrapInitTimeout passthrough = %v, want %v", got, base)
	}
}

func TestReportInitFailure_WritesTimeoutMessage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "termination-log")
	t.Setenv("TERMINATION_MESSAGE_PATH", path)

	reportInitFailure(wrapInitTimeout("s3 download", time.Second, fmt.Errorf("list objects: %w", context.DeadlineExceeded)))
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, "timed out") {
		t.Fatalf("termination message missing timeout text: %q", text)
	}
	if !strings.Contains(text, "s3 download") {
		t.Fatalf("termination message missing operation: %q", text)
	}
}
