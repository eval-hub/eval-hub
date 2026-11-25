package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/eval-hub/eval-hub/internal/agent/loop"
	"github.com/eval-hub/eval-hub/internal/collectiondesign"
	"github.com/eval-hub/eval-hub/pkg/api"
)

const (
	searchBenchmarks   = "search_benchmarks"
	getProviderDetails = "get_provider_details"
	listCollections    = "list_collections"
	toolResultBytes    = 1920
	defaultToolPage    = 3
)

type catalogTools struct {
	source  Catalog
	options collectiondesign.Options
}

type toolPage struct {
	Offset int `json:"offset"`
	Limit  int `json:"limit"`
}

func (page toolPage) limit() int {
	if page.Limit == 0 {
		return defaultToolPage
	}
	return page.Limit
}

type searchArgs struct {
	toolPage
	Query      string `json:"query"`
	ProviderID string `json:"provider_id"`
	Domain     string `json:"domain"`
	Task       string `json:"task"`
	Tag        string `json:"tag"`
	Capability string `json:"capability"`
}

type providerArgs struct {
	toolPage
	ProviderID string `json:"provider_id"`
}

type benchmarkSummary struct {
	ProviderID   string            `json:"provider_id"`
	ID           string            `json:"id"`
	Name         string            `json:"name,omitempty"`
	Description  string            `json:"description,omitempty"`
	Domains      []string          `json:"domains,omitempty"`
	Tasks        []string          `json:"tasks,omitempty"`
	Modalities   []string          `json:"modalities,omitempty"`
	Targets      []string          `json:"evaluation_targets,omitempty"`
	Tags         []string          `json:"tags,omitempty"`
	Metrics      []string          `json:"metrics,omitempty"`
	PrimaryScore *api.PrimaryScore `json:"primary_score,omitempty"`
	AgentSummary string            `json:"interpretation,omitempty"`
}

type referenceSummary struct {
	ID         string             `json:"id"`
	Name       string             `json:"name,omitempty"`
	Domains    []string           `json:"domains,omitempty"`
	System     bool               `json:"system"`
	Benchmarks []benchmarkSummary `json:"benchmarks,omitempty"`
}

func (benchmark benchmarkSummary) compact() benchmarkSummary {
	return benchmarkSummary{
		ProviderID: benchmark.ProviderID, ID: benchmark.ID,
		Name: benchmark.Name, PrimaryScore: benchmark.PrimaryScore,
	}
}

func (benchmark benchmarkSummary) identity() benchmarkSummary {
	return benchmarkSummary{ProviderID: benchmark.ProviderID, ID: benchmark.ID}
}

func (reference referenceSummary) identity() referenceSummary {
	return referenceSummary{ID: reference.ID, System: reference.System}
}

func (tools catalogTools) registry() (*loop.Registry, error) {
	const page = `"offset":{"type":"integer","minimum":0,"maximum":10000},"limit":{"type":"integer","minimum":1,"maximum":5}`
	const provider = `"provider_id":{"type":"string","pattern":"^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$"}`
	const slug = `{"type":"string","pattern":"^[A-Za-z0-9_][A-Za-z0-9_.-]{0,79}$"}`
	const query = `{"type":"string","maxLength":160,"pattern":"^[^:/?#%\\\\\\r\\n]*$"}`
	return loop.NewRegistry(
		functionTool(searchBenchmarks, "Search benchmark metadata in the visible catalog. Use a focused query or a provider, domain, task, tag, or capability filter. Follow next_offset for more results.",
			`{"type":"object","additionalProperties":false,"properties":{`+page+`,`+provider+`,"query":`+query+`,"domain":`+slug+`,"task":`+slug+`,"tag":`+slug+`,"capability":`+slug+`}}`, tools.search),
		functionTool(getProviderDetails, "Inspect one visible provider by ID. Use for benchmark metrics, score direction, evaluation targets, and interpretation. Follow next_offset for additional benchmarks.",
			`{"type":"object","additionalProperties":false,"required":["provider_id"],"properties":{`+page+`,`+provider+`}}`, tools.provider),
		functionTool(listCollections, "List visible collections and a few of their benchmark references. Use system collections as calibration examples. Follow next_offset for more results.",
			`{"type":"object","additionalProperties":false,"properties":{`+page+`}}`, tools.collections),
	)
}

func functionTool(name, description, schema string, handler loop.ToolHandler) loop.Tool {
	return loop.Tool{
		Definition: loop.ToolDefinition{Type: "function", Function: loop.FunctionMetadata{
			Name: name, Description: description, Parameters: json.RawMessage(schema),
		}},
		ProgressLabel: "Catalog lookup",
		Handler:       handler,
	}
}

func (tools catalogTools) search(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var args searchArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	if args.ProviderID != "" && !allowedProvider(tools.options, args.ProviderID) {
		return nil, errors.New("provider is outside the request filter")
	}
	providers, err := readProviders(ctx, tools.source)
	if err != nil {
		return nil, err
	}
	query := normalizedTerms(args.Query)
	var candidates []candidate
	for _, provider := range providers {
		id := provider.Resource.ID
		if providerUnavailable(provider) || !allowedProvider(tools.options, id) || (args.ProviderID != "" && args.ProviderID != id) {
			continue
		}
		for _, benchmark := range provider.Benchmarks {
			if !matchesFilters(benchmark, args) {
				continue
			}
			if score := relevance(query, benchmark); score > 0 {
				candidates = append(candidates, candidate{benchmarkKey{id, benchmark.ID}, benchmark, score})
			}
		}
	}
	slices.SortFunc(candidates, compareCandidates)
	return boundedPage(args.toolPage, len(candidates), func(i int) benchmarkSummary {
		return candidates[i].summary(false)
	}, "", benchmarkSummary.compact, benchmarkSummary.identity)
}

