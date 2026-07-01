package shared

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// FormatLogSectionHeader builds the plain-text section delimiter for concatenated job logs.
func FormatLogSectionHeader(podName, containerName, benchmarkID string) string {
	return fmt.Sprintf("=== pod=%s container=%s benchmark_id=%s ===", podName, containerName, benchmarkID)
}

const tailReadBlockSize = 8192

// TailFileLines returns up to the last n non-empty-terminated lines from a file.
// A missing file yields an empty string without error.
func TailFileLines(path string, n int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if info.Size() == 0 {
		return "", nil
	}

	var content string
	if n <= 0 {
		data, err := io.ReadAll(f)
		if err != nil {
			return "", err
		}
		content = string(data)
	} else {
		content, err = readLastNLines(f, info.Size(), n)
		if err != nil {
			return "", err
		}
	}

	lines := strings.Split(content, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if n > 0 && len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n"), nil
}

func readLastNLines(f *os.File, size int64, n int) (string, error) {
	var (
		offset    = size
		remainder []byte
	)
	for offset > 0 && countLogLines(remainder) <= n {
		readSize := int64(tailReadBlockSize)
		if readSize > offset {
			readSize = offset
		}
		offset -= readSize
		buf := make([]byte, readSize)
		if _, err := f.ReadAt(buf, offset); err != nil {
			return "", err
		}
		combined := make([]byte, 0, len(buf)+len(remainder))
		combined = append(combined, buf...)
		combined = append(combined, remainder...)
		remainder = combined
	}
	return string(remainder), nil
}

func countLogLines(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		return len(lines) - 1
	}
	return len(lines)
}
