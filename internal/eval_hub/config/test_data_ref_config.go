package config

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// DefaultTestDataRefDownloadTimeout is applied when service.test_data_ref.download_timeout is omitted or zero.
const DefaultTestDataRefDownloadTimeout = 10 * time.Minute

// DefaultHFHubEndpoint is the default Hugging Face Hub API URL for HF test-data init containers.
const DefaultHFHubEndpoint = "https://huggingface.co"

// TestDataRefServiceConfig holds cluster-wide defaults for test_data_ref init containers.
type TestDataRefServiceConfig struct {
	// DownloadTimeout applies to S3, git, and HF init containers.
	DownloadTimeout time.Duration               `mapstructure:"download_timeout,omitempty"`
	HF              *HFTestDataRefServiceConfig `mapstructure:"hf,omitempty"`
}

// HFTestDataRefServiceConfig holds Hugging Face Hub defaults for test_data_ref.hf jobs.
type HFTestDataRefServiceConfig struct {
	Endpoint string `mapstructure:"endpoint,omitempty"`
}

// EffectiveDownloadTimeout returns the shared test-data download timeout.
func (c *TestDataRefServiceConfig) EffectiveDownloadTimeout() time.Duration {
	if c == nil || c.DownloadTimeout <= 0 {
		return DefaultTestDataRefDownloadTimeout
	}
	return c.DownloadTimeout
}

// EffectiveHFEndpoint returns the Hugging Face Hub API base URL.
func (c *TestDataRefServiceConfig) EffectiveHFEndpoint() string {
	if c == nil || c.HF == nil {
		return DefaultHFHubEndpoint
	}
	endpoint := strings.TrimSpace(c.HF.Endpoint)
	if endpoint == "" {
		return DefaultHFHubEndpoint
	}
	return endpoint
}

// Validate returns an error when test_data_ref settings are invalid.
func (c *TestDataRefServiceConfig) Validate() error {
	if c == nil {
		return nil
	}
	if c.DownloadTimeout < 0 {
		return fmt.Errorf("service.test_data_ref.download_timeout must not be negative")
	}
	if c.HF != nil {
		if err := validateHFHubEndpoint(c.HF.Endpoint); err != nil {
			return err
		}
	}
	return nil
}

func validateHFHubEndpoint(endpoint string) error {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return nil
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("service.test_data_ref.hf.endpoint is invalid: %w", err)
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("service.test_data_ref.hf.endpoint must use https scheme")
	}
	if parsed.Hostname() == "" {
		return fmt.Errorf("service.test_data_ref.hf.endpoint must include a hostname")
	}
	return nil
}
