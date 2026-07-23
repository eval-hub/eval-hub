package main

import (
	"bytes"
	"os"
	"testing"
)

func TestHelpFlag(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	code := run([]string{"--help"})

	_ = w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	output := buf.String()

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if !bytes.Contains([]byte(output), []byte("Usage: evalhub-mcp")) {
		t.Errorf("expected usage output, got: %s", output)
	}
	if !bytes.Contains([]byte(output), []byte("--transport")) {
		t.Errorf("expected flag list in output, got: %s", output)
	}
}

func TestVersionFlag(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	code := run([]string{"--version"})

	_ = w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	output := buf.String()

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if !bytes.Contains([]byte(output), []byte("evalhub-mcp version")) {
		t.Errorf("expected version output, got: %s", output)
	}
}

func TestVersionFlagWithBuildInfo(t *testing.T) {
	origBuild, origDate, origHash := Build, BuildDate, GitHash
	Build = "abc123"
	BuildDate = "2026-01-01"
	GitHash = "def456"
	t.Cleanup(func() {
		Build = origBuild
		BuildDate = origDate
		GitHash = origHash
	})

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	code := run([]string{"--version"})

	_ = w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	output := buf.String()

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if !bytes.Contains([]byte(output), []byte("version abc123")) {
		t.Errorf("expected build info in output, got: %s", output)
	}
	if !bytes.Contains([]byte(output), []byte("commit: def456")) {
		t.Errorf("expected git hash in output, got: %s", output)
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

func TestInvalidAuthTypeFlag(t *testing.T) {
	code := run([]string{"--auth-type", "standalone"})
	if code != 1 {
		t.Fatalf("expected exit code 1 for invalid auth-type, got %d", code)
	}
}

func TestConfigLoadError(t *testing.T) {
	code := run([]string{"--config", "/nonexistent/config.yaml"})
	if code != 1 {
		t.Fatalf("expected exit code 1 for missing config, got %d", code)
	}
}
