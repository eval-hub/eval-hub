package sql

import (
	"net/url"
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

	// Other map[string]any `mapstructure:",remain"`
}

func (s *SQLDatabaseConfig) getDriverName() string {
	return s.Driver
}

func (s *SQLDatabaseConfig) getConnectionURL() string {
	// Sanitize URL to avoid exposing credentials
	parsed, err := url.Parse(s.URL)
	if err != nil {
		return s.Driver + "://<parse-error>"
	}
	// Remove password from userinfo
	if parsed.User != nil {
		parsed.User = url.User(parsed.User.Username())
	}
	return parsed.String()
}
