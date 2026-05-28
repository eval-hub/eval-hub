package api

type BenchmarkResource struct {
	ID           string        `mapstructure:"id" yaml:"id" json:"id"`
	URL          string        `mapstructure:"url" yaml:"url" json:"url,omitempty"`
	Name         string        `mapstructure:"name" yaml:"name" json:"name"`
	Description  string        `mapstructure:"description" yaml:"description" json:"description,omitempty" validate:"omitempty,max=1024,min=1"`
	Category     string        `mapstructure:"category" yaml:"category" json:"category"`
	Metrics      []string      `mapstructure:"metrics" yaml:"metrics" json:"metrics,omitempty"`
	NumFewShot   int           `mapstructure:"num_few_shot" yaml:"num_few_shot" json:"num_few_shot"`
	DatasetSize  int           `mapstructure:"dataset_size" yaml:"dataset_size" json:"dataset_size"`
	Tags         []string      `mapstructure:"tags" yaml:"tags" json:"tags,omitempty"`
	PrimaryScore *PrimaryScore `mapstructure:"primary_score" yaml:"primary_score" json:"primary_score,omitempty"`
	PassCriteria *PassCriteria `mapstructure:"pass_criteria" yaml:"pass_criteria" json:"pass_criteria,omitempty" validate:"omitempty"`
}

type ProviderConfig struct {
	Name        string              `mapstructure:"name" yaml:"name" json:"name"`
	Description string              `mapstructure:"description" yaml:"description" json:"description,omitempty" validate:"omitempty,max=1024,min=1"`
	Title       string              `mapstructure:"title" yaml:"title" json:"title"`
	Tags        []string            `mapstructure:"tags" yaml:"tags" json:"tags,omitempty" validate:"omitempty,dive,tagname"`
	Benchmarks  []BenchmarkResource `mapstructure:"benchmarks" yaml:"benchmarks" json:"benchmarks" validate:"dive"`
	Runtime     *Runtime            `mapstructure:"runtime" yaml:"runtime" json:"runtime,omitempty"`
}

type ProviderResource struct {
	Resource Resource `json:"resource"`
	ProviderConfig
}

type Runtime struct {
	K8s   *K8sRuntime   `mapstructure:"k8s" yaml:"k8s" json:"k8s,omitempty"`
	Local *LocalRuntime `mapstructure:"local" yaml:"local" json:"local,omitempty"`
}

// GPUConfig declares the GPU resources required by an adapter.
// Resource is the Kubernetes extended resource name (e.g. "nvidia.com/gpu", "amd.com/gpu").
// When Resource is omitted, no specific GPU resource is requested in the pod spec —
// node selection is left to Kueue ResourceFlavors or cluster defaults.
// Count is the number of GPU units to request; must be ≥ 1 when GPU is specified.
//
// Kubernetes requires requests == limits for GPU extended resources, so both are set to Count.
// When GPU is nil or Count is 0 the adapter is scheduled as CPU-only.
//
// NodeSelector is an optional map of node label key→value pairs added to the evaluation pod
// to target a specific GPU model or node pool (e.g. {"nvidia.com/gpu.product": "NVIDIA-H100-SXM5-80GB"}).
// NodeSelector is ignored when the evaluation job is submitted with a queue — in that case
// Kueue's ResourceFlavors govern node selection.
type GPUConfig struct {
	Resource     string            `mapstructure:"resource" yaml:"resource" json:"resource,omitempty"`
	Count        int               `mapstructure:"count" yaml:"count" json:"count"`
	NodeSelector map[string]string `mapstructure:"node_selector" yaml:"node_selector" json:"node_selector,omitempty"`
}

// ProviderRuntime contains runtime configuration for Kubernetes jobs.
//
// Example YAML for provider configs:
//
//	runtime:
//	  image: "quay.io/evalhub/adapter:latest"
//	  entrypoint:
//	    - "/path/to/program"
//	  cpu_request: "250m"
//	  memory_request: "512Mi"
//	  cpu_limit: "1"
//	  memory_limit: "2Gi"
//	  gpu:
//	    resource: nvidia.com/gpu         # omit to leave GPU resource unspecified (any GPU)
//	    count: 1
//	    node_selector:                   # optional; ignored when a queue is specified
//	      nvidia.com/gpu.product: NVIDIA-H100-SXM5-80GB
//	  default_env:
//	    - name: FOO
//	      value: "bar"
type K8sRuntime struct {
	Image         string   `mapstructure:"image" yaml:"image"`
	Entrypoint    []string `mapstructure:"entrypoint" yaml:"entrypoint"`
	CPURequest    string   `mapstructure:"cpu_request" yaml:"cpu_request"`
	MemoryRequest string   `mapstructure:"memory_request" yaml:"memory_request"`
	CPULimit      string   `mapstructure:"cpu_limit" yaml:"cpu_limit"`
	MemoryLimit   string   `mapstructure:"memory_limit" yaml:"memory_limit"`
	// GPU declares the GPU resource requirement for this adapter. Omit entirely for CPU-only
	// adapters — existing adapters are unaffected.
	GPU *GPUConfig `mapstructure:"gpu" yaml:"gpu" json:"gpu,omitempty"`
	Env []EnvVar   `mapstructure:"env" yaml:"env"`
}

type LocalRuntime struct {
	Command string   `mapstructure:"command" yaml:"command" json:"command,omitempty"`
	Env     []EnvVar `mapstructure:"env" yaml:"env" json:"env,omitempty"`
}

// ProviderResourceList represents response for listing providers
type ProviderResourceList struct {
	Page
	Items []ProviderResource `json:"items"`
}
