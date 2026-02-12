package config

type Config struct {
	Service    *ServiceConfig    `mapstructure:"service"`
	Database   *map[string]any   `mapstructure:"database"`
	MLFlow     *MLFlowConfig     `mapstructure:"mlflow,omitempty"`
	OTEL       *OTELConfig       `mapstructure:"otel,omitempty"`
	Prometheus *PrometheusConfig `mapstructure:"prometheus,omitempty"`
}
