package rbac

import (
	"fmt"

	"github.com/spf13/viper"
)

type AuthorizationConfig struct {
	Authorization Authorization `yaml:"authorization" mapstructure:"authorization"`
}

type Authorization struct {
	Endpoints []Endpoint `yaml:"endpoints" mapstructure:"endpoints"`
}

type Endpoint struct {
	Path     string    `yaml:"path" mapstructure:"path"`
	Mappings []Mapping `yaml:"mappings" mapstructure:"mappings"`
}

type Mapping struct {
	Methods   []string       `yaml:"methods" mapstructure:"methods"`
	Resources []ResourceRule `yaml:"resources" mapstructure:"resources"`
}

type ResourceRule struct {
	Rewrites           Rewrite            `yaml:"rewrites" mapstructure:"rewrites"`
	ResourceAttributes ResourceAttributes `yaml:"resourceAttributes" mapstructure:"resourceAttributes"`
}

type Rewrite struct {
	ByHttpHeader  *ByHttpHeader  `yaml:"byHttpHeader,omitempty" mapstructure:"byHttpHeader"`
	ByQueryString *ByQueryString `yaml:"byQueryString,omitempty" mapstructure:"byQueryString"`
}

type ByHttpHeader struct {
	Name string `yaml:"name" mapstructure:"name"`
}

type ByQueryString struct {
	Name string `yaml:"name" mapstructure:"name"`
}

type ResourceAttributes struct {
	Namespace   string `yaml:"namespace" mapstructure:"namespace"`
	APIGroup    string `yaml:"apiGroup" mapstructure:"apiGroup"`
	APIVersion  string `yaml:"apiVersion" mapstructure:"apiVersion"`
	Resource    string `yaml:"resource" mapstructure:"resource"`
	Name        string `yaml:"name" mapstructure:"name"`
	Subresource string `yaml:"subresource" mapstructure:"subresource"`
	Verb        string `yaml:"verb" mapstructure:"verb"`
}

func loadAuthorizerConfig(filePath string) (*AuthorizationConfig, error) {

	v := viper.New()
	v.SetConfigFile(filePath)
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("Cannot load authorized config from file (%q): %v", filePath, err)
	}

	var cfg AuthorizationConfig
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("Cannot parse authorized config from file (%q): %v", filePath, err)
	}
	return &cfg, nil
}
