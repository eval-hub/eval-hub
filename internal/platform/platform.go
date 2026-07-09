package platform

import (
	"os"
	"strings"
)

func readFile(path string) string {
	content, err := os.ReadFile(path) // #nosec G304 -- platform metadata path from Kubernetes downward API
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
