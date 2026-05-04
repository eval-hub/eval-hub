package config

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/eval-hub/eval-hub/internal/logging"
	"github.com/go-playground/validator/v10"
	"github.com/spf13/viper"
	"go.yaml.in/yaml/v2"
)

type Config struct {
	BaseURL   string `mapstructure:"base_url,omitempty" validate:"omitempty,url"`
	Token     string `mapstructure:"token"`
	Tenant    string `mapstructure:"tenant"`
	Insecure  bool   `mapstructure:"insecure"`
	Transport string `mapstructure:"transport" validate:"required,oneof=stdio http"`
	Host      string `mapstructure:"host"      validate:"required"`
	Port      int    `mapstructure:"port,omitempty" validate:"omitempty,min=1,max=65535"`
}

type Flags struct {
	Transport  *string
	Host       *string
	Port       *int
	Insecure   *bool
	ConfigPath string
}

func DefaultConfig() *Config {
	return &Config{
		Transport: "stdio",
		Host:      "localhost",
		Port:      3001,
	}
}

// Load builds a Config using the precedence: CLI flags > YAML config > env vars.
// Environment variables are applied first as the base layer, YAML config values
// override them, and CLI flags (when explicitly set) override everything.
func Load(flags *Flags, logger *slog.Logger) (*Config, error) {
	configPath := ""
	if flags != nil && flags.ConfigPath != "" {
		configPath = flags.ConfigPath
	}
	conf, err := applyYAMLConfig(DefaultConfig(), configPath)
	if err != nil {
		return nil, err
	}

	if flags != nil {
		applyFlags(conf, flags)
	}

	if logger != nil {
		logger.Info("Loaded configuration", "config", logging.AsPrettyJson(conf), "config_path", configPath)
	}

	return conf, nil
}

// Validate checks the Config using go-playground/validator struct tags.
func Validate(cfg *Config) error {
	validate := validator.New(validator.WithRequiredStructEnabled())

	if err := validate.Struct(cfg); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	return nil
}

// applyYAMLConfig reads a YAML config file using Viper and applies the active
// profile's values over the current config. Missing default config files are
// silently ignored; explicitly specified files that don't exist produce an error.
func applyYAMLConfig(cfg *Config, path string) (*Config, error) {
	v := viper.New()
	err := v.BindEnv("base_url", "EVALHUB_BASE_URL")
	if err != nil {
		return nil, fmt.Errorf("binding environment variable EVALHUB_BASE_URL: %w", err)
	}
	err = v.BindEnv("token", "EVALHUB_TOKEN")
	if err != nil {
		return nil, fmt.Errorf("binding environment variable EVALHUB_TOKEN: %w", err)
	}
	err = v.BindEnv("tenant", "EVALHUB_TENANT")
	if err != nil {
		return nil, fmt.Errorf("binding environment variable EVALHUB_TENANT: %w", err)
	}
	err = v.BindEnv("insecure", "EVALHUB_INSECURE")
	if err != nil {
		return nil, fmt.Errorf("binding environment variable EVALHUB_INSECURE: %w", err)
	}
	err = v.BindEnv("transport", "EVALHUB_TRANSPORT")
	if err != nil {
		return nil, fmt.Errorf("binding environment variable EVALHUB_TRANSPORT: %w", err)
	}
	err = v.BindEnv("host", "EVALHUB_HOST")
	if err != nil {
		return nil, fmt.Errorf("binding environment variable EVALHUB_HOST: %w", err)
	}
	err = v.BindEnv("port", "EVALHUB_PORT")
	if err != nil {
		return nil, fmt.Errorf("binding environment variable EVALHUB_PORT: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshalling default config: %w", err)
	}
	v.SetConfigType("yaml")
	if err := v.ReadConfig(io.Reader(bytes.NewReader(data))); err != nil {
		return nil, fmt.Errorf("parsing default config: %w", err)
	}

	if path == "" {
		var conf Config
		if err := v.Unmarshal(&conf); err != nil {
			return nil, fmt.Errorf("unmarshalling config: %w", err)
		}
		return &conf, nil
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path for config file: %w", err)
	}
	path = absPath
	v.SetConfigFile(path)

	if err := v.MergeInConfig(); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("config file not found: %s", v.ConfigFileUsed())
		}
		// Viper wraps file-not-found in its own type
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			return nil, fmt.Errorf("config file not found: %s", v.ConfigFileUsed())
		}
		return nil, fmt.Errorf("reading config file %s: %w", path, err)
	}

	var conf Config
	if err := v.Unmarshal(&conf); err != nil {
		return nil, fmt.Errorf("unmarshalling config: %w", err)
	}

	return &conf, nil
}

func applyFlags(cfg *Config, flags *Flags) {
	if flags.Transport != nil {
		cfg.Transport = *flags.Transport
	}
	if flags.Host != nil {
		cfg.Host = *flags.Host
	}
	if flags.Port != nil {
		cfg.Port = *flags.Port
	}
	if flags.Insecure != nil {
		cfg.Insecure = *flags.Insecure
	}
}
