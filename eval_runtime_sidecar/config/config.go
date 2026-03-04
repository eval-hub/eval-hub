package config

type Config struct {
	Sidecar    *SidecarConfig    `mapstructure:"sidecar"`
	OTEL       *OTELConfig       `mapstructure:"otel,omitempty"`
	Prometheus *PrometheusConfig `mapstructure:"prometheus,omitempty"`
}

func (c *Config) IsOTELEnabled() bool {
	return (c != nil) && (c.OTEL != nil) && c.OTEL.Enabled
}

func (c *Config) IsPrometheusEnabled() bool {
	return (c != nil) && (c.Prometheus != nil) && c.Prometheus.Enabled
}
