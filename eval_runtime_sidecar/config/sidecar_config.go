package config

type SidecarConfig struct {
	Version         string `mapstructure:"version,omitempty"`
	Build           string `mapstructure:"build,omitempty"`
	BuildDate       string `mapstructure:"build_date,omitempty"`
	Port            int    `mapstructure:"port,omitempty"`
	ReadyFile       string `mapstructure:"ready_file,omitempty"`
	TerminationFile string `mapstructure:"termination_file,omitempty"`
	LocalMode       bool   `mapstructure:"local_mode,omitempty"`
	DisableAuth     bool   `mapstructure:"disable_auth,omitempty"`
}
