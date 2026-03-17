package config

import (
	"crypto/tls"
	"time"
)

type SidecarConfig struct {
	Port             int                     `mapstructure:"port,omitempty"`
	BaseURL          string                  `mapstructure:"base_url,omitempty"`
	EvalHub          *EvalHubClientConfig    `mapstructure:"eval_hub"`
	MLFlow           *SidecarMLFlowConfig    `mapstructure:"mlflow,omitempty"`
	OCI              *SidecarOCIConfig      `mapstructure:"oci,omitempty"`
	SidecarContainer *SidecarContainerConfig `mapstructure:"sidecar_container,omitempty"`
}

// SidecarOCIConfig holds sidecar OCI/registry proxy settings (host from configmap).
type SidecarOCIConfig struct {
	Host               string        `mapstructure:"host,omitempty"`                  // OCI registry host (e.g. https://registry.example.com:5000)
	Repository         string        `mapstructure:"repository,omitempty"`             // optional scope repository (e.g. namespace/repo)
	CACertPath         string        `mapstructure:"ca_cert_path,omitempty"`          // optional PEM CA for registry TLS
	InsecureSkipVerify bool          `mapstructure:"insecure_skip_verify,omitempty"`   // skip TLS verify for registry (e.g. self-signed)
	HTTPTimeout        time.Duration `mapstructure:"http_timeout,omitempty"`           // HTTP client timeout for registry requests (e.g. 30s)
}

type EvalHubClientConfig struct {
	HTTPTimeout        time.Duration `mapstructure:"http_timeout"`
	CACertPath         string        `mapstructure:"ca_cert_path,omitempty"`
	InsecureSkipVerify bool          `mapstructure:"insecure_skip_verify,omitempty"`
	Token              string        `mapstructure:"token,omitempty"`
	TokenCacheTimeout  time.Duration `mapstructure:"token_cache_timeout"`
	TLSConfig          *tls.Config   // set at runtime, not from config file
}

// SidecarMLFlowConfig holds sidecar-specific MLflow settings (e.g. token cache TTL).
type SidecarMLFlowConfig struct {
	TokenCacheTimeout time.Duration `mapstructure:"token_cache_timeout"`
}

type ServiceAccountConfig struct {
	Path     string `mapstructure:"path,omitempty"`
	FileName string `mapstructure:"file_name,omitempty"`
}

type SidecarContainerConfig struct {
	Image     string                `mapstructure:"image,omitempty"`
	Resources *ResourceRequirements `mapstructure:"resources,omitempty"`
}

type ResourceRequirements struct {
	Requests *ResourceRequirementDef `mapstructure:"requests,omitempty"`
	Limits   *ResourceRequirementDef `mapstructure:"limits,omitempty"`
}

type ResourceRequirementDef struct {
	CPU    string `mapstructure:"cpu,omitempty"`
	Memory string `mapstructure:"memory,omitempty"`
}
