package shared

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

	t.Run("returns all lines when n is zero", func(t *testing.T) {
		if err := os.WriteFile(path, []byte("line1\nline2\nline3\n"), 0644); err != nil {
			t.Fatalf("write file: %v", err)
		}
		got, err := TailFileLines(path, 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "line1\nline2\nline3" {
			t.Fatalf("got %q, want all lines", got)
		}
	})

	t.Run("empty file returns empty", func(t *testing.T) {
		emptyPath := filepath.Join(dir, "empty.log")
		if err := os.WriteFile(emptyPath, nil, 0644); err != nil {
			t.Fatalf("write file: %v", err)
		}
		got, err := TailFileLines(emptyPath, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "" {
			t.Fatalf("got %q, want empty", got)
		}
	})

	t.Run("returns last n lines from large file", func(t *testing.T) {
		largePath := filepath.Join(dir, "large.log")
		var b strings.Builder
		for i := 1; i <= 1000; i++ {
			b.WriteString("line")
			b.WriteString(strconv.Itoa(i))
			b.WriteByte('\n')
		}
		if err := os.WriteFile(largePath, []byte(b.String()), 0644); err != nil {
			t.Fatalf("write file: %v", err)
		}
		got, err := TailFileLines(largePath, 3)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "line998\nline999\nline1000" {
			t.Fatalf("got %q, want last 3 lines", got)
		}
	})
}

func TestStreamFileAll(t *testing.T) {
	dir := t.TempDir()

	t.Run("streams existing file content", func(t *testing.T) {
		path := filepath.Join(dir, "existing.log")
		content := "line1\nline2\nline3\n"
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("write file: %v", err)
		}
		var buf bytes.Buffer
		if err := StreamFileAll(path, &buf); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if buf.String() != content {
			t.Fatalf("got %q, want %q", buf.String(), content)
		}
	})

	t.Run("missing file writes nothing", func(t *testing.T) {
		var buf bytes.Buffer
		err := StreamFileAll(filepath.Join(dir, "missing.log"), &buf)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if buf.Len() != 0 {
			t.Fatalf("got %q, want empty", buf.String())
		}
	})

	t.Run("empty file writes nothing", func(t *testing.T) {
		path := filepath.Join(dir, "empty.log")
		if err := os.WriteFile(path, nil, 0644); err != nil {
			t.Fatalf("write file: %v", err)
		}
		var buf bytes.Buffer
		if err := StreamFileAll(path, &buf); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if buf.Len() != 0 {
			t.Fatalf("got %q, want empty", buf.String())
		}
	})
}
