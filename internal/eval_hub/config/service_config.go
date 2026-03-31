package config

import "fmt"

type ServiceConfig struct {
	Version         string `mapstructure:"version,omitempty"`
	Build           string `mapstructure:"build,omitempty"`
	BuildDate       string `mapstructure:"build_date,omitempty"`
	Port            int    `mapstructure:"port,omitempty"`
	Host            string `mapstructure:"host,omitempty"`
	ReadyFile       string `mapstructure:"ready_file"`
	TerminationFile string `mapstructure:"termination_file"`
	EvalInitImage   string `mapstructure:"eval_init_image,omitempty"`
	LocalMode       bool   `mapstructure:"local_mode,omitempty"`
	DisableAuth     bool   `mapstructure:"disable_auth,omitempty"`
	KueueEnabled    bool   `mapstructure:"kueue_enabled,omitempty"`
	TLSCertFile     string `mapstructure:"tls_cert_file,omitempty"`
	TLSKeyFile      string `mapstructure:"tls_key_file,omitempty"`
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
