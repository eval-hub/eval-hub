package api

// CollectionConfig represents request to create a collection
type CollectionConfig struct {
	Name         string            `mapstructure:"name" json:"name" validate:"required"`
	Description  string            `mapstructure:"description" json:"description,omitempty" validate:"omitempty,max=1024,min=1"`
	Category     string            `mapstructure:"category" json:"category" validate:"required,max=128,min=1"`
	Tags         []string          `mapstructure:"tags" json:"tags,omitempty" validate:"omitempty,dive,tagname"`
	Custom       *map[string]any   `mapstructure:"custom" json:"custom,omitempty"`
	PassCriteria PassCriteria      `mapstructure:"pass_criteria" json:"pass_criteria,omitempty"`
	Benchmarks   []BenchmarkConfig `mapstructure:"benchmarks" json:"benchmarks" validate:"required,min=1,dive"`
}

// CollectionResource represents collection resource
type CollectionResource struct {
	Resource Resource `json:"resource"`
	CollectionConfig
}

// CollectionResourceList represents list of collection resources with pagination
type CollectionResourceList struct {
	Page
	Items []CollectionResource `json:"items"`
}
