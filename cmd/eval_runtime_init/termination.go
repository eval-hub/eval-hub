package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	terminationMessagePath     = "/dev/termination-log"
	maxTerminationMessageBytes = 4096
)

// wrapInitTimeout annotates context deadline errors with the configured timeout
// so the termination message is specific (git clone / S3 download / HF download).
func wrapInitTimeout(op string, timeout time.Duration, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s timed out after %s: %w", op, timeout, err)
	}
	return err
}

// formatInitFailure returns the termination-log text for an init failure.
// Context deadlines become an explicit timeout message so kube / eval-hub do not
// surface an empty message= when the init container hits TEST_DATA_*_TIMEOUT.
func formatInitFailure(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Sprintf("eval-runtime-init timed out: %v", err)
	}
	return err.Error()
}

// reportInitFailure writes the failure into the Kubernetes termination message file.
func reportInitFailure(err error) {
	writeTerminationMessage(formatInitFailure(err))
}

func writeTerminationMessage(message string) {
	text := strings.TrimSpace(message)
	if text == "" {
		return
	}
	encoded := []byte(text)
	if len(encoded) > maxTerminationMessageBytes {
		encoded = encoded[:maxTerminationMessageBytes]
	}
	if writeErr := writeTerminationMessageFile(encoded); writeErr != nil {
		slog.Warn("failed to write termination message", "error", writeErr)
	}
}

// writeTerminationMessageFile writes via os.Root so TERMINATION_MESSAGE_PATH cannot escape its directory.
func writeTerminationMessageFile(content []byte) error {
	path := strings.TrimSpace(os.Getenv("TERMINATION_MESSAGE_PATH"))
	if path == "" {
		path = terminationMessagePath
	}
	clean := filepath.Clean(path)
	dir, name := filepath.Split(clean)
	if name == "" || name == "." || name == ".." {
		return fmt.Errorf("invalid termination message path %q", path)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	return root.WriteFile(name, append(content, '\n'), 0o600)
}
