package api

// Provider contains the configuration details for an evaluation provider.
type ProviderResource struct {
	ProviderID   string              `mapstructure:"provider_id" yaml:"provider_id" json:"provider_id"`
	ProviderName string              `mapstructure:"provider_name" yaml:"provider_name" json:"provider_name"`
	Description  string              `mapstructure:"description" yaml:"description" json:"description"`
	ProviderType string              `mapstructure:"provider_type" yaml:"provider_type" json:"provider_type"`
	BaseURL      *string             `mapstructure:"base_url" yaml:"base_url" json:"base_url"`
	Benchmarks   []BenchmarkResource `mapstructure:"benchmarks" yaml:"benchmarks" json:"benchmarks"`
	Runtime      *Runtime            `mapstructure:"runtime" yaml:"runtime" json:"-"`
}

type Runtime struct {
	K8s   *K8sRuntime   `mapstructure:"k8s" yaml:"k8s" json:"k8s,omitempty"`
	Local *LocalRuntime `mapstructure:"local" yaml:"local" json:"local,omitempty"`
}

// ProviderRuntime contains runtime configuration for Kubernetes jobs.
//
// Example YAML for provider configs:
//
//	runtime:
//	  image: "quay.io/eval-hub/adapter:latest"
//	  entrypoint: "/path/to/program"
//	  cpu_request: "250m"
//	  memory_request: "512Mi"
//	  cpu_limit: "1"
//	  memory_limit: "2Gi"
//	  default_env:
//	    - name: FOO
//	      value: "bar"
type K8sRuntime struct {
	Image         string   `mapstructure:"image" yaml:"image"`
	Entrypoint    string   `mapstructure:"entrypoint" yaml:"entrypoint"`
	CPURequest    string   `mapstructure:"cpu_request" yaml:"cpu_request"`
	MemoryRequest string   `mapstructure:"memory_request" yaml:"memory_request"`
	CPULimit      string   `mapstructure:"cpu_limit" yaml:"cpu_limit"`
	MemoryLimit   string   `mapstructure:"memory_limit" yaml:"memory_limit"`
	Env           []EnvVar `mapstructure:"env" yaml:"env"`
}

type LocalRuntime struct {
}

// ProviderResourceList represents response for listing providers
type ProviderResourceList struct {
	TotalCount int                `json:"total_count"`
	Items      []ProviderResource `json:"items,omitempty"`
}
