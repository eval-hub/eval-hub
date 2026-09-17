package handlers

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestLimitedWriterUnlimited(t *testing.T) {
	var buf bytes.Buffer
	lw := &LimitedWriter{W: &buf, Limit: -1}
	data := strings.Repeat("x", 1000)
	n, err := lw.Write([]byte(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 1000 {
		t.Fatalf("wrote %d, want 1000", n)
	}
	if buf.Len() != 1000 {
		t.Fatalf("buf len = %d, want 1000", buf.Len())
	}
}

func TestLimitedWriterTruncates(t *testing.T) {
	var buf bytes.Buffer
	lw := &LimitedWriter{W: &buf, Limit: 10}
	n, err := lw.Write([]byte("hello world!"))
	if !errors.Is(err, ErrLogResponseTruncated) {
		t.Fatalf("err = %v, want ErrLogResponseTruncated", err)
	}
	if n != 10 {
		t.Fatalf("wrote %d, want 10", n)
	}
	if buf.String() != "hello worl" {
		t.Fatalf("buf = %q, want %q", buf.String(), "hello worl")
	}
}

func TestLimitedWriterExactLimit(t *testing.T) {
	var buf bytes.Buffer
	lw := &LimitedWriter{W: &buf, Limit: 5}
	n, err := lw.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 5 {
		t.Fatalf("wrote %d, want 5", n)
	}
	// next write should be truncated
	n, err = lw.Write([]byte("x"))
	if !errors.Is(err, ErrLogResponseTruncated) {
		t.Fatalf("err = %v, want ErrLogResponseTruncated", err)
	}
	if n != 0 {
		t.Fatalf("wrote %d, want 0", n)
	}
}

func TestLimitedWriterMultipleWrites(t *testing.T) {
	var buf bytes.Buffer
	lw := &LimitedWriter{W: &buf, Limit: 15}

	n, err := lw.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("write 1: %v", err)
	}
	if n != 5 {
		t.Fatalf("write 1: wrote %d, want 5", n)
	}

	n, err = lw.Write([]byte(" world"))
	if err != nil {
		t.Fatalf("write 2: %v", err)
	}
	if n != 6 {
		t.Fatalf("write 2: wrote %d, want 6", n)
	}

	// This write exceeds the remaining limit (4 bytes left)
	n, err = lw.Write([]byte("truncated"))
	if !errors.Is(err, ErrLogResponseTruncated) {
		t.Fatalf("write 3: err = %v, want ErrLogResponseTruncated", err)
	}
	if n != 4 {
		t.Fatalf("write 3: wrote %d, want 4", n)
	}
	if buf.String() != "hello worldtrun" {
		t.Fatalf("buf = %q, want %q", buf.String(), "hello worldtrun")
	}
}

type failAfterNWriter struct {
	n       int
	written int
}

func (w *failAfterNWriter) Write(p []byte) (int, error) {
	if w.written+len(p) > w.n {
		allowed := w.n - w.written
		w.written = w.n
		return allowed, errors.New("disk full")
	}
	w.written += len(p)
	return len(p), nil
}

func TestLimitedWriterUnderlyingWriteError(t *testing.T) {
	fw := &failAfterNWriter{n: 3}
	lw := &LimitedWriter{W: fw, Limit: 10}
	n, err := lw.Write([]byte("hello"))
	if err == nil {
		t.Fatal("expected error from underlying writer")
	}
	if errors.Is(err, ErrLogResponseTruncated) {
		t.Fatal("expected underlying writer error, not ErrLogResponseTruncated")
	}
	if n != 3 {
		t.Fatalf("wrote %d, want 3", n)
	}
}

func TestLimitedWriterPartialWriteUnderlyingError(t *testing.T) {
	fw := &failAfterNWriter{n: 2}
	lw := &LimitedWriter{W: fw, Limit: 5}

	n, err := lw.Write([]byte("abcde"))
	if err == nil {
		t.Fatal("expected error from underlying writer")
	}
	if errors.Is(err, ErrLogResponseTruncated) {
		t.Fatal("expected underlying writer error, not ErrLogResponseTruncated")
	}
	if n != 2 {
		t.Fatalf("wrote %d, want 2", n)
	}
}
