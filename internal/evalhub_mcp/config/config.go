package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/eval-hub/eval-hub/internal/logging"
	"github.com/eval-hub/eval-hub/pkg/evalhubclient"
	"github.com/go-playground/validator/v10"
	"github.com/spf13/viper"
)

type Config struct {
	BaseURL       string `mapstructure:"base_url,omitempty" validate:"omitempty,url"`
	Token         string `mapstructure:"token"`
	Tenant        string `mapstructure:"tenant"`
	Insecure      bool   `mapstructure:"insecure"`
	Transport     string `mapstructure:"transport" validate:"required,oneof=stdio http http-sse"`
	Host          string `mapstructure:"host"      validate:"required"`
	Port          int    `mapstructure:"port,omitempty" validate:"omitempty,min=1,max=65535"`
	ListPageLimit int    `mapstructure:"list_page_limit,omitempty" validate:"omitempty,min=1,max=2000"`
	TLSCertFile   string `mapstructure:"tls_cert_file"`
	TLSKeyFile    string `mapstructure:"tls_key_file"`
}

type Flags struct {
	Transport   *string
	Host        *string
	Port        *int
	Insecure    *bool
	ConfigPath  string
	TLSCertFile *string
	TLSKeyFile  *string
}

func DefaultConfig() *Config {
	return &Config{
		Transport:     "stdio",
		Host:          "localhost",
		Port:          3001,
		ListPageLimit: evalhubclient.DefaultListPageLimit,
	}
}

// Load builds a Config by merging DefaultConfig, optional YAML at flags.ConfigPath,
// and bound EVALHUB_* environment variables using Viper (for each key, env overrides
// the YAML file and defaults). Finally, any CLI fields that were explicitly set on
// flags override the merged result.
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

	normalizeListPageLimit(conf)

	if logger != nil {
		logger.Info("Loaded configuration", "config", logging.AsPrettyJson(conf, "token"), "config_path", configPath)
	}

	return conf, nil
}

func normalizeListPageLimit(cfg *Config) {
	if cfg == nil {
		return
	}
	if cfg.ListPageLimit == 0 {
		cfg.ListPageLimit = evalhubclient.DefaultListPageLimit
	}
}

// TLSEnabled returns true when both TLS cert and key files are configured.
func (c *Config) TLSEnabled() bool {
	return c.TLSCertFile != "" && c.TLSKeyFile != ""
}

// IsHTTPTransport returns true for any HTTP-based transport mode.
func (c *Config) IsHTTPTransport() bool {
	return c.Transport == "http" || c.Transport == "http-sse"
}

// Validate checks the Config using go-playground/validator struct tags.
func Validate(cfg *Config) error {
	normalizeListPageLimit(cfg)
	validate := validator.New(validator.WithRequiredStructEnabled())

	if err := validate.Struct(cfg); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	if (cfg.TLSCertFile == "") != (cfg.TLSKeyFile == "") {
		return fmt.Errorf("config validation failed: tls_cert_file and tls_key_file must both be set or both be empty")
	}

	return nil
}

func bindEnvs(v *viper.Viper, envs ...string) error {
	for i := 0; i < len(envs); i += 2 {
		err := v.BindEnv(envs[i], envs[i+1])
		if err != nil {
			return fmt.Errorf("binding environment variable %s: %w", envs[i], err)
		}
	}
	return nil
}

// applyYAMLConfig seeds Viper with cfg, binds EVALHUB_* env vars, then merges an
// optional YAML file when path is non-empty (env still overrides merged values per
// Viper precedence). When path is empty, only defaults and environment apply. When
// path is set but the file does not exist, returns an error.
func applyYAMLConfig(cfg *Config, path string) (*Config, error) {
	v := viper.New()
	err := bindEnvs(
		v,
		"base_url", "EVALHUB_BASE_URL",
		"token", "EVALHUB_TOKEN",
		"tenant", "EVALHUB_TENANT",
		"insecure", "EVALHUB_INSECURE",
		"transport", "EVALHUB_TRANSPORT",
		"host", "EVALHUB_HOST",
		"port", "EVALHUB_PORT",
		"list_page_limit", "EVALHUB_LIST_PAGE_LIMIT",
		"tls_cert_file", "EVALHUB_TLS_CERT_FILE",
		"tls_key_file", "EVALHUB_TLS_KEY_FILE",
	)
	if err != nil {
		return nil, err
	}

	if cfg != nil {
		v.SetDefault("base_url", cfg.BaseURL)
		v.SetDefault("token", cfg.Token)
		v.SetDefault("tenant", cfg.Tenant)
		v.SetDefault("insecure", cfg.Insecure)
		v.SetDefault("transport", cfg.Transport)
		v.SetDefault("host", cfg.Host)
		v.SetDefault("port", cfg.Port)
		v.SetDefault("list_page_limit", cfg.ListPageLimit)
		v.SetDefault("tls_cert_file", cfg.TLSCertFile)
		v.SetDefault("tls_key_file", cfg.TLSKeyFile)
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
	v.SetConfigType("yaml")
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
	if flags.TLSCertFile != nil {
		cfg.TLSCertFile = *flags.TLSCertFile
	}
	if flags.TLSKeyFile != nil {
		cfg.TLSKeyFile = *flags.TLSKeyFile
	}
}
