package config

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/spf13/viper"
)

var (
	configLookup = []string{"config/providers", "./config/providers", "../../config/providers", "../../../config/providers"}

	once        = sync.Once{}
	isLocalMode = false
)

type EnvMap struct {
	EnvMappings map[string]string `mapstructure:"env_mappings,omitempty"`
}

type SecretMap struct {
	Dir      string            `mapstructure:"dir,omitempty"`
	Mappings map[string]string `mapstructure:"mappings,omitempty"`
}

func localMode() bool {
	once.Do(func() {
		localMode := flag.Bool("local", false, "Server operates in local mode or not.")
		flag.Parse()
		isLocalMode = *localMode
	})
	return isLocalMode
}

// readConfig locates and reads a configuration file using Viper. It searches for
// a file named "{name}.{ext}" in each of the given directories in order; the first
// found file is read. The returned Viper instance contains the parsed config and
// can be used for further unmarshaling or env binding.
//
// Parameters:
//   - logger: Logger for config load messages (success and failure).
//   - name: Config file base name without extension (e.g., "config").
//   - ext: Config file extension/type (e.g., "yaml"); used by Viper as config type.
//   - dirs: One or more directories to search for the file; first match wins.
//
// Returns:
//   - *viper.Viper: Viper instance with the config loaded, or a new Viper if no file was read.
//   - error: Non-nil if no config file was found in any dir or if reading failed.
func readConfig(logger *slog.Logger, name string, ext string, dirs ...string) (*viper.Viper, error) {
	logger.Debug("Reading the configuration file", "file", fmt.Sprintf("%s.%s", name, ext), "dirs", fmt.Sprintf("%v", dirs))

	configValues := viper.New()

	configValues.SetConfigName(name) // name of config file (without extension)
	configValues.SetConfigType(ext)  // REQUIRED if the config file does not have the extension in the name
	for _, dir := range dirs {
		configValues.AddConfigPath(dir)
	}
	err := configValues.ReadInConfig() // Find and read the config file

	if err != nil {
		logger.Error("Failed to read the configuration file", "file", fmt.Sprintf("%s.%s", name, ext), "dirs", fmt.Sprintf("%v", dirs), "error", err.Error())
	} else {
		logger.Debug("Read the configuration file", "file", configValues.ConfigFileUsed())
	}

	return configValues, err
}

func loadProvider(logger *slog.Logger, file string) (api.ProviderResource, error) {
	providerConfig := api.ProviderResource{}
	configValues, err := readConfig(logger, file, "yaml", configLookup...)
	if err != nil {
		return providerConfig, err
	}

	if err := configValues.Unmarshal(&providerConfig); err != nil {
		return providerConfig, err
	}
	return providerConfig, nil
}

func scanFolders(logger *slog.Logger, dirs ...string) ([]os.DirEntry, error) {
	var dirsChecked []string
	for _, dir := range dirs {
		absDir, err := filepath.Abs(dir)
		if err != nil {
			logger.Error("Failed to get absolute path for provider config directory", "directory", dir, "error", err.Error())
			continue
		}
		dirsChecked = append(dirsChecked, absDir)
		files, err := os.ReadDir(absDir)
		if err != nil {
			continue
		}
		return files, nil
	}
	logger.Warn("No providers found", "directories", dirsChecked)
	return []os.DirEntry{}, nil
}

func LoadProviderConfigs(logger *slog.Logger, dirs ...string) (map[string]api.ProviderResource, error) {
	if len(dirs) == 0 {
		dirs = configLookup
	}
	providerConfigs := make(map[string]api.ProviderResource)
	files, err := scanFolders(logger, dirs...)
	if err != nil {
		return providerConfigs, err
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
func LoadConfig(logger *slog.Logger, version string, build string, buildDate string, dirs ...string) (*Config, error) {
	logger.Info("Start reading configuration", "version", version, "build", build, "build_date", buildDate, "dirs", dirs)

	if len(dirs) == 0 {
		dirs = []string{"config", "./config", "../../config", "tests"} // tests is for running the service on a local machine (not local mode)
	}
	configValues, err := readConfig(logger, "config", "yaml", dirs...)
	if err != nil {
		logger.Error("Failed to read configuration file config.yaml", "error", err.Error(), "dirs", dirs)
		return nil, err
	}

	// now load the cluster config if found
	// set up the secrets from the secrets directory
	secrets := SecretMap{}
	if err := configValues.Unmarshal(&secrets); err != nil {
		logger.Error("Failed to unmarshal secret mappings", "error", err.Error())
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
					// don't log the secret here as it may contain sensitive information
					logger.Info("Set secret", "field_name", fieldName)
				}
			}
		}
	}
	// set up the environment variable mappings
	envMappings := EnvMap{}
	if err := configValues.Unmarshal(&envMappings); err != nil {
		logger.Error("Failed to unmarshal environment variable mappings", "error", err.Error())
		return nil, err
	}
	for envName, field := range envMappings.EnvMappings {
		configValues.BindEnv(field, strings.ToUpper(envName))
		logger.Info("Mapped environment variable", "field_name", field, "env_name", envName)
	}

	conf := Config{}
	if err := configValues.Unmarshal(&conf); err != nil {
		logger.Error("Failed to unmarshal configuration", "error", err.Error())
		return nil, err
	}

	// set the version, build, and build date
	conf.Service.Version = version
	conf.Service.Build = build
	conf.Service.BuildDate = buildDate
	conf.Service.LocalMode = localMode()

	// TODO make a safe version to ensure that secrets are not logged
	logger.Info("End reading configuration", "config", asJSON(conf))
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

func asJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(data)
}
