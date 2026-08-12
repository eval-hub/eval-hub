package config

import (
	"crypto/tls"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

const (
	DefaultSidecarPort    = 8080
	DefaultSidecarBaseURL = "http://localhost:8080"
)

type SidecarConfig struct {
	LocalMode        bool                    `mapstructure:"local_mode,omitempty" json:"local_mode,omitempty"`
	BaseURL          string                  `mapstructure:"base_url,omitempty" json:"base_url,omitempty"`
	Port             int32                   `mapstructure:"-" json:"-"` // derived from BaseURL by ResolvePort; never serialised
	EvalHub          *EvalHubClientConfig    `mapstructure:"eval_hub" json:"eval_hub,omitempty"`
	MLFlow           *SidecarMLFlowConfig    `mapstructure:"mlflow,omitempty" json:"mlflow,omitempty"`
	OCI              *SidecarOCIConfig       `mapstructure:"oci,omitempty" json:"oci,omitempty"`
	Model            *SidecarModelConfig     `mapstructure:"model,omitempty" json:"model,omitempty"`
	InitContainer    *InitContainerConfig    `mapstructure:"init_container,omitempty" json:"init_container,omitempty"`
	SidecarContainer *SidecarContainerConfig `mapstructure:"sidecar_container,omitempty" json:"sidecar_container,omitempty"`
	OTEL             *OTELConfig             `mapstructure:"otel,omitempty" json:"otel,omitempty"`
}

// InitContainerConfig holds metadata written by eval-hub for the init container phase.
// The sidecar reads this at startup to configure init-container-specific behaviour.
type InitContainerConfig struct {
	IsGitJob bool `mapstructure:"is_git_job,omitempty" json:"is_git_job,omitempty"`
}

// EffectiveBaseURL returns BaseURL if non-empty, otherwise DefaultSidecarBaseURL.
func (sc *SidecarConfig) EffectiveBaseURL() string {
	if sc.BaseURL != "" {
		return sc.BaseURL
	}
	return DefaultSidecarBaseURL
}

// ResolvePort extracts the listen port from BaseURL and stores it in Port.
// When BaseURL is empty both fields are left at their zero values; each
// consumer module is responsible for falling back to the defaults.
// A non-empty BaseURL must use http/https, include a hostname, and carry
// an explicit port (required for Kubernetes probe/container port alignment).
// Call once after loading config to fail fast on malformed URLs.
func (sc *SidecarConfig) ResolvePort() error {
	if sc.BaseURL == "" {
		return nil
	}
	u, err := url.Parse(sc.BaseURL)
	if err != nil {
		return fmt.Errorf("invalid sidecar base URL %q: %w", sc.BaseURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("sidecar base URL %q must use http or https scheme", sc.BaseURL)
	}
	if u.Hostname() == "" {
		return fmt.Errorf("sidecar base URL %q must include a hostname", sc.BaseURL)
	}
	portStr := u.Port()
	if portStr == "" {
		return fmt.Errorf("sidecar base URL %q must include an explicit port", sc.BaseURL)
	}
	port, err := strconv.ParseInt(portStr, 10, 32)
	if err != nil {
		return fmt.Errorf("invalid port in sidecar base URL %q: %w", sc.BaseURL, err)
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("sidecar port %d out of range (1-65535)", port)
	}
	sc.Port = int32(port)
	return nil
}

// SidecarModelConfig holds the model credential-injection proxy settings written into
// sidecar_config.json. The sidecar reads this at startup to configure the model reverse
// proxy: URL is the model endpoint; AuthSecretMountPath is where the sidecar mounts the
// model credential secret. The adapter container only sees the ephemeral ref secret
// with placeholder values like "api-key:ref".
type SidecarModelConfig struct {
	URL                 string        `mapstructure:"url,omitempty" json:"url,omitempty"`
	AuthSecretMountPath string        `mapstructure:"auth_secret_mount_path,omitempty" json:"auth_secret_mount_path,omitempty"`
	HTTPTimeout         time.Duration `mapstructure:"http_timeout,omitempty" json:"http_timeout,omitempty"`
	InsecureSkipVerify  bool          `mapstructure:"insecure_skip_verify,omitempty" json:"insecure_skip_verify,omitempty"`
}

// SidecarOCIConfig holds optional TLS/timeout overrides for the OCI registry HTTP client.
// Whether the OCI reverse proxy runs is determined from the job spec (exports.oci), not from
// the presence of this block; when nil, the sidecar uses defaults for registry TLS.
type SidecarOCIConfig struct {
	CACertPath  string        `mapstructure:"ca_cert_path,omitempty" json:"ca_cert_path,omitempty"` // optional PEM CA for registry TLS
	HTTPTimeout time.Duration `mapstructure:"http_timeout,omitempty" json:"http_timeout,omitempty"` // HTTP client timeout for registry requests (e.g. 30s)
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
// CACertPath may also be set under sidecar.mlflow in YAML; when writing sidecar_config.json
// for job pods, that field is overwritten from top-level mlflow config. TLS verification
// is always enabled for MLflow; use CACertPath for custom CAs.
type SidecarMLFlowConfig struct {
	TrackingURI       string        `mapstructure:"tracking_uri,omitempty" json:"tracking_uri,omitempty"`
	TokenPath         string        `mapstructure:"token_path,omitempty" json:"token_path,omitempty"`
	Workspace         string        `mapstructure:"workspace,omitempty" json:"workspace,omitempty"`
	TokenCacheTimeout time.Duration `mapstructure:"token_cache_timeout" json:"token_cache_timeout,omitempty"`
	HTTPTimeout       time.Duration `mapstructure:"http_timeout" json:"http_timeout,omitempty"`
	CACertPath        string        `mapstructure:"ca_cert_path,omitempty" json:"ca_cert_path,omitempty"`
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
