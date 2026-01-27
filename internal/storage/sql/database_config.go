package sql

import (
	"fmt"
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
	DatabaseName    string         `mapstructure:"database_name,omitempty"`
	// Tables configurations
	Evaluations SQLTableConfig `mapstructure:"evaluations"`
	Collections SQLTableConfig `mapstructure:"collections"`
	// Other map[string]any `mapstructure:",remain"`
}

type SQLTableConfig struct {
	TableName     string `mapstructure:"table_name"`
	JSONFieldType string `mapstructure:"json_field_type,omitempy"` // fallback is TEXT
}

func (tc *SQLTableConfig) CheckConfig() error {
	if tc.TableName == "" {
		return fmt.Errorf("missing table name")
	}
	if tc.JSONFieldType == "" {
		tc.JSONFieldType = "TEXT"
	}
	return nil
}
