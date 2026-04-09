package platform_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eval-hub/eval-hub/internal/platform"
)

func TestReadFile(t *testing.T) {
	t.Parallel()

	t.Run("missing file returns empty", func(t *testing.T) {
		t.Parallel()
		p := filepath.Join(t.TempDir(), "does-not-exist")
		if got := platform.ReadFile(p); got != "" {
			t.Errorf("ReadFile(%q) = %q, want empty string", p, got)
		}
	})

	t.Run("returns file contents", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		p := filepath.Join(dir, "content.txt")
		if err := os.WriteFile(p, []byte("hello"), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := platform.ReadFile(p); got != "hello" {
			t.Errorf("ReadFile(%q) = %q, want %q", p, got, "hello")
		}
	})
}

func TestIsFIPSFromPath(t *testing.T) {
	t.Parallel()

	// Must match the path used for package init in platform.go.
	const fipsProcPath = "/proc/sys/crypto/fips_enabled"

	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{name: "one", content: "1", want: true},
		{name: "one with newline", content: "1\n", want: true},
		{name: "one with windows newline", content: "1\r\n", want: true},
		{name: "zero", content: "0", want: false},
		{name: "empty file", content: "", want: false},
		{name: "other text", content: "yes", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			p := filepath.Join(dir, "fips_flag")
			if err := os.WriteFile(p, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			if got := platform.IsFIPSFromPath(p); got != tc.want {
				t.Errorf("IsFIPSFromPath(%q) = %v, want %v", p, got, tc.want)
			}
		})
	}

	t.Run("missing file is not FIPS", func(t *testing.T) {
		t.Parallel()
		p := filepath.Join(t.TempDir(), "missing")
		if platform.IsFIPSFromPath(p) {
			t.Error("IsFIPSFromPath(missing file) = true, want false")
		}
	})

	t.Run("matches kernel proc path at init", func(t *testing.T) {
		t.Parallel()
		want := platform.IsFIPSFromPath(fipsProcPath)
		if got := platform.IsFIPS(); got != want {
			t.Errorf("IsFIPS() = %v, IsFIPSFromPath(%q) = %v — they must agree", got, fipsProcPath, want)
		}
	})
}
