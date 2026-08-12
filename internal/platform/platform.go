package platform

import (
	"path/filepath"
	"strings"

	"github.com/eval-hub/eval-hub/internal/safefile"
)

func readFile(path string) string {
	content, err := safefile.ReadFile(filepath.Dir(path), filepath.Base(path))
	if err != nil {
		return ""
	}
	return string(content)
}

func isFIPSFromPath(path string) bool {
	return strings.TrimSpace(readFile(path)) == "1"
}

var fipsEnabled = isFIPSFromPath("/proc/sys/crypto/fips_enabled")

func IsFIPS() bool {
	return fipsEnabled
}
