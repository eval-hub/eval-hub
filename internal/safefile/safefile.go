// Package safefile opens and creates files under a caller-provided trusted root
// via os.Root. Relative names are validated with filepath.IsLocal and resolved
// only beneath that root, so ".." and symlinks (including intermediate
// directories) cannot escape. Prefer this over os.ReadFile/os.Open/os.Create when
// a path is carried in a variable (gosec G304) and over filepath.Clean alone.
package safefile

import (
	"fmt"
	"os"
	"path/filepath"
)

// cleanLocalPath validates and cleans a path that must stay within an OpenRoot directory.
func cleanLocalPath(name string) (string, error) {
	if name == "" || name == "." {
		return "", fmt.Errorf("invalid path %q", name)
	}
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." || !filepath.IsLocal(clean) {
		return "", fmt.Errorf("path %q is not local to root", name)
	}
	return clean, nil
}

// ReadFile reads name relative to trusted rootDir.
func ReadFile(rootDir, name string) ([]byte, error) {
	localName, err := cleanLocalPath(name)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(rootDir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	return root.ReadFile(localName)
}

// Open opens name relative to trusted rootDir for reading.
// The returned file remains valid after the temporary Root is closed.
func Open(rootDir, name string) (*os.File, error) {
	localName, err := cleanLocalPath(name)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(rootDir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	return root.Open(localName)
}

// Create creates or truncates name relative to trusted rootDir.
// The returned file remains valid after the temporary Root is closed.
func Create(rootDir, name string) (*os.File, error) {
	localName, err := cleanLocalPath(name)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(rootDir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	return root.Create(localName)
}
