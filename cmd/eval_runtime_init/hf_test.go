package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateHFSubPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		subPath string
		wantErr string
	}{
		{name: "valid nested", subPath: "staging_sub_path"},
		{name: "valid single file", subPath: "data/train.jsonl"},
		{name: "traversal rejected", subPath: "../etc", wantErr: "escapes repository root"},
		{name: "absolute rejected", subPath: "/abs", wantErr: "escapes repository root"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateHFSubPath(tt.subPath)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateHFSubPath() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validateHFSubPath() = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestFileMatchesSubPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		fileName string
		subPath  string
		want     bool
	}{
		{fileName: "staging_sub_path/foo.txt", subPath: "staging_sub_path", want: true},
		{fileName: "staging_sub_path", subPath: "staging_sub_path", want: true},
		{fileName: "other/foo.txt", subPath: "staging_sub_path", want: false},
		{fileName: "anything", subPath: "", want: true},
	}

	for _, tt := range tests {
		got := fileMatchesSubPath(tt.fileName, tt.subPath)
		if got != tt.want {
			t.Fatalf("fileMatchesSubPath(%q, %q) = %v, want %v", tt.fileName, tt.subPath, got, tt.want)
		}
	}
}

func TestClassifyHFError(t *testing.T) {
	t.Parallel()

	gated := classifyHFError("org/repo", errors.New("401 Client Error: gated repo"))
	if !strings.Contains(gated.Error(), "gated") {
		t.Fatalf("expected gated message, got %v", gated)
	}

	gatedOnly := classifyHFError("org/repo", errors.New("gated repo"))
	if !strings.Contains(gatedOnly.Error(), "gated") {
		t.Fatalf("expected gated message, got %v", gatedOnly)
	}

	notFound := classifyHFError("org/repo", errors.New("404 Repository Not Found"))
	if !strings.Contains(notFound.Error(), "not found") {
		t.Fatalf("expected not found message, got %v", notFound)
	}

	raw := errors.New("connection reset by peer")
	if classifyHFError("org/repo", raw) != raw {
		t.Fatalf("expected unclassified error to pass through unchanged")
	}
}

func TestStageHFSubPathAndClearDest(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	snapshot := filepath.Join(tmp, "snap")
	sub := filepath.Join(snapshot, "staging_sub_path")
	if err := os.MkdirAll(sub, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "data.txt"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}

	snapshotRoot, err := os.OpenRoot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = snapshotRoot.Close() }()

	dest := filepath.Join(tmp, "dest")
	if err := stageHFSubPath(snapshotRoot, "staging_sub_path", dest); err != nil {
		t.Fatalf("stageHFSubPath: %v", err)
	}
	if !destHasData(dest) {
		t.Fatal("expected staged data")
	}

	if err := clearDestDir(dest); err != nil {
		t.Fatalf("clearDestDir: %v", err)
	}
	if destHasData(dest) {
		t.Fatal("expected dest cleared")
	}
}

func TestRevisionOrDefault(t *testing.T) {
	t.Parallel()

	if revisionOrDefault("main") != "main" {
		t.Fatal("expected explicit revision")
	}
	if revisionOrDefault("") != "default" {
		t.Fatal("expected default revision label")
	}
}

func TestRunHF_RequiresRepoID(t *testing.T) {
	t.Setenv(envHFRepoID, "")
	err := runHF()
	if err == nil {
		t.Fatal("runHF() = nil, want missing repo id error")
	}
	if !strings.Contains(err.Error(), envHFRepoID) {
		t.Fatalf("runHF() error = %v, want mention of %s", err, envHFRepoID)
	}
}

func TestRunHF_RejectsInvalidSubPath(t *testing.T) {
	t.Setenv(envHFRepoID, "org/repo")
	t.Setenv(envHFSubPath, "../escape")
	err := runHF()
	if err == nil {
		t.Fatal("runHF() = nil, want invalid sub_path error")
	}
	if !strings.Contains(err.Error(), "escapes repository root") {
		t.Fatalf("runHF() error = %v, want sub_path validation error", err)
	}
}

func TestStageHFSubPath_SingleFile(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	snapshot := filepath.Join(tmp, "snap")
	if err := os.MkdirAll(snapshot, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snapshot, "data.jsonl"), []byte("line\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	snapshotRoot, err := os.OpenRoot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = snapshotRoot.Close() }()

	dest := filepath.Join(tmp, "dest")
	if err := stageHFSubPath(snapshotRoot, "data.jsonl", dest); err != nil {
		t.Fatalf("stageHFSubPath: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "data.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "line\n" {
		t.Fatalf("unexpected file content: %q", got)
	}
}

func TestDestHasData(t *testing.T) {
	t.Parallel()

	empty := t.TempDir()
	if destHasData(empty) {
		t.Fatal("expected empty dir to have no data")
	}

	withData := t.TempDir()
	if err := os.WriteFile(filepath.Join(withData, "x"), []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !destHasData(withData) {
		t.Fatal("expected dir with file to have data")
	}
}

func TestWriteTerminationMessage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "termination-log")
	t.Setenv("TERMINATION_MESSAGE_PATH", path)

	writeTerminationMessage("repository org/gated is gated; provide secret_ref")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(content), "gated") {
		t.Fatalf("unexpected content: %q", content)
	}
}

func TestWriteTerminationMessage_EmptySkipped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "termination-log")
	t.Setenv("TERMINATION_MESSAGE_PATH", path)

	writeTerminationMessage("   ")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("expected no file for empty termination message")
	}
}

func TestWriteTerminationMessage_TruncatesLongMessage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "termination-log")
	t.Setenv("TERMINATION_MESSAGE_PATH", path)

	writeTerminationMessage(strings.Repeat("x", maxTerminationMessageBytes+10))
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(content) > maxTerminationMessageBytes+1 {
		t.Fatalf("termination message too long: %d bytes", len(content))
	}
}

func TestWriteTerminationMessageFile_InvalidPath(t *testing.T) {
	t.Setenv("TERMINATION_MESSAGE_PATH", "/tmp/")
	err := writeTerminationMessageFile([]byte("fail"))
	if err == nil {
		t.Fatal("expected error for invalid termination message path")
	}
}
