// Package safefile opens and creates files confined to their parent directory
// via os.Root, so path components cannot escape that directory (including via
// symlinks). Prefer this over os.ReadFile/os.Open/os.Create when the path is
// carried in a variable (gosec G304) and over filepath.Clean alone.
package safefile

import (
	"fmt"
	"os"
	"path/filepath"
)

// split returns the parent directory and a single local path segment for path.
func split(path string) (dir, name string, err error) {
	clean := filepath.Clean(path)
	if clean == "" || clean == "." {
		return "", "", fmt.Errorf("invalid path %q", path)
	}
	dir = filepath.Dir(clean)
	name = filepath.Base(clean)
	if name == "." || name == string(filepath.Separator) || !filepath.IsLocal(name) {
		return "", "", fmt.Errorf("invalid path %q", path)
	}
	return dir, name, nil
}

// ReadFile reads the named file, confined to filepath.Dir(path).
func ReadFile(path string) ([]byte, error) {
	dir, name, err := split(path)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	return root.ReadFile(name)
}

// Open opens the named file for reading, confined to filepath.Dir(path).
// The returned file remains valid after the temporary Root is closed.
func Open(path string) (*os.File, error) {
	dir, name, err := split(path)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	return root.Open(name)
}

// Create creates or truncates the named file, confined to filepath.Dir(path).
// The returned file remains valid after the temporary Root is closed.
func Create(path string) (*os.File, error) {
	dir, name, err := split(path)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	return root.Create(name)
}
