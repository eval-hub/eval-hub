package config

import (
	"crypto/tls"
	"time"
)

type SidecarConfig struct {
	Port             int                     `mapstructure:"port,omitempty" json:"port,omitempty"`
	BaseURL          string                  `mapstructure:"base_url,omitempty" json:"base_url,omitempty"`
	EvalHub          *EvalHubClientConfig    `mapstructure:"eval_hub" json:"eval_hub,omitempty"`
	MLFlow           *SidecarMLFlowConfig    `mapstructure:"mlflow,omitempty" json:"mlflow,omitempty"`
	OCI              *SidecarOCIConfig       `mapstructure:"oci,omitempty" json:"oci,omitempty"`
	SidecarContainer *SidecarContainerConfig `mapstructure:"sidecar_container,omitempty" json:"sidecar_container,omitempty"`
}

// SidecarOCIConfig holds sidecar OCI/registry proxy settings (host from configmap).
type SidecarOCIConfig struct {
	CACertPath         string        `mapstructure:"ca_cert_path,omitempty" json:"ca_cert_path,omitempty"`                 // optional PEM CA for registry TLS
	InsecureSkipVerify bool          `mapstructure:"insecure_skip_verify,omitempty" json:"insecure_skip_verify,omitempty"` // skip TLS verify for registry (e.g. self-signed)
	HTTPTimeout        time.Duration `mapstructure:"http_timeout,omitempty" json:"http_timeout,omitempty"`                 // HTTP client timeout for registry requests (e.g. 30s)
}

type EvalHubClientConfig struct {
	BaseURL            string        `mapstructure:"base_url,omitempty" json:"base_url,omitempty"` // eval-hub API base (sidecar proxy upstream)
	HTTPTimeout        time.Duration `mapstructure:"http_timeout" json:"http_timeout,omitempty"`
	CACertPath         string        `mapstructure:"ca_cert_path,omitempty" json:"ca_cert_path,omitempty"`
	InsecureSkipVerify bool          `mapstructure:"insecure_skip_verify,omitempty" json:"insecure_skip_verify,omitempty"`
	Token              string        `mapstructure:"token,omitempty" json:"-"`
	TokenCacheTimeout  time.Duration `mapstructure:"token_cache_timeout" json:"token_cache_timeout,omitempty"`
	TLSConfig          *tls.Config   `json:"-"` // set at runtime, not from config file
}

// SidecarMLFlowConfig holds sidecar-specific MLflow settings (e.g. token cache TTL).
// CACertPath and InsecureSkipVerify may also be set under sidecar.mlflow in YAML; when writing
// sidecar_config.json for job pods, those fields are overwritten from top-level mlflow config.
type SidecarMLFlowConfig struct {
	TrackingURI        string        `mapstructure:"tracking_uri,omitempty" json:"tracking_uri,omitempty"`
	TokenPath          string        `mapstructure:"token_path,omitempty" json:"token_path,omitempty"`
	Workspace          string        `mapstructure:"workspace,omitempty" json:"workspace,omitempty"`
	TokenCacheTimeout  time.Duration `mapstructure:"token_cache_timeout" json:"token_cache_timeout,omitempty"`
	HTTPTimeout        time.Duration `mapstructure:"http_timeout" json:"http_timeout,omitempty"`
	CACertPath         string        `mapstructure:"ca_cert_path,omitempty" json:"ca_cert_path,omitempty"`
	InsecureSkipVerify bool          `mapstructure:"insecure_skip_verify,omitempty" json:"insecure_skip_verify,omitempty"`
}

type ServiceAccountConfig struct {
	Path     string `mapstructure:"path,omitempty"`
	FileName string `mapstructure:"file_name,omitempty"`
}

type SidecarContainerConfig struct {
	Image     string                `mapstructure:"image,omitempty" json:"image,omitempty"`
	Resources *ResourceRequirements `mapstructure:"resources,omitempty" json:"resources,omitempty"`
}

type ResourceRequirements struct {
	Requests *ResourceRequirementDef `mapstructure:"requests,omitempty" json:"requests,omitempty"`
	Limits   *ResourceRequirementDef `mapstructure:"limits,omitempty" json:"limits,omitempty"`
}

type ResourceRequirementDef struct {
	CPU    string `mapstructure:"cpu,omitempty" json:"cpu,omitempty"`
	Memory string `mapstructure:"memory,omitempty" json:"memory,omitempty"`
}
