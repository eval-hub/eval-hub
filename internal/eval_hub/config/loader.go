package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/go-playground/validator/v10"
	"github.com/spf13/viper"
)

var (
	configLookup = []string{"config", "./config", "../../config", "../../../config"}
)

type EnvMap struct {
	EnvMappings map[string]string `mapstructure:"env_mappings,omitempty"`
}

type SecretMap struct {
	Dir      string            `mapstructure:"dir,omitempty"`
	Mappings map[string]string `mapstructure:"mappings,omitempty"`
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

	// set up the environment variable mappings
	envMappings := EnvMap{}
	if err := configValues.Unmarshal(&envMappings); err != nil {
		logger.Error("Failed to unmarshal environment variable mappings", "error", err.Error())
		return nil, err
	}
	if len(envMappings.EnvMappings) > 0 {
		for envName, field := range envMappings.EnvMappings {
			if err := configValues.BindEnv(field, strings.ToUpper(envName)); err != nil {
				logger.Error("Failed to bind environment variable", "field_name", field, "env_name", envName, "error", err.Error())
				return nil, err
			}
			logger.Info("Mapped environment variable", "field_name", field, "env_name", envName)
		}
		// now we need to reload the config file
		err = configValues.ReadInConfig()
		if err != nil {
			logger.Error("Failed to reload the configuration file", "error", err.Error())
			return nil, err
		}
	}

	return configValues, err
}

// gpuNodeSelectorKeyDelimiter is used when unmarshaling node_selector only. The main Viper
// instance uses "." which splits dotted label keys during struct Unmarshal; a dedicated
// Viper with a different delimiter does not. See https://pkg.go.dev/github.com/spf13/viper#KeyDelimiter.
const gpuNodeSelectorKeyDelimiter = "::"

// parseGPUNodeSelector unmarshals node_selector from the value returned by Get on the main
// Viper. Use Get, not AllSettings: AllSettings re-applies the "." delimiter and nests keys
// like nvidia.com/gpu.product into map[nvidia][com/gpu][product].
func parseGPUNodeSelector(raw any) (map[string]string, error) {
	if raw == nil {
		return nil, nil
	}
	selectorV := viper.NewWithOptions(viper.KeyDelimiter(gpuNodeSelectorKeyDelimiter))
	if err := selectorV.MergeConfigMap(map[string]any{"node_selector": raw}); err != nil {
		return nil, fmt.Errorf("merge node_selector config: %w", err)
	}
	var out struct {
		NodeSelector map[string]string `mapstructure:"node_selector"`
	}
	if err := selectorV.Unmarshal(&out); err != nil {
		return nil, fmt.Errorf("unmarshal node_selector: %w", err)
	}
	return out.NodeSelector, nil
}

func applyGPUNodeSelector(cfg *api.ProviderConfig, nodeSelector map[string]string) {
	if len(nodeSelector) == 0 {
		return
	}
	if cfg.Runtime == nil {
		cfg.Runtime = &api.Runtime{}
	}
	if cfg.Runtime.K8s == nil {
		cfg.Runtime.K8s = &api.K8sRuntime{}
	}
	if cfg.Runtime.K8s.GPU == nil {
		cfg.Runtime.K8s.GPU = &api.GPUConfig{}
	}
	cfg.Runtime.K8s.GPU.NodeSelector = nodeSelector
}

func loadProvider(logger *slog.Logger, validate *validator.Validate, file string, dirs ...string) (*api.ProviderResource, string, error) {
	type providerConfigInternal struct {
		ID                 string `mapstructure:"id" yaml:"id" json:"id"`
		api.ProviderConfig `mapstructure:",squash"`
	}
	providerConfig := providerConfigInternal{}
	configValues, err := readConfig(logger, file, "yaml", dirs...)
	if err != nil {
		return nil, "", err
	}

	// node_selector is stripped before Unmarshal because struct decode uses "." paths and
	// cannot fill map[string]string; parseGPUNodeSelector re-decodes the Get() value with "::".
	configPath := configValues.ConfigFileUsed()
	var rawNodeSelector any
	if configValues.IsSet("runtime.k8s.gpu.node_selector") {
		rawNodeSelector = configValues.Get("runtime.k8s.gpu.node_selector")
		configValues.Set("runtime.k8s.gpu.node_selector", nil)
	}
	if err := configValues.Unmarshal(&providerConfig); err != nil {
		return nil, configPath, err
	}
	if rawNodeSelector != nil {
		nodeSelector, err := parseGPUNodeSelector(rawNodeSelector)
		if err != nil {
			return nil, configPath, err
		}
		applyGPUNodeSelector(&providerConfig.ProviderConfig, nodeSelector)
	}
	res := &api.ProviderResource{
		Resource: api.Resource{
			ID:    providerConfig.ID,
			Owner: "system",
		},
		ProviderConfig: providerConfig.ProviderConfig,
	}

	// validate the provider
	if err := validate.Struct(res); err != nil {
		return nil, configValues.ConfigFileUsed(), err
	}

	return res, configValues.ConfigFileUsed(), nil
}

