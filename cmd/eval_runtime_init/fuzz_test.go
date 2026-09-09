package main

import (
	"path/filepath"
	"testing"
)

func FuzzRelativeDestPath(f *testing.F) {
	f.Add("data/", "data/file.csv")
	f.Add("data/", "data/subdir/file.csv")
	f.Add("data", "data/file.csv")
	f.Add("data/", "data/")
	f.Add("data/", "data/../../../etc/passwd")
	f.Add("", "file.csv")
	f.Add("prefix/", "prefix/./valid")
	f.Add("prefix/", "prefix/a/b/../../../escape")
	f.Add("data/", "data/a/b/c/d.txt")
	f.Add("x", "x")

	f.Fuzz(func(t *testing.T, prefix, key string) {
		rel, err := relativeDestPath(prefix, key)
		if err != nil {
			return
		}

		if rel == "" {
			t.Fatalf("relativeDestPath(%q, %q) returned empty string without error", prefix, key)
		}

		fromSlash := filepath.FromSlash(rel)
		if !filepath.IsLocal(fromSlash) {
			t.Fatalf("relativeDestPath(%q, %q) = %q is not local", prefix, key, rel)
		}

		if filepath.IsAbs(fromSlash) {
			t.Fatalf("relativeDestPath(%q, %q) = %q is absolute", prefix, key, rel)
		}
	})
}
