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

func TestWrapInitTimeout_Nil(t *testing.T) {
	t.Parallel()
	if got := wrapInitTimeout("git clone", time.Second, nil); got != nil {
		t.Fatalf("wrapInitTimeout nil = %v, want nil", got)
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

func TestWriteTerminationMessageFile_CurrentDirectoryPath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("TERMINATION_MESSAGE_PATH", "./termination.log")

	if err := writeTerminationMessageFile([]byte("init failed")); err != nil {
		t.Fatalf("writeTerminationMessageFile: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "termination.log"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got, want := string(content), "init failed\n"; got != want {
		t.Fatalf("termination message = %q, want %q", got, want)
	}
}

func TestWriteTerminationMessage_DropsWriteFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "termination-log")
	t.Setenv("TERMINATION_MESSAGE_PATH", path)

	writeTerminationMessage("init failed")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected no termination message file after write failure, got stat error %v", err)
	}
}

func TestWriteTerminationMessageFile_RejectsRootPath(t *testing.T) {
	t.Setenv("TERMINATION_MESSAGE_PATH", string(filepath.Separator))

	err := writeTerminationMessageFile([]byte("init failed"))
	if err == nil {
		t.Fatal("writeTerminationMessageFile() = nil, want invalid path error")
	}
	if !strings.Contains(err.Error(), "invalid termination message path") {
		t.Fatalf("writeTerminationMessageFile() = %v, want invalid path error", err)
	}
}
