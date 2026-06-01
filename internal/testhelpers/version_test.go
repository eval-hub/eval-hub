package testhelpers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersion(t *testing.T) {
	got := Version(t)
	want, err := RepoVersion()
	if err != nil {
		t.Fatalf("RepoVersion() = %v", err)
	}
	if got != want {
		t.Fatalf("Version() = %q, want %q", got, want)
	}
}

func TestRepoVersion(t *testing.T) {
	root, err := findRepoRoot()
	if err != nil {
		t.Fatalf("findRepoRoot() = %v", err)
	}
	wantBytes, err := os.ReadFile(filepath.Join(root, "VERSION"))
	if err != nil {
		t.Fatalf("read VERSION: %v", err)
	}
	want := strings.TrimSpace(string(wantBytes))

	got, err := RepoVersion()
	if err != nil {
		t.Fatalf("RepoVersion() = %v", err)
	}
	if got != want {
		t.Fatalf("RepoVersion() = %q, want %q", got, want)
	}
}
