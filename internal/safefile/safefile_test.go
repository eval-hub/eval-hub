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
	if err := os.WriteFile(filepath.Join(dir, "data.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := safefile.ReadFile(dir, "data.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("ReadFile = %q, want hello", got)
	}
}

func TestReadFile_Nested(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "data.txt"), []byte("nested"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := safefile.ReadFile(dir, "sub/data.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "nested" {
		t.Fatalf("ReadFile = %q, want nested", got)
	}
}

func TestReadFile_RejectsNonLocal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, name := range []string{"", ".", "/", "..", "../x", "/etc/passwd", "./", "sub/.."} {
		if _, err := safefile.ReadFile(dir, name); err == nil {
			t.Fatalf("ReadFile(%q): expected error", name)
		}
		if _, err := safefile.Open(dir, name); err == nil {
			t.Fatalf("Open(%q): expected error", name)
		}
		if _, err := safefile.Create(dir, name); err == nil {
			t.Fatalf("Create(%q): expected error", name)
		}
	}
}

func TestReadFile_Missing(t *testing.T) {
	t.Parallel()
	_, err := safefile.ReadFile(t.TempDir(), "missing")
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
	f, err := safefile.Create(dir, "out.txt")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := f.WriteString("x"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	rf, err := safefile.Open(dir, "out.txt")
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

func TestOpenAndCreate_RejectsNonLocal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, name := range []string{"", "..", "/etc/passwd"} {
		if _, err := safefile.Open(dir, name); err == nil {
			t.Fatalf("Open(%q): expected error", name)
		}
		if _, err := safefile.Create(dir, name); err == nil {
			t.Fatalf("Create(%q): expected error", name)
		}
	}
}

func TestOpenRootFailures(t *testing.T) {
	t.Parallel()
	missingRoot := filepath.Join(t.TempDir(), "does-not-exist")
	if _, err := safefile.ReadFile(missingRoot, "x.txt"); err == nil {
		t.Fatal("ReadFile: expected OpenRoot error")
	}
	if _, err := safefile.Open(missingRoot, "x.txt"); err == nil {
		t.Fatal("Open: expected OpenRoot error")
	}
	if _, err := safefile.Create(missingRoot, "x.txt"); err == nil {
		t.Fatal("Create: expected OpenRoot error")
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
	_, err := safefile.ReadFile(dir, "link.txt")
	if err == nil {
		t.Fatal("expected symlink escape to fail")
	}
}

func TestReadFile_SymlinkedIntermediateDir(t *testing.T) {
	t.Parallel()
	trusted := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(trusted, "via")); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	// Escape via a symlinked intermediate directory must fail.
	if _, err := safefile.ReadFile(trusted, "via/secret.txt"); err == nil {
		t.Fatal("ReadFile through symlinked intermediate dir: expected error")
	}
	if _, err := safefile.Open(trusted, "via/secret.txt"); err == nil {
		t.Fatal("Open through symlinked intermediate dir: expected error")
	}
	if _, err := safefile.Create(trusted, "via/out.txt"); err == nil {
		t.Fatal("Create through symlinked intermediate dir: expected error")
	}

	// A real nested path under the trusted root still works.
	if err := os.MkdirAll(filepath.Join(trusted, "real"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(trusted, "real", "ok.txt"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := safefile.ReadFile(trusted, "real/ok.txt")
	if err != nil {
		t.Fatalf("ReadFile nested under trusted root: %v", err)
	}
	if string(got) != "ok" {
		t.Fatalf("ReadFile = %q, want ok", got)
	}
}
