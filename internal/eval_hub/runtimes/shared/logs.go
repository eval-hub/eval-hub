package shared

import (
	"fmt"
	"os"
	"strings"
)

// FormatLogSectionHeader builds the plain-text section delimiter for concatenated job logs.
func FormatLogSectionHeader(podName, containerName, benchmarkID string) string {
	return fmt.Sprintf("=== pod=%s container=%s benchmark_id=%s ===", podName, containerName, benchmarkID)
}

// TailFileLines returns up to the last n non-empty-terminated lines from a file.
// A missing file yields an empty string without error.
func TailFileLines(path string, n int) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	if len(data) == 0 {
		return "", nil
	}
	content := string(data)
	lines := strings.Split(content, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if n > 0 && len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n"), nil
}
