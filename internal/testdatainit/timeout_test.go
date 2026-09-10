package testdatainit

import (
	"testing"
	"time"
)

func TestParseDownloadTimeout(t *testing.T) {
	t.Setenv(EnvDownloadTimeout, "")
	t.Setenv("TEST_DATA_S3_TIMEOUT", "")

	got, err := ParseDownloadTimeout("TEST_DATA_S3_TIMEOUT")
	if err != nil {
		t.Fatalf("ParseDownloadTimeout() error = %v", err)
	}
	if got != DefaultDownloadTimeout {
		t.Fatalf("got %v, want default %v", got, DefaultDownloadTimeout)
	}

	t.Setenv(EnvDownloadTimeout, "5m")
	got, err = ParseDownloadTimeout("TEST_DATA_S3_TIMEOUT")
	if err != nil {
		t.Fatalf("ParseDownloadTimeout() error = %v", err)
	}
	if got != 5*time.Minute {
		t.Fatalf("got %v, want 5m", got)
	}

	t.Setenv(EnvDownloadTimeout, "")
	t.Setenv("TEST_DATA_S3_TIMEOUT", "2m")
	got, err = ParseDownloadTimeout("TEST_DATA_S3_TIMEOUT")
	if err != nil {
		t.Fatalf("ParseDownloadTimeout() error = %v", err)
	}
	if got != 2*time.Minute {
		t.Fatalf("got %v, want 2m", got)
	}
}

func TestParseDownloadTimeoutRejectsInvalid(t *testing.T) {
	t.Setenv(EnvDownloadTimeout, "nope")
	_, err := ParseDownloadTimeout("")
	if err == nil {
		t.Fatal("expected error for invalid duration")
	}
}
