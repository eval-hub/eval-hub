package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestVersionFlag(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "--version")
	cmd.Dir = findCmdDir(t)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("--version failed: %v\noutput: %s", err, out)
	}

	output := string(out)
	if !strings.Contains(output, "evalhub-mcp version") {
		t.Errorf("expected version output, got: %s", output)
	}
}

func TestInvalidTransportFlag(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "--transport", "grpc")
	cmd.Dir = findCmdDir(t)

	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected error for invalid transport")
	}

	output := string(out)
	if !strings.Contains(output, "invalid transport") {
		t.Errorf("expected 'invalid transport' in error output, got: %s", output)
	}
}

func findCmdDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getting working directory: %v", err)
	}
	return dir
}
