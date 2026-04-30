package main

import (
	"bytes"
	"os"
	"testing"
)

func TestVersionFlag(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	code := run([]string{"--version"})

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if !bytes.Contains([]byte(output), []byte("evalhub-mcp version")) {
		t.Errorf("expected version output, got: %s", output)
	}
}

func TestVersionFlagWithBuildInfo(t *testing.T) {
	origBuild, origDate := Build, BuildDate
	Build = "abc123"
	BuildDate = "2026-01-01"
	t.Cleanup(func() {
		Build = origBuild
		BuildDate = origDate
	})

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	code := run([]string{"--version"})

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if !bytes.Contains([]byte(output), []byte("build: abc123")) {
		t.Errorf("expected build info in output, got: %s", output)
	}
	if !bytes.Contains([]byte(output), []byte("built: 2026-01-01")) {
		t.Errorf("expected build date in output, got: %s", output)
	}
}

func TestInvalidFlag(t *testing.T) {
	code := run([]string{"--nonexistent"})
	if code != 1 {
		t.Fatalf("expected exit code 1 for invalid flag, got %d", code)
	}
}

func TestInvalidTransportFlag(t *testing.T) {
	code := run([]string{"--transport", "grpc"})
	if code != 1 {
		t.Fatalf("expected exit code 1 for invalid transport, got %d", code)
	}
}

func TestConfigLoadError(t *testing.T) {
	code := run([]string{"--config", "/nonexistent/config.yaml"})
	if code != 1 {
		t.Fatalf("expected exit code 1 for missing config, got %d", code)
	}
}
