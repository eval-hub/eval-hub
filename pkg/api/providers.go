package api

// Provider contains the configuration details for an evaluation provider.
type ProviderResource struct {
	ProviderID   string              `mapstructure:"provider_id" yaml:"provider_id" json:"provider_id"`
	ProviderName string              `mapstructure:"provider_name" yaml:"provider_name" json:"provider_name"`
	Description  string              `mapstructure:"description" yaml:"description" json:"description"`
	ProviderType string              `mapstructure:"provider_type" yaml:"provider_type" json:"provider_type"`
	BaseURL      *string             `mapstructure:"base_url" yaml:"base_url" json:"base_url"`
	Benchmarks   []BenchmarkResource `mapstructure:"benchmarks" yaml:"benchmarks" json:"benchmarks"`
	Runtime      *ProviderRuntime    `mapstructure:"runtime" yaml:"runtime" json:"-"`
}

// ProviderRuntime contains runtime configuration for Kubernetes jobs.
//
// Example YAML for provider configs:
//
//	runtime:
//	  adapter_image: "quay.io/eval-hub/adapter:latest"
//	  cpu_request: "250m"
//	  memory_request: "512Mi"
//	  cpu_limit: "1"
//	  memory_limit: "2Gi"
//	  default_env:
//	    - name: FOO
//	      value: "bar"
type ProviderRuntime struct {
	AdapterImage  string           `mapstructure:"adapter_image" yaml:"adapter_image"`
	CPURequest    string           `mapstructure:"cpu_request" yaml:"cpu_request"`
	MemoryRequest string           `mapstructure:"memory_request" yaml:"memory_request"`
	CPULimit      string           `mapstructure:"cpu_limit" yaml:"cpu_limit"`
	MemoryLimit   string           `mapstructure:"memory_limit" yaml:"memory_limit"`
	DefaultEnv    []ProviderEnvVar `mapstructure:"default_env" yaml:"default_env"`
}

// ProviderEnvVar captures environment variables for the job template.
type ProviderEnvVar struct {
	Name  string `mapstructure:"name" yaml:"name"`
	Value string `mapstructure:"value" yaml:"value"`
}

// ProviderResourceList represents response for listing providers
type ProviderResourceList struct {
	TotalCount int                `json:"total_count"`
	Items      []ProviderResource `json:"items,omitempty"`
}