func matchesFilters(benchmark api.BenchmarkResource, args searchArgs) bool {
	has := func(list []string, value string) bool { return value == "" || slices.Contains(list, value) }
	return has(benchmark.Domains, args.Domain) && has(benchmark.Tasks, args.Task) && has(benchmark.Tags, args.Tag) &&
		has(slices.Concat(benchmark.EvaluationTargets, benchmark.Modalities, benchmark.Tasks), args.Capability)
}

func (tools catalogTools) provider(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var args providerArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	if !allowedProvider(tools.options, args.ProviderID) {
		return nil, errors.New("provider is outside the request filter")
	}
	provider, err := tools.source.GetProvider(ctx, args.ProviderID)
	if err != nil {
		return nil, err
	}
	if provider.Resource.ID != args.ProviderID || providerUnavailable(provider) {
		return nil, errors.New("provider is unavailable")
	}
	var overview string
	if provider.Agent != nil {
		overview = clip(provider.Agent.Summary, 160)
	}
	return boundedPage(args.toolPage, len(provider.Benchmarks), func(i int) benchmarkSummary {
		benchmark := provider.Benchmarks[i]
		return (candidate{key: benchmarkKey{args.ProviderID, benchmark.ID}, benchmark: benchmark}).summary(true)
	}, overview, benchmarkSummary.compact, benchmarkSummary.identity)
}

func (tools catalogTools) collections(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var page toolPage
	if err := json.Unmarshal(raw, &page); err != nil {
		return nil, err
	}
	items, err := readCatalog(ctx, tools.source.ListCollections)
	if err != nil {
		return nil, err
	}
	if tools.options.ProviderFilter != "" {
		items = slices.DeleteFunc(items, func(item api.CollectionResource) bool {
			return !slices.ContainsFunc(item.Benchmarks, func(benchmark api.CollectionBenchmarkConfig) bool {
				return allowedProvider(tools.options, benchmark.ProviderID)
			})
		})
	}
	// Preserve the source ordering so independently requested pages line up.
	return boundedPage(page, len(items), func(i int) referenceSummary {
		item := items[i]
		entry := referenceSummary{clip(item.Resource.ID, 128), clip(item.Name, 80), smallList(item.Domains), item.Resource.IsSystemResource(), nil}
		for _, benchmark := range item.Benchmarks {
			if allowedProvider(tools.options, benchmark.ProviderID) && len(benchmark.ID) <= 200 && len(benchmark.ProviderID) <= 128 {
				entry.Benchmarks = append(entry.Benchmarks, benchmarkSummary{ID: benchmark.ID, ProviderID: benchmark.ProviderID})
				if len(entry.Benchmarks) == 2 {
					break
				}
			}
		}
		return entry
	}, "", referenceSummary.identity)
}

func boundedPage[T any](page toolPage, total int, project func(int) T, overview string, reduce ...func(T) T) (json.RawMessage, error) {
	result := struct {
		Items      []T    `json:"items"`
		NextOffset int    `json:"next_offset,omitempty"`
		Overview   string `json:"overview,omitempty"`
	}{Items: []T{}, Overview: overview}
	var lastPage json.RawMessage
	for i := page.Offset; i < min(total, page.Offset+page.limit()); i++ {
		result.Items = append(result.Items, project(i))
		if i+1 < total {
			result.NextOffset = i + 1
		} else {
			result.NextOffset = 0
		}
		data, err := json.Marshal(result)
		if err != nil {
			return nil, err
		}
		if len(data) <= toolResultBytes {
			lastPage = data
			continue
		}
		if lastPage != nil {
			return lastPage, nil
		}
		// A first item must fit on its own page for pagination to advance.
		// Sacrifice optional metadata before dropping the item.
		result.Overview = ""
		for _, shrink := range reduce {
			result.Items[0] = shrink(result.Items[0])
			data, err := json.Marshal(result)
			if err != nil {
				return nil, err
			}
			if len(data) <= toolResultBytes {
				return data, nil
			}
		}
		return nil, errors.New("catalog item exceeds the tool result limit")
	}
	if lastPage != nil {
		return lastPage, nil
	}
	return json.Marshal(result)
}

func (item candidate) summary(detail bool) benchmarkSummary {
	b := item.benchmark
	result := benchmarkSummary{
		ProviderID: item.key.provider, ID: item.key.id, Name: clip(b.Name, 96),
		Domains: smallList(b.Domains), Tasks: smallList(b.Tasks),
		Modalities: smallList(b.Modalities), Targets: smallList(b.EvaluationTargets),
		Metrics: smallList(b.Metrics),
	}
	if b.PrimaryScore != nil && len(b.PrimaryScore.Metric) <= 96 {
		result.PrimaryScore = b.PrimaryScore
	}
	if detail {
		result.Tags = smallList(b.Tags)
		result.Description = clip(b.Description, 160)
		if b.Agent != nil {
			result.AgentSummary = clip(b.Agent.ResultInterpretation, 96)
		}
	}
	return result
}

func smallList(values []string) []string {
	var result []string
	for _, value := range values {
		if len(value) <= 96 {
			result = append(result, value)
			if len(result) == 4 {
				break
			}
		}
	}
	return result
}

func clip(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	for limit > 0 && !utf8.RuneStart(value[limit]) {
		limit--
	}
	return strings.TrimSpace(value[:limit])
}
