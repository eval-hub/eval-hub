package api

// CollectionConfig represents request to create a collection
type CollectionConfig struct {
	Name         string            `json:"name"`
	Description  *string           `json:"description,omitempty"`
	Tags         []string          `json:"tags,omitempty"`
	Custom       *map[string]any   `json:"custom,omitempty"`
	PassCriteria PassCriteria      `json:"pass_criteria,omitempty"`
	Benchmarks   []BenchmarkConfig `json:"benchmarks"`
}

// CollectionResource represents collection resource
type CollectionResource struct {
	Resource Resource `json:"resource"`
	Type     string   `json:"type" enum:"system,owned"`
	CollectionConfig
}

// CollectionResourceList represents list of collection resources with pagination
type CollectionResourceList struct {
	Page
	Items []CollectionResource `json:"items"`
}
