package api

// Benchmark represents benchmark specification
type BenchmarkResource struct {
	BenchmarkId string   `mapstructure:"benchmark_id" yaml:"benchmark_id" json:"benchmark_id"`
	Name        string   `mapstructure:"name" yaml:"name" json:"name"`
	Description string   `mapstructure:"description" yaml:"description" json:"description"`
	Category    string   `mapstructure:"category" yaml:"category" json:"category"`
	Metrics     []string `mapstructure:"metrics" yaml:"metrics" json:"metrics"`
	NumFewShot  int      `mapstructure:"num_few_shot" yaml:"num_few_shot" json:"num_few_shot"`
	DatasetSize int      `mapstructure:"dataset_size" yaml:"dataset_size" json:"dataset_size"`
	Tags        []string `mapstructure:"tags" yaml:"tags" json:"tags"`
}

// BenchmarkResourceList represents list of benchmarks
type BenchmarkResourceList struct {
	TotalCount int                 `json:"total_count"`
	Items      []BenchmarkResource `json:"items"`
}
