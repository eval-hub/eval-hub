package config

type Config struct {
	Service    *ServiceConfig    `mapstructure:"service"`
	Database   *map[string]any   `mapstructure:"database"`
	MLFlow     *MLFlowConfig     `mapstructure:"mlflow,omitempty"`
	OTEL       *OTELConfig       `mapstructure:"otel,omitempty"`
	Prometheus *PrometheusConfig `mapstructure:"prometheus,omitempty"`
}

func (c *Config) IsOTELEnabled() bool {
	return (c != nil) && (c.OTEL != nil) && c.OTEL.Enabled
}

func (c *Config) IsPrometheusEnabled() bool {
	return (c != nil) && (c.Prometheus != nil) && c.Prometheus.Enabled
}
