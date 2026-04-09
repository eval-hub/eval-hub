package platform

import "os"

var (
	fipsEnabled = readFile("/proc/sys/crypto/fips_enabled") == "1"
)

func IsFIPS() bool {
	return fipsEnabled
}

func readFile(path string) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(content)
}
