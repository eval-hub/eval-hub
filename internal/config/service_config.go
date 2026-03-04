package config

type ServiceConfig struct {
	Version         string `mapstructure:"version,omitempty"`
	Build           string `mapstructure:"build,omitempty"`
	BuildDate       string `mapstructure:"build_date,omitempty"`
	Port            int    `mapstructure:"port,omitempty"`
	Host            string `mapstructure:"host,omitempty"`
	ReadyFile       string `mapstructure:"ready_file"`
	TerminationFile string `mapstructure:"termination_file"`
	LocalMode       bool   `mapstructure:"local_mode,omitempty"`
	DisableAuth     bool   `mapstructure:"disable_auth,omitempty"`
	TLSCertFile     string `mapstructure:"tls_cert_file,omitempty"`
	TLSKeyFile      string `mapstructure:"tls_key_file,omitempty"`
}

// TLSEnabled returns true when both TLS cert and key paths are configured.
func (c *ServiceConfig) TLSEnabled() bool {
	return c.TLSCertFile != "" && c.TLSKeyFile != ""
}
