package collectiondesign

import (
	"context"
	"fmt"

	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/evalhubclient"
)

// Page is the transport-independent result of a paginated catalog read.
type Page[T any] struct {
	Items      []T
	TotalCount int
}

// CatalogSource exposes only the reads needed by collection design. Callers
// must supply a source scoped to the current request's identity and context.
type CatalogSource interface {
	ListProviders(ctx context.Context, offset, limit int) (Page[api.ProviderResource], error)
	ListCollections(ctx context.Context, offset, limit int) (Page[api.CollectionResource], error)
}

type BenchmarkCatalogEntry struct {
	ID          string   `json:"id"`
	ProviderID  string   `json:"provider_id"`
	Name        string   `json:"name,omitempty"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Metrics     []string `json:"metrics,omitempty"`
}

type CollectionExample struct {
	ID           string                          `json:"id"`
	Name         string                          `json:"name"`
	Domains      []string                        `json:"domains,omitempty"`
	Description  string                          `json:"description,omitempty"`
	Tags         []string                        `json:"tags,omitempty"`
	PassCriteria *api.PassCriteria               `json:"pass_criteria,omitempty"`
	Benchmarks   []api.CollectionBenchmarkConfig `json:"benchmarks"`
}

type Catalog struct {
	Benchmarks []BenchmarkCatalogEntry
	Examples   []CollectionExample
}

// Gather reads the complete catalog for the existing MCP prompt and tool.
// The REST generator can use CatalogSource's paginated methods selectively.
func Gather(ctx context.Context, source CatalogSource, options Options) (*Catalog, error) {
	providers, err := readAllPages(ctx, source.ListProviders)
	if err != nil {
		return nil, fmt.Errorf("fetching benchmark catalog: %w", err)
	}

	benchmarks := make([]BenchmarkCatalogEntry, 0)
	for _, provider := range providers {
		if !options.allowsProvider(provider.Resource.ID) {
			continue
		}
		for _, benchmark := range provider.Benchmarks {
			entry := BenchmarkCatalogEntry{
				ID:          benchmark.ID,
				ProviderID:  provider.Resource.ID,
				Name:        benchmark.Name,
				Description: benchmark.Description,
				Tags:        benchmark.Tags,
			}
			if len(benchmark.Metrics) > 0 {
				entry.Metrics = benchmark.Metrics
			}
			benchmarks = append(benchmarks, entry)
		}
	}
	if len(benchmarks) == 0 {
		if options.ProviderFilter != "" {
			return nil, fmt.Errorf("benchmark catalog is empty for provider filter %q; check that the eval-hub service has these providers loaded", options.ProviderFilter)
		}
		return nil, fmt.Errorf("benchmark catalog is empty; check that the eval-hub service has providers loaded")
	}

	collections, err := readAllPages(ctx, source.ListCollections)
	if err != nil {
		return nil, fmt.Errorf("fetching collection examples: %w", err)
	}
	examples := make([]CollectionExample, 0)
	for _, collection := range collections {
		if collection.Resource.Owner != "system" {
			continue
		}
		selected := collection.Benchmarks
		if options.ProviderFilter != "" {
			selected = make([]api.CollectionBenchmarkConfig, 0)
			for _, benchmark := range collection.Benchmarks {
				if options.allowsProvider(benchmark.ProviderID) {
					selected = append(selected, benchmark)
				}
			}
			if len(selected) == 0 {
				continue
			}
		}
		examples = append(examples, CollectionExample{
			ID:           collection.Resource.ID,
			Name:         collection.Name,
			Domains:      collection.Domains,
			Description:  collection.Description,
			Tags:         collection.Tags,
			PassCriteria: collection.PassCriteria,
			Benchmarks:   selected,
		})
	}
	return &Catalog{Benchmarks: benchmarks, Examples: examples}, nil
}

func readAllPages[T any](ctx context.Context, read func(context.Context, int, int) (Page[T], error)) ([]T, error) {
	const pageSize = evalhubclient.DefaultListPageLimit
	var all []T
	for offset := 0; ; offset += pageSize {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		page, err := read(ctx, offset, pageSize)
		if err != nil {
			return nil, err
		}
		all = append(all, page.Items...)
		if len(all) >= page.TotalCount || len(page.Items) < pageSize {
			break
		}
	}
	return all, ctx.Err()
}
