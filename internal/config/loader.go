package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/spf13/viper"
)

type EnvMap struct {
	EnvMappings map[string]string `mapstructure:"env_mappings,omitempty"`
}

type SecretMap struct {
	Dir      string            `mapstructure:"dir,omitempty"`
	Mappings map[string]string `mapstructure:"mappings,omitempty"`
}

func readConfig(logger *slog.Logger, defaultConfigValues *viper.Viper, name string, ext string, dirs ...string) (*viper.Viper, error) {
	logger.Info("Reading the configuration file", "file", fmt.Sprintf("%s.%s", name, ext), "dirs", fmt.Sprintf("%v", dirs))

	configValues := viper.New()

	if defaultConfigValues != nil {
		// set the default values
		for _, key := range defaultConfigValues.AllKeys() {
			configValues.SetDefault(key, defaultConfigValues.Get(key))
		}
	}

	configValues.SetConfigName(name) // name of config file (without extension)
	configValues.SetConfigType(ext)  // REQUIRED if the config file does not have the extension in the name
	for _, dir := range dirs {
		configValues.AddConfigPath(dir)
	}
	err := configValues.ReadInConfig() // Find and read the config file

	if err != nil {
		logger.Error("Failed to read the configuration file", "file", fmt.Sprintf("%s.%s", name, ext), "dirs", fmt.Sprintf("%v", dirs), "error", err.Error())
	} else {
		logger.Info("Read the configuration file", "file", configValues.ConfigFileUsed())
	}

	return configValues, err
}

func loadProvider(logger *slog.Logger, file string) (*api.ProviderResource, error) {
	providerConfig := &api.ProviderResource{}
	configValues, err := readConfig(logger, nil, file, "yaml", "config/providers", "./config/providers", "../../config/providers")
	if err != nil {
		return nil, err
	}

	if err := configValues.Unmarshal(&providerConfig); err != nil {
		return nil, err
	}
	return providerConfig, nil
}

func scanFolders(logger *slog.Logger, dirs ...string) ([]os.DirEntry, error) {
	for _, dir := range dirs {
		files, err := os.ReadDir(dir)
		if err != nil {
			logger.Warn("No providers found in the directory", "directory", dir)
			continue
		}
		return files, nil
	}
	return nil, fmt.Errorf("No files found in the directories")
}

func LoadProviderConfigs(logger *slog.Logger) (map[string]*api.ProviderResource, error) {
	providerConfigs := make(map[string]*api.ProviderResource)
	files, err := scanFolders(logger, "./config/providers", "../../config/providers")
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".yaml") {
			continue
		}
		name := strings.TrimSuffix(file.Name(), ".yaml")
		providerConfig, err := loadProvider(logger, name)
		if err != nil {
			return nil, err
		}

		if providerConfig.ProviderID == "" {
			logger.Warn("Provider config missing provider_id, skipping", "file", file.Name())
			continue
		}

		providerConfigs[providerConfig.ProviderID] = providerConfig
		logger.Info("Provider loaded", "provider_id", providerConfig.ProviderID)
	}

	return providerConfigs, nil
}

// LoadConfig loads configuration using a two-tier system with Viper. This implements
// a sophisticated loading strategy that supports cascading configuration values and
// multiple sources.
//
// Configuration loading order (later sources override earlier ones):
//  1. config.yaml (config/config.yaml) - Configuration loaded first
//  2. Environment variables - Mapped via env.mappings configuration
//  3. Secrets from files - Mapped via secrets.mappings with secrets.dir
//
// Configuration supports:
//   - Environment variable mapping: Define in env.mappings (e.g., PORT → service.port)
//   - Secrets from files: Define in secrets.mappings with secrets.dir (e.g., /tmp/db_password → database.password)
//   - Optional secrets: Append :optional to the secret file name to mark it as optional.
//     If an optional secret file doesn't exist, no error is logged and the configuration
//     continues loading without that secret value.
//
// Example configuration structure:
//
//	env:
//	  mappings:
//	    service.port: PORT
//	secrets:
//	  dir: /tmp
//	  mappings:
//	    database.password: db_password
//	    optional.token: api_token:optional
//
// Parameters:
//   - logger: The logger for configuration loading messages
//
// Returns:
//   - *Config: The loaded configuration with all sources applied
//   - error: An error if configuration cannot be loaded or is invalid
func LoadConfig(logger *slog.Logger, version string, build string, buildDate string) (*Config, error) {
	configValues, err := readConfig(logger, nil, "config", "yaml", "config", "./config", "../../config")
	if err != nil {
		return nil, err
	}

	// now load the cluster config if found
	// set up the secrets from the secrets directory
	secrets := SecretMap{}
	if err := configValues.Unmarshal(&secrets); err != nil {
		return nil, err
	}
	if secrets.Dir != "" {
		// check that the secrets directory exists
		if _, err := os.Stat(secrets.Dir); !os.IsNotExist(err) {
			for fileName, fieldName := range secrets.Mappings {
				// the secret file name can be optional by appending :optional to the file name
				optional := strings.HasSuffix(fileName, ":optional")
				if optional {
					fileName = strings.TrimSuffix(fileName, ":optional")
				}
				secret, err := getSecret(secrets.Dir, fileName, optional)
				if err != nil {
					// log the error and fail the startup (by returning the error)
					logger.Error("Failed to read secret file", "file", fmt.Sprintf("%s/%s", secrets.Dir, fileName), "error", err.Error())
					return nil, err
				}
				if secret != "" {
					configValues.Set(fieldName, secret)
				}
			}
		}
	}
	// set up the environment variable mappings
	envMappings := EnvMap{}
	if err := configValues.Unmarshal(&envMappings); err != nil {
		return nil, err
	}
	for envName, field := range envMappings.EnvMappings {
		configValues.BindEnv(field, strings.ToUpper(envName))
		logger.Info("Mapped environment variable", "field_name", field, "env_name", envName)
	}

	conf := Config{}
	if err := configValues.Unmarshal(&conf); err != nil {
		return nil, err
	}

	// set the version, build, and build date
	conf.Service.Version = version
	conf.Service.Build = build
	conf.Service.BuildDate = buildDate

	return &conf, nil
}

// getSecret reads a secret from a file and returns the value as a string.
// If the file does not exist and optional is false, it logs an error and returns an empty string.
// If the file does not exist and optional is true, it silently returns an empty string.
// If the file cannot be read (permissions, etc.), it always logs an error and returns an empty string.
//
// Parameters:
//   - logger: The logger for logging messages
//   - secretsDir: The directory containing the secret files
//   - secretName: The name of the secret file
//   - optional: If true, missing files won't generate error logs
//
// Returns:
//   - string: The value of the secret as a string, or empty string if file doesn't exist or cannot be read
func getSecret(secretsDir string, secretName string, optional bool) (string, error) {
	// this is the full name of the secrets file to read
	secret, err := os.ReadFile(fmt.Sprintf("%s/%s", secretsDir, secretName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && optional {
			return "", nil
		}
		return "", err
	}
	return string(secret), nil
}
