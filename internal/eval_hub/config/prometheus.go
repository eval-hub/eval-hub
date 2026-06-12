package config

const (
	DefaultMetricsPort = 9090
	DefaultMetricsHost = "0.0.0.0"
)

type PrometheusConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Port    int    `mapstructure:"port,omitempty"`
	Host    string `mapstructure:"host,omitempty"`
}

func (c *PrometheusConfig) EffectivePort() int {
	if c == nil || c.Port <= 0 {
		return DefaultMetricsPort
	}
	return c.Port
}

func (c *PrometheusConfig) EffectiveHost() string {
	if c == nil || c.Host == "" {
		return DefaultMetricsHost
	}
	return c.Host
}
