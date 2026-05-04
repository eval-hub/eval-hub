package config

const (
	// SidecarTerminationFilePath is used for Kubernetes termination messages.
	SidecarTerminationFilePath = "/data/termination-log"
)

type Config struct {
	Service    *ServiceConfig    `mapstructure:"service"`
	Database   *map[string]any   `mapstructure:"database"`
	MLFlow     *MLFlowConfig     `mapstructure:"mlflow,omitempty"`
	OTEL       *OTELConfig       `mapstructure:"otel,omitempty"`
	Prometheus *PrometheusConfig `mapstructure:"prometheus,omitempty"`
	Sidecar    *SidecarConfig    `mapstructure:"sidecar,omitempty"`
}

// IsOTELEnabled reports whether OpenTelemetry export is turned on in config.
func (c *Config) IsOTELEnabled() bool {
	return (c != nil) && (c.OTEL != nil) && c.OTEL.Enabled
}

// IsOTELStorageScansEnabled reports whether OTEL is enabled and database scan spans are not disabled.
func (c *Config) IsOTELStorageScansEnabled() bool {
	return c.IsOTELEnabled() && !c.OTEL.DisableDatabaseOTELScans
}

// IsPrometheusEnabled reports whether the Prometheus metrics endpoint is enabled.
func (c *Config) IsPrometheusEnabled() bool {
	return (c != nil) && (c.Prometheus != nil) && c.Prometheus.Enabled
}

// IsAuthenticationEnabled reports whether HTTP authentication should be enforced (not disabled and not local mode).
func (c *Config) IsAuthenticationEnabled() bool {
	return (c != nil) && (c.Service != nil) && !c.Service.DisableAuth && !c.Service.LocalMode
}
