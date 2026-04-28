package shared

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

type DatabaseConfig struct {
	SQL map[string]SQLDatabaseConfig `mapstructure:"sql,omitempty"`
}

type SQLDatabaseConfig struct {
	Enabled         bool           `mapstructure:"enabled,omitempty"`
	Driver          string         `mapstructure:"driver"`
	URL             string         `mapstructure:"url"`
	ConnMaxLifetime *time.Duration `mapstructure:"conn_max_lifetime,omitempty"`
	MaxIdleConns    *int           `mapstructure:"max_idle_conns,omitempty"`
	MaxOpenConns    *int           `mapstructure:"max_open_conns,omitempty"`
	Fallback        bool           `mapstructure:"fallback,omitempty"`
	TxRetryMax      int            `mapstructure:"tx_retry_max,omitempty"`
	TxRetryInterval time.Duration  `mapstructure:"tx_retry_interval,omitempty"`

	// Other map[string]any `mapstructure:",remain"`
}

const (
	DefaultTxRetryMax      = 3
	DefaultTxRetryInterval = 50 * time.Millisecond
)

func (s *SQLDatabaseConfig) GetTxRetryMax() int {
	if s.TxRetryMax <= 0 {
		return DefaultTxRetryMax
	}
	return s.TxRetryMax
}

func (s *SQLDatabaseConfig) GetTxRetryInterval() time.Duration {
	if s.TxRetryInterval <= 0 {
		return DefaultTxRetryInterval
	}
	return s.TxRetryInterval
}

func (s *SQLDatabaseConfig) GetDriverName() string {
	return s.Driver
}

func (s *SQLDatabaseConfig) GetConnectionURL() (string, error) {
	// Sanitize URL to avoid exposing credentials
	parsed, err := url.Parse(s.URL)
	if err != nil {
		return "", fmt.Errorf("failed to parse connection URL: %w", err)
	}
	// Remove password from userinfo
	if parsed.User != nil {
		parsed.User = url.User(parsed.User.Username())
	}
	return parsed.String(), nil
}

func (s *SQLDatabaseConfig) GetDatabaseName() string {
	parsed, err := url.Parse(s.URL)
	if err != nil {
		return ""
	}
	if parsed.Scheme == "file" {
		return strings.TrimSuffix(strings.TrimPrefix(parsed.Opaque, ":"), ":")
	}
	return strings.TrimPrefix(parsed.Path, "/")
}

func (s *SQLDatabaseConfig) GetUser() string {
	parsed, err := url.Parse(s.URL)
	if err != nil {
		return ""
	}
	if parsed.User == nil {
		return ""
	}
	return parsed.User.Username()
}
