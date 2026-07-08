package testhelpers

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/validation"
	"github.com/go-playground/validator/v10"
)

func findRepoRoot() (string, error) {
	_, file, _, ok := runtime.Caller(1)
	if !ok {
		return "", fmt.Errorf("runtime.Caller failed")
	}

	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "VERSION")); err == nil {
				return dir, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find repo root (go.mod and VERSION)")
		}
		dir = parent
	}
}

// RepoVersion returns the trimmed contents of the repository VERSION file.
func RepoVersion() (string, error) {
	root, err := findRepoRoot()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(root, "VERSION")) // #nosec G304 -- repo root resolved from test caller location
	if err != nil {
		return "", fmt.Errorf("read VERSION: %w", err)
	}
	version := strings.TrimSpace(string(data))
	if version == "" {
		return "", fmt.Errorf("VERSION file is empty")
	}
	return version, nil
}

// Version returns the trimmed contents of the repository VERSION file.
func Version(t *testing.T) string {
	t.Helper()

	version, err := RepoVersion()
	if err != nil {
		t.Fatal(err)
	}
	return version
}

func NewValidator(t *testing.T) *validator.Validate {
	validate, err := validation.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	return validate
}
