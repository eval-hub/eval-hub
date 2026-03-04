package config

type ServiceConfig struct {
	Version         string     `mapstructure:"version,omitempty"`
	Build           string     `mapstructure:"build,omitempty"`
	BuildDate       string     `mapstructure:"build_date,omitempty"`
	Port            int        `mapstructure:"port,omitempty"`
	ReadyFile       string     `mapstructure:"ready_file"`
	TerminationFile string     `mapstructure:"termination_file"`
	LocalMode       bool       `mapstructure:"local_mode,omitempty"`
	DisableAuth     bool       `mapstructure:"disable_auth,omitempty"`
	TLS             *TLSConfig `mapstructure:"tls,omitempty"`
}

type TLSConfig struct {
	CertFile string `mapstructure:"cert_file"`
	KeyFile  string `mapstructure:"key_file"`
}