func loadCollection(logger *slog.Logger, validate *validator.Validate, file string, dirs ...string) (*api.CollectionResource, string, error) {
	type collectionConfigInternal struct {
		ID                   string `mapstructure:"id"`
		api.CollectionConfig `mapstructure:",squash"`
	}
	collectionConfig := collectionConfigInternal{}
	configValues, err := readConfig(logger, file, "yaml", dirs...)
	if err != nil {
		return nil, "", err
	}

	if err := configValues.Unmarshal(&collectionConfig); err != nil {
		return nil, configValues.ConfigFileUsed(), err
	}
	res := &api.CollectionResource{
		Resource: api.Resource{
			ID:    collectionConfig.ID,
			Owner: "system",
		},
		CollectionConfig: collectionConfig.CollectionConfig,
	}

	// validate the collection
	if err := validate.Struct(res); err != nil {
		return nil, configValues.ConfigFileUsed(), err
	}

	return res, configValues.ConfigFileUsed(), nil
}

func scanFolders(logger *slog.Logger, dirs ...string) ([]os.DirEntry, string, error) {
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
		return files, absDir, nil
	}
	logger.Warn("No config files found", "directories", dirsChecked)
	return []os.DirEntry{}, "", nil
}

func hasExplicitConfigDir(dirs []string) bool {
	return len(dirs) > 0 && dirs[0] != ""
}

func LoadProviderConfigs(logger *slog.Logger, validate *validator.Validate, dirs ...string) (map[string]api.ProviderResource, error) {
	if !hasExplicitConfigDir(dirs) {
		dirs = []string{}
		for _, dir := range configLookup {
			dirs = append(dirs, dir+"/providers")
		}
	} else {
		dirs = []string{dirs[0] + "/providers"}
	}

	providerConfigs := make(map[string]api.ProviderResource)

	files, dir, err := scanFolders(logger, dirs...)
	if err != nil {
		return providerConfigs, err
	}
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".yaml") {
			continue
		}
		name := strings.TrimSuffix(file.Name(), ".yaml")
		providerConfig, fileUsed, err := loadProvider(logger, validate, name, dir)
		if err != nil {
			return nil, err
		}

		fileName := fileUsed
		if fileName == "" {
			fileName = file.Name()
		}
		if providerConfig.Resource.ID == "" {
			logger.Warn("Provider config missing id, skipping", "file", fileName)
			continue
		}

		providerConfigs[providerConfig.Resource.ID] = *providerConfig
		logger.Info("Provider loaded", "provider_id", providerConfig.Resource.ID, "file", fileName)
	}

	return providerConfigs, nil
}

func LoadCollectionConfigs(logger *slog.Logger, validate *validator.Validate, dirs ...string) (map[string]api.CollectionResource, error) {
	if !hasExplicitConfigDir(dirs) {
		dirs = []string{}
		for _, dir := range configLookup {
			dirs = append(dirs, dir+"/collections")
		}
	} else {
		dirs = []string{dirs[0] + "/collections"}
	}

	collectionConfigs := make(map[string]api.CollectionResource)

	files, dir, err := scanFolders(logger, dirs...)
	if err != nil {
		return collectionConfigs, err
	}
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".yaml") {
			continue
		}
		name := strings.TrimSuffix(file.Name(), ".yaml")
		collectionConfig, fileUsed, err := loadCollection(logger, validate, name, dir)
		if err != nil {
			return nil, err
		}
		fileName := fileUsed
		if fileName == "" {
			fileName = file.Name()
		}
		if collectionConfig.Resource.ID == "" {
			logger.Warn("Collection config missing id, skipping", "file", fileName)
			continue
		}

		collectionConfigs[collectionConfig.Resource.ID] = *collectionConfig
		logger.Info("Collection loaded", "collection_id", collectionConfig.Resource.ID, "file", fileName)
	}

	return collectionConfigs, nil
}

