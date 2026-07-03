package server_test

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_hub/constants"
	"github.com/eval-hub/eval-hub/internal/eval_hub/server"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestGetTerminationFile(t *testing.T) {
	const fallback = "/opt/evalhub/work/termination-log"

	tests := []struct {
		name      string
		conf      *config.Config
		localMode bool
		envVal    string
		want      string
	}{
		{
			name:      "from config",
			conf:      &config.Config{Service: &config.ServiceConfig{TerminationFile: "/custom/termination-log"}},
			localMode: false,
			want:      "/custom/termination-log",
		},
		{
			name:      "config takes precedence over env",
			conf:      &config.Config{Service: &config.ServiceConfig{TerminationFile: "/from-config"}},
			localMode: false,
			envVal:    "/from-env",
			want:      "/from-config",
		},
		{
			name:      "whitespace-only config falls through",
			conf:      &config.Config{Service: &config.ServiceConfig{TerminationFile: "   "}},
			localMode: false,
			want:      fallback,
		},
		{
			name:      "from env var",
			conf:      &config.Config{Service: &config.ServiceConfig{}},
			localMode: false,
			envVal:    "/env/termination-log",
			want:      "/env/termination-log",
		},
		{
			name:      "nil config",
			conf:      nil,
			localMode: false,
			want:      fallback,
		},
		{
			name:      "nil service config",
			conf:      &config.Config{},
			localMode: false,
			want:      fallback,
		},
		{
			name:      "local mode returns empty",
			conf:      &config.Config{Service: &config.ServiceConfig{TerminationFile: "/should-be-ignored"}},
			localMode: true,
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(constants.EnvVarTerminationFile, tt.envVal)
			got := server.GetTerminationFile(tt.conf, tt.localMode, discardLogger())
			if got != tt.want {
				t.Errorf("GetTerminationFile() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSetTerminationMessage_WritesFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "termination-log")

	err := server.SetTerminationMessage(path, "startup failed: boom", discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read termination file: %v", err)
	}
	if string(data) != "startup failed: boom" {
		t.Errorf("expected 'startup failed: boom', got %q", string(data))
	}
}

func TestSetTerminationMessage_OverwritesExisting(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "termination-log")

	if err := os.WriteFile(path, []byte("old message"), 0o644); err != nil {
		t.Fatalf("failed to seed file: %v", err)
	}

	err := server.SetTerminationMessage(path, "new message", discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read termination file: %v", err)
	}
	if string(data) != "new message" {
		t.Errorf("expected 'new message', got %q", string(data))
	}
}

func TestSetTerminationMessage_InvalidPath(t *testing.T) {
	t.Parallel()
	err := server.SetTerminationMessage("/nonexistent/dir/termination-log", "msg", discardLogger())
	if err == nil {
		t.Error("expected error for invalid path, got nil")
	}
}

func TestSetTerminationMessage_EmptyPath(t *testing.T) {
	t.Parallel()
	err := server.SetTerminationMessage("", "msg", discardLogger())
	if err != nil {
		t.Errorf("expected no-op for empty path, got error: %v", err)
	}
}

func TestHandleStartupFailure_WritesTerminationFile(t *testing.T) {
	dir := t.TempDir()
	termFile := filepath.Join(dir, "termination-log")
	conf := &config.Config{Service: &config.ServiceConfig{TerminationFile: termFile}}

	server.HandleStartupFailure(conf, false, errors.New("db connect failed"), "Failed to create storage", discardLogger())

	data, err := os.ReadFile(termFile)
	if err != nil {
		t.Fatalf("failed to read termination file: %v", err)
	}
	want := "Failed to create storage: db connect failed"
	if string(data) != want {
		t.Errorf("got %q, want %q", string(data), want)
	}
}

func TestHandleStartupFailure_LocalModeSkipsFile(t *testing.T) {
	dir := t.TempDir()
	termFile := filepath.Join(dir, "termination-log")
	conf := &config.Config{Service: &config.ServiceConfig{TerminationFile: termFile}}

	server.HandleStartupFailure(conf, true, errors.New("boom"), "Failed", discardLogger())

	if _, err := os.Stat(termFile); !os.IsNotExist(err) {
		t.Error("expected no termination file in local mode")
	}
}

func TestHandleStartupFailure_NilConfig(t *testing.T) {
	t.Setenv(constants.EnvVarTerminationFile, "")
	server.HandleStartupFailure(nil, false, errors.New("boom"), "Failed", discardLogger())
}
