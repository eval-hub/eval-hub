package config

type Config struct {
	Service  *ServiceConfig  `mapstructure:"service"`
	Database *map[string]any `mapstructure:"database"`
}
