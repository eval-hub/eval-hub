package api

// Provider contains the configuration details for an evaluation provider.
type ProviderResource struct {
	ProviderID   string              `mapstructure:"provider_id" yaml:"provider_id" json:"provider_id"`
	ProviderName string              `mapstructure:"provider_name" yaml:"provider_name" json:"provider_name"`
	Description  string              `mapstructure:"description" yaml:"description" json:"description"`
	ProviderType string              `mapstructure:"provider_type" yaml:"provider_type" json:"provider_type"`
	BaseURL      *string             `mapstructure:"base_url" yaml:"base_url" json:"base_url"`
	Benchmarks   []BenchmarkResource `mapstructure:"benchmarks" yaml:"benchmarks" json:"benchmarks"`
}

// ProviderResourceList represents response for listing providers
type ProviderResourceList struct {
	TotalCount int                 `json:"total_count"`
	Items      []*ProviderResource `json:"items"`
}
