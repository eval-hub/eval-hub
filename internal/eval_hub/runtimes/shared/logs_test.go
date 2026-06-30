package shared

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFormatLogSectionHeader(t *testing.T) {
	got := FormatLogSectionHeader("pod-1", "adapter", "bench-a")
	want := "=== pod=pod-1 container=adapter benchmark_id=bench-a ==="
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestTailFileLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jobrun.log")

	t.Run("missing file returns empty", func(t *testing.T) {
		got, err := TailFileLines(filepath.Join(dir, "missing.log"), 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "" {
			t.Fatalf("got %q, want empty", got)
		}
	})

	t.Run("returns all lines when under limit", func(t *testing.T) {
		if err := os.WriteFile(path, []byte("line1\nline2\n"), 0644); err != nil {
			t.Fatalf("write file: %v", err)
		}
		got, err := TailFileLines(path, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "line1\nline2" {
			t.Fatalf("got %q, want %q", got, "line1\nline2")
		}
	})

	t.Run("returns last n lines", func(t *testing.T) {
		if err := os.WriteFile(path, []byte("line1\nline2\nline3\nline4\n"), 0644); err != nil {
			t.Fatalf("write file: %v", err)
		}
		got, err := TailFileLines(path, 2)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "line3\nline4" {
			t.Fatalf("got %q, want %q", got, "line3\nline4")
		}
	})
}
