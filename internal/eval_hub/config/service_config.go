package config

import (
	"fmt"
	"net/http"
	"time"
)

// DefaultMaxRequestBodyBytes is applied when service.max_request_body_bytes is omitted or zero.
const DefaultMaxRequestBodyBytes int64 = 10 << 20 // 10 MiB

// DefaultMaxLogResponseBytes is applied when service.max_log_response_bytes is omitted or zero.
const DefaultMaxLogResponseBytes int64 = 50 << 20 // 50 MiB

// DefaultLogStreamTimeout is applied when service.log_stream_timeout is omitted or zero.
const DefaultLogStreamTimeout = 5 * time.Minute

type ServiceConfig struct {
	Version         string `mapstructure:"version,omitempty"`
	Build           string `mapstructure:"build,omitempty"`
	BuildDate       string `mapstructure:"build_date,omitempty"`
	GitHash         string `mapstructure:"git_hash,omitempty"`
	Port            int    `mapstructure:"port,omitempty"`
	Host            string `mapstructure:"host,omitempty"`
	TerminationFile string `mapstructure:"termination_file"`
	EvalInitImage   string `mapstructure:"eval_init_image,omitempty"`
	LocalMode       bool   `mapstructure:"local_mode,omitempty"`
	TLSCertFile     string `mapstructure:"tls_cert_file,omitempty"`
	TLSKeyFile      string `mapstructure:"tls_key_file,omitempty"`
	// ReadTimeout is http.Server ReadTimeout (entire request read). Zero uses default (15s).
	ReadTimeout time.Duration `mapstructure:"read_timeout,omitempty"`
	// WriteTimeout is http.Server WriteTimeout. Zero uses default (15s).
	WriteTimeout time.Duration `mapstructure:"write_timeout,omitempty"`
	// IdleTimeout is http.Server IdleTimeout. Zero uses default (60s).
	IdleTimeout time.Duration `mapstructure:"idle_timeout,omitempty"`
	// ReadHeaderTimeout is the HTTP server ReadHeaderTimeout (time to read request headers).
	// Zero means use the default (15s), matching the server read timeout.
	ReadHeaderTimeout time.Duration `mapstructure:"read_header_timeout,omitempty"`
	// MaxHeaderBytes is http.Server MaxHeaderBytes (bytes allowed for request headers). Zero uses
	// [http.DefaultMaxHeaderBytes] (1 MiB), matching the net/http default when unset.
	MaxHeaderBytes int `mapstructure:"max_header_bytes,omitempty"`
	// MaxRequestBodyBytes limits incoming request bodies via http.MaxBytesReader.
	// Zero or unset uses DefaultMaxRequestBodyBytes. -1 disables the limit.
	MaxRequestBodyBytes int64 `mapstructure:"max_request_body_bytes,omitempty"`
	// MaxLogResponseBytes limits the size of streamed log responses.
	// Zero or unset uses DefaultMaxLogResponseBytes (50 MiB). -1 disables the limit.
	MaxLogResponseBytes int64 `mapstructure:"max_log_response_bytes,omitempty"`
	// LogStreamTimeout is the maximum duration for streaming log responses.
	// Zero or unset uses DefaultLogStreamTimeout (5 minutes). This is independent of the
	// server WriteTimeout and allows log streams to run longer than normal responses.
	LogStreamTimeout time.Duration `mapstructure:"log_stream_timeout,omitempty"`
}

// TLSEnabled returns true when both TLS cert and key paths are configured.
func (c *ServiceConfig) TLSEnabled() bool {
	return c.TLSCertFile != "" && c.TLSKeyFile != ""
}

// ValidateTLSConfig returns an error when exactly one of TLSCertFile or
// TLSKeyFile is set, which would cause a silent fallback to plain HTTP.
func (c *ServiceConfig) ValidateTLSConfig() error {
	if (c.TLSCertFile != "") != (c.TLSKeyFile != "") {
		return fmt.Errorf("partial TLS config: both TLSCertFile and TLSKeyFile must be provided")
	}
	return nil
}

const (
	defaultReadTimeout       = 15 * time.Second
	defaultWriteTimeout      = 15 * time.Second
	defaultIdleTimeout       = 60 * time.Second
	defaultReadHeaderTimeout = 15 * time.Second
)

