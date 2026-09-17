package safefile

import (
	"path/filepath"
	"testing"
)

func FuzzCleanLocalPath(f *testing.F) {
	f.Add("file.txt")
	f.Add("subdir/file.txt")
	f.Add("../escape")
	f.Add("../../etc/passwd")
	f.Add("./valid")
	f.Add(".")
	f.Add("")
	f.Add("/absolute/path")
	f.Add("subdir/../escape")
	f.Add("a/b/../../../c")
	f.Add("valid/./path")
	f.Add("a\\b\\c")
	f.Add("..\\..\\windows")
	f.Add("\x00null")
	f.Add("a/b/c/d/e/f/g/h/i/j")

	f.Fuzz(func(t *testing.T, name string) {
		result, err := cleanLocalPath(name)

		if name == "" || name == "." {
			if err == nil {
				t.Fatalf("expected error for %q, got result %q", name, result)
			}
			return
		}

		if err != nil {
			return
		}

		if result == "." {
			t.Fatalf("cleanLocalPath(%q) returned '.'", name)
		}

		if !filepath.IsLocal(result) {
			t.Fatalf("cleanLocalPath(%q) = %q is not local", name, result)
		}

		if filepath.IsAbs(result) {
			t.Fatalf("cleanLocalPath(%q) = %q is absolute", name, result)
		}
	})
}
