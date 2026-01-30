package api

// CollectionConfig represents request to create a collection
type CollectionConfig struct {
	Name        string   `json:"name"`
	Description *string  `json:"description,omitempty"`
	Benchmarks  []string `json:"benchmarks"`
}

// CollectionResource represents collection resource
type CollectionResource struct {
	Resource
	CollectionConfig
}

// CollectionResourceList represents list of collection resources with pagination
type CollectionResourceList struct {
	Page
	Items []CollectionResource `json:"items"`
}

type PublicCollectionResourceList struct {
	TotalCount int                  `json:"total_count"`
	Items      []CollectionResource `json:"items"`
}