// EffectiveReadTimeout returns http.Server ReadTimeout. When unset or non-positive, returns 15s.
func (c *ServiceConfig) EffectiveReadTimeout() time.Duration {
	if c == nil || c.ReadTimeout <= 0 {
		return defaultReadTimeout
	}
	return c.ReadTimeout
}

// EffectiveWriteTimeout returns http.Server WriteTimeout. When unset or non-positive, returns 15s.
func (c *ServiceConfig) EffectiveWriteTimeout() time.Duration {
	if c == nil || c.WriteTimeout <= 0 {
		return defaultWriteTimeout
	}
	return c.WriteTimeout
}

// EffectiveIdleTimeout returns http.Server IdleTimeout. When unset or non-positive, returns 60s.
func (c *ServiceConfig) EffectiveIdleTimeout() time.Duration {
	if c == nil || c.IdleTimeout <= 0 {
		return defaultIdleTimeout
	}
	return c.IdleTimeout
}

// EffectiveReadHeaderTimeout returns the HTTP server ReadHeaderTimeout. When unset or
// non-positive, it matches the default read timeout used by the server (15s).
func (c *ServiceConfig) EffectiveReadHeaderTimeout() time.Duration {
	if c == nil || c.ReadHeaderTimeout <= 0 {
		return defaultReadHeaderTimeout
	}
	return c.ReadHeaderTimeout
}

// EffectiveMaxHeaderBytes returns http.Server MaxHeaderBytes. When unset or non-positive, returns
// [http.DefaultMaxHeaderBytes] (1 MiB).
func (c *ServiceConfig) EffectiveMaxHeaderBytes() int {
	if c == nil || c.MaxHeaderBytes <= 0 {
		return http.DefaultMaxHeaderBytes
	}
	return c.MaxHeaderBytes
}

// EffectiveMaxRequestBodyBytes returns the limit for http.MaxBytesReader; -1 means no limit.
func (c *ServiceConfig) EffectiveMaxRequestBodyBytes() int64 {
	if c == nil {
		return DefaultMaxRequestBodyBytes
	}
	if c.MaxRequestBodyBytes == -1 {
		return -1
	}
	if c.MaxRequestBodyBytes == 0 {
		return DefaultMaxRequestBodyBytes
	}
	return c.MaxRequestBodyBytes
}

// EffectiveMaxLogResponseBytes returns the byte limit for streamed log responses; -1 means no limit.
func (c *ServiceConfig) EffectiveMaxLogResponseBytes() int64 {
	if c == nil {
		return DefaultMaxLogResponseBytes
	}
	if c.MaxLogResponseBytes == -1 {
		return -1
	}
	if c.MaxLogResponseBytes == 0 {
		return DefaultMaxLogResponseBytes
	}
	return c.MaxLogResponseBytes
}

// EffectiveLogStreamTimeout returns the deadline for log streaming operations.
// Zero or unset returns DefaultLogStreamTimeout (5 minutes).
func (c *ServiceConfig) EffectiveLogStreamTimeout() time.Duration {
	if c == nil || c.LogStreamTimeout <= 0 {
		return DefaultLogStreamTimeout
	}
	return c.LogStreamTimeout
}

// ValidateHTTPConfig returns an error when HTTP-related settings are invalid.
func (c *ServiceConfig) ValidateHTTPConfig() error {
	if c == nil {
		return nil
	}
	if c.ReadTimeout < 0 {
		return fmt.Errorf("service.read_timeout must not be negative")
	}
	if c.WriteTimeout < 0 {
		return fmt.Errorf("service.write_timeout must not be negative")
	}
	if c.IdleTimeout < 0 {
		return fmt.Errorf("service.idle_timeout must not be negative")
	}
	if c.ReadHeaderTimeout < 0 {
		return fmt.Errorf("service.read_header_timeout must not be negative")
	}
	if c.MaxHeaderBytes < 0 {
		return fmt.Errorf("service.max_header_bytes must not be negative")
	}
	if c.MaxRequestBodyBytes < -1 {
		return fmt.Errorf("service.max_request_body_bytes must be -1 (unlimited) or >= 0")
	}
	if c.MaxLogResponseBytes < -1 {
		return fmt.Errorf("service.max_log_response_bytes must be -1 (unlimited) or >= 0")
	}
	if c.LogStreamTimeout < 0 {
		return fmt.Errorf("service.log_stream_timeout must not be negative")
	}
	return nil
}