// LoadConfig loads configuration using a two-tier system with Viper. This implements
// a sophisticated loading strategy that supports cascading configuration values and
// multiple sources.
//
// Configuration loading order (later sources override earlier ones):
//  1. config.yaml (config/config.yaml) - Configuration loaded first
//  2. Environment variables - Mapped via env_mappings configuration
//  3. Secrets from files - Mapped via secrets.mappings with secrets.dir
//
// Configuration supports:
//   - Environment variable mapping: Define in env_mappings (e.g., PORT → service.port)
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
func LoadConfig(logger *slog.Logger, version string, build string, buildDate string, gitHash string, dirs ...string) (*Config, error) {
	logger.Info("Start reading configuration", "version", version, "build", build, "build_date", buildDate, "dirs", dirs)

	if !hasExplicitConfigDir(dirs) {
		dirs = configLookup
	}

	configValues, err := readConfig(logger, "config", "yaml", dirs...)
	if err != nil {
		logger.Error("Failed to read configuration file config.yaml", "error", err.Error(), "dirs", dirs)
		return nil, err
	}

	// If CONFIG_PATH is set, load the operator-mounted config and apply its
	// top-level keys over the bundled defaults. This replaces (not deep-merges)
	// sections like secrets, so bundled secret mappings don't leak through.
	// Values not present in the operator config (e.g. service) are preserved.
	if configPath := os.Getenv("CONFIG_PATH"); configPath != "" {
		logger.Info("CONFIG_PATH set, applying operator config", "config_path", configPath)
		operatorConfig := viper.New()
		operatorConfig.SetConfigFile(configPath)
		if err := operatorConfig.ReadInConfig(); err != nil {
			logger.Error("Failed to read CONFIG_PATH config", "config_path", configPath, "error", err.Error())
			return nil, err
		}
		for key, value := range operatorConfig.AllSettings() {
			configValues.Set(key, value)
		}
		logger.Info("Applied operator config", "config_path", configPath)
	}

	// set up the secrets from the secrets directory
	var redactedFields []string
	secrets := SecretMap{}
	if secretsSub := configValues.Sub("secrets"); secretsSub != nil {
		if err := secretsSub.Unmarshal(&secrets); err != nil {
			logger.Error("Failed to unmarshal secret mappings", "error", err.Error())
			return nil, err
		}
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
					redactedFields = append(redactedFields, fieldName)
					logger.Info("Set secret", "field_name", fieldName)
				}
			}
		}
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
	conf.Service.GitHash = gitHash
	logger.Info("End reading configuration", "config", RedactedJSON(conf, redactedFields))
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

// RedactedJSON serialises v to JSON, then replaces any values whose dotted
// field path matches a redacted field with "[redacted]". Field paths use dots
// to denote nesting (e.g. "database.url" redacts the "url" key inside "database").
func RedactedJSON(v any, fields []string) string {
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	if len(fields) == 0 {
		return string(data)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return string(data)
	}
	for _, field := range fields {
		redactField(raw, strings.Split(field, "."))
	}
	out, err := json.Marshal(raw)
	if err != nil {
		return string(data)
	}
	return string(out)
}

func redactField(m map[string]any, path []string) {
	if len(path) == 0 {
		return
	}
	// case-insensitive key lookup
	var matchedKey string
	for k := range m {
		if strings.EqualFold(k, path[0]) {
			matchedKey = k
			break
		}
	}
	if matchedKey == "" {
		return
	}
	if len(path) == 1 {
		m[matchedKey] = sanitiseValue(m[matchedKey])
		return
	}
	if sub, ok := m[matchedKey].(map[string]any); ok {
		redactField(sub, path[1:])
	}
}

// sanitiseValue strips credentials from URL strings that contain embedded
// userinfo. URLs without credentials and non-URL values are replaced with
// "[redacted]".
func sanitiseValue(v any) string {
	s, ok := v.(string)
	if !ok {
		return "[redacted]"
	}
	parsed, err := url.Parse(s)
	if err != nil || parsed.Scheme == "" {
		return "[redacted]"
	}
	if parsed.User == nil {
		return "[redacted]"
	}
	parsed.User = url.User(parsed.User.Username())
	return parsed.String()
}
