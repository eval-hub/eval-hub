package safefile_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eval-hub/eval-hub/internal/safefile"
)

func TestReadFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "data.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := safefile.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("ReadFile = %q, want hello", got)
	}
}

func TestReadFile_RejectsEmptyAndDot(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"", ".", "/"} {
		if _, err := safefile.ReadFile(path); err == nil {
			t.Fatalf("ReadFile(%q): expected error", path)
		}
	}
}

func TestReadFile_Missing(t *testing.T) {
	t.Parallel()
	_, err := safefile.ReadFile(filepath.Join(t.TempDir(), "missing"))
	if err == nil {
		t.Fatal("expected error")
	}
	if !os.IsNotExist(err) {
		t.Fatalf("got %v, want IsNotExist", err)
	}
}

func TestOpenAndCreate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	f, err := safefile.Create(path)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := f.WriteString("x"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	rf, err := safefile.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = rf.Close() }()
	buf := make([]byte, 1)
	n, err := rf.Read(buf)
	if err != nil || n != 1 || buf[0] != 'x' {
		t.Fatalf("Read = %q n=%d err=%v", buf[:n], n, err)
	}
}

func TestReadFile_SymlinkEscape(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	_, err := safefile.ReadFile(link)
	if err == nil {
		t.Fatal("expected symlink escape to fail")
	}
}
