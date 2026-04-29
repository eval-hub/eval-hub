package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/spf13/viper"
)

type Config struct {
	BaseURL   string `mapstructure:"base_url"  validate:"omitempty,url"`
	Token     string `mapstructure:"token"`
	Tenant    string `mapstructure:"tenant"`
	Insecure  bool   `mapstructure:"insecure"`
	Transport string `mapstructure:"transport" validate:"required,oneof=stdio http"`
	Host      string `mapstructure:"host"      validate:"required"`
	Port      int    `mapstructure:"port,omitempty" validate:"omitempty,min=1,max=65535"`
}

type ProfileConfig struct {
	DefaultProfile string              `mapstructure:"default_profile"`
	Profiles       map[string]*Profile `mapstructure:"profiles"`
}

type Profile struct {
	BaseURL  string `mapstructure:"base_url"`
	Token    string `mapstructure:"token"`
	Tenant   string `mapstructure:"tenant"`
	Insecure *bool  `mapstructure:"insecure,omitempty"`
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

func defaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".evalhub", "config.yaml")
}

// Load builds a Config using the precedence: CLI flags > YAML config > env vars.
// Environment variables are applied first as the base layer, YAML config values
// override them, and CLI flags (when explicitly set) override everything.
func Load(flags *Flags) (*Config, error) {
	cfg := DefaultConfig()

	applyEnvVars(cfg)

	configPath := defaultConfigPath()
	if flags != nil && flags.ConfigPath != "" {
		configPath = flags.ConfigPath
	}
	if err := applyYAMLConfig(cfg, configPath, flags); err != nil {
		return nil, err
	}

	if flags != nil {
		applyFlags(cfg, flags)
	}

	return cfg, nil
}

// Validate checks the Config using go-playground/validator struct tags.
func Validate(cfg *Config) error {
	validate := validator.New(validator.WithRequiredStructEnabled())

	if err := validate.Struct(cfg); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	return nil
}

func applyEnvVars(cfg *Config) {
	if v := os.Getenv("EVALHUB_BASE_URL"); v != "" {
		cfg.BaseURL = v
	}
	if v := os.Getenv("EVALHUB_TOKEN"); v != "" {
		cfg.Token = v
	}
	if v := os.Getenv("EVALHUB_TENANT"); v != "" {
		cfg.Tenant = v
	}
	if v := os.Getenv("EVALHUB_INSECURE"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Insecure = b
		}
	}
}

// applyYAMLConfig reads a YAML config file using Viper and applies the active
// profile's values over the current config. Missing default config files are
// silently ignored; explicitly specified files that don't exist produce an error.
func applyYAMLConfig(cfg *Config, path string, flags *Flags) error {
	if path == "" {
		return nil
	}

	v := viper.New()
	v.SetConfigFile(path)

	if err := v.ReadInConfig(); err != nil {
		if os.IsNotExist(err) {
			if flags != nil && flags.ConfigPath != "" {
				return fmt.Errorf("config file not found: %s", path)
			}
			return nil
		}
		// Viper wraps file-not-found in its own type
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			if flags != nil && flags.ConfigPath != "" {
				return fmt.Errorf("config file not found: %s", path)
			}
			return nil
		}
		return fmt.Errorf("reading config file %s: %w", path, err)
	}

	var profileCfg ProfileConfig
	if err := v.Unmarshal(&profileCfg); err != nil {
		return fmt.Errorf("parsing config file %s: %w", path, err)
	}

	if len(profileCfg.Profiles) == 0 {
		return nil
	}

	profileName := profileCfg.DefaultProfile
	if profileName == "" {
		profileName = "default"
	}

	profile, ok := profileCfg.Profiles[profileName]
	if !ok {
		available := make([]string, 0, len(profileCfg.Profiles))
		for k := range profileCfg.Profiles {
			available = append(available, k)
		}
		return fmt.Errorf("profile %q not found in config file (available: %s)", profileName, strings.Join(available, ", "))
	}

	if profile.BaseURL != "" {
		cfg.BaseURL = profile.BaseURL
	}
	if profile.Token != "" {
		cfg.Token = profile.Token
	}
	if profile.Tenant != "" {
		cfg.Tenant = profile.Tenant
	}
	if profile.Insecure != nil {
		cfg.Insecure = *profile.Insecure
	}

	return nil
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
