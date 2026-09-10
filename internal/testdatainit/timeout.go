// Package testdatainit holds helpers shared by test-data init container binaries.
package testdatainit

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// EnvDownloadTimeout is the unified timeout env var set by the Kubernetes job builder
// for S3, git, and HF test-data init containers.
const EnvDownloadTimeout = "TEST_DATA_DOWNLOAD_TIMEOUT"

// DefaultDownloadTimeout matches the service default (service.test_data_ref.download_timeout).
const DefaultDownloadTimeout = 10 * time.Minute

// ParseDownloadTimeout reads TEST_DATA_DOWNLOAD_TIMEOUT when set, otherwise sourceEnv
// (e.g. TEST_DATA_S3_TIMEOUT), otherwise DefaultDownloadTimeout.
func ParseDownloadTimeout(sourceEnv string) (time.Duration, error) {
	if raw := strings.TrimSpace(os.Getenv(EnvDownloadTimeout)); raw != "" {
		return parsePositiveDuration(EnvDownloadTimeout, raw)
	}
	if sourceEnv != "" {
		if raw := strings.TrimSpace(os.Getenv(sourceEnv)); raw != "" {
			return parsePositiveDuration(sourceEnv, raw)
		}
	}
	return DefaultDownloadTimeout, nil
}

func parsePositiveDuration(name, raw string) (time.Duration, error) {
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", name, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("invalid %s: must be a positive duration", name)
	}
	return parsed, nil
}
