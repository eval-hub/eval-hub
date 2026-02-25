package api

// CollectionConfig represents request to create a collection
type CollectionConfig struct {
	Name         string            `json:"name" validate:"required"`
	Description  *string           `json:"description,omitempty" validate:"required"`
	Tags         []string          `json:"tags,omitempty"`
	Custom       *map[string]any   `json:"custom,omitempty"`
	PassCriteria PassCriteria      `json:"pass_criteria,omitempty"`
	Benchmarks   []BenchmarkConfig `json:"benchmarks" validate:"required,min=1,dive"`
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
