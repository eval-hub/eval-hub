package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	BaseURL   string
	Token     string
	Tenant    string
	Insecure  bool
	Transport string
	Host      string
	Port      int
}

type ProfileConfig struct {
	DefaultProfile string              `yaml:"default_profile"`
	Profiles       map[string]*Profile `yaml:"profiles"`
}

type Profile struct {
	BaseURL  string `yaml:"base_url"`
	Token    string `yaml:"token"`
	Tenant   string `yaml:"tenant"`
	Insecure *bool  `yaml:"insecure,omitempty"`
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

func Validate(cfg *Config) error {
	switch cfg.Transport {
	case "stdio", "http":
	default:
		return fmt.Errorf("invalid transport %q: must be \"stdio\" or \"http\"", cfg.Transport)
	}

	if cfg.Transport == "http" {
		if cfg.Port < 1 || cfg.Port > 65535 {
			return fmt.Errorf("invalid port %d: must be between 1 and 65535", cfg.Port)
		}
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

func applyYAMLConfig(cfg *Config, path string, flags *Flags) error {
	if path == "" {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			if flags != nil && flags.ConfigPath != "" {
				return fmt.Errorf("config file not found: %s", path)
			}
			return nil
		}
		return fmt.Errorf("reading config file: %w", err)
	}

	var profileCfg ProfileConfig
	if err := yaml.Unmarshal(data, &profileCfg); err != nil {
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
