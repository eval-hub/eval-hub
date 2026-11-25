package orchestration

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"unicode"

	"github.com/eval-hub/eval-hub/internal/collectiondesign"
	"github.com/eval-hub/eval-hub/pkg/api"
)

const (
	catalogPageSize = 25
	maxCatalogPages = 40
	maxBenchmarks   = 10000
)

// Catalog extends the collection-design reads with a single provider lookup.
// Implementations must already be scoped to the workload's tenant.
type Catalog interface {
	collectiondesign.CatalogSource
	GetProvider(context.Context, string) (api.ProviderResource, error)
}

type benchmarkKey struct{ provider, id string }

type candidate struct {
	key       benchmarkKey
	benchmark api.BenchmarkResource
	score     int
}

type catalogSnapshot struct {
	providers   []api.ProviderResource
	collections []api.CollectionResource
}

func (s catalogSnapshot) ListProviders(ctx context.Context, offset, limit int) (collectiondesign.Page[api.ProviderResource], error) {
	return pageFromSnapshot(s.providers, offset, limit), ctx.Err()
}

func (s catalogSnapshot) ListCollections(ctx context.Context, offset, limit int) (collectiondesign.Page[api.CollectionResource], error) {
	return pageFromSnapshot(s.collections, offset, limit), ctx.Err()
}

func pageFromSnapshot[T any](items []T, offset, limit int) collectiondesign.Page[T] {
	offset = min(offset, len(items))
	return collectiondesign.Page[T]{Items: items[offset:min(offset+limit, len(items))], TotalCount: len(items)}
}

func readCatalog[T any](ctx context.Context, read func(context.Context, int, int) (collectiondesign.Page[T], error)) ([]T, error) {
	var items []T
	for range maxCatalogPages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		page, err := read(ctx, len(items), catalogPageSize)
		if err != nil {
			return nil, err
		}
		if len(page.Items) > catalogPageSize || page.TotalCount < len(items)+len(page.Items) {
			return nil, errors.New("catalog page has inconsistent pagination")
		}
		items = append(items, page.Items...)
		if len(items) == page.TotalCount {
			return items, nil
		}
		if len(page.Items) == 0 {
			return nil, errors.New("catalog pagination ended before the last entry")
		}
	}
	return nil, errors.New("catalog exceeds the discovery page limit")
}

func readProviders(ctx context.Context, source Catalog) ([]api.ProviderResource, error) {
	providers, err := readCatalog(ctx, source.ListProviders)
	if err != nil {
		return nil, err
	}
	total := 0
	ids := make(map[string]bool, len(providers))
	for _, provider := range providers {
		total += len(provider.Benchmarks)
		if total > maxBenchmarks || provider.Resource.ID == "" || ids[provider.Resource.ID] {
			return nil, errors.New("provider catalog exceeds limits or contains duplicate IDs")
		}
		ids[provider.Resource.ID] = true
	}
	return providers, nil
}

func discover(providers []api.ProviderResource, options collectiondesign.Options) ([]candidate, string) {
	requested := maps.Clone(options.ProviderIDs)
	goal := normalizedTerms(options.Goal)
	var eligible []candidate
	unavailable, otherTarget := false, false
	target := explicitTarget(options.Goal)
	for _, provider := range providers {
		id := provider.Resource.ID
		if !allowedProvider(options, id) {
			continue
		}
		delete(requested, id)
		for _, benchmark := range provider.Benchmarks {
			score := relevance(goal, benchmark)
			if score == 0 || benchmark.ID == "" {
				continue
			}
			if target != "" && len(benchmark.EvaluationTargets) > 0 && !slices.Contains(benchmark.EvaluationTargets, target) {
				otherTarget = true
				continue
			}
			if providerUnavailable(provider) {
				unavailable = true
				continue
			}
			eligible = append(eligible, candidate{benchmarkKey{id, benchmark.ID}, benchmark, score})
		}
	}
	switch {
	case len(requested) != 0:
		return nil, "validation_failed"
	case len(eligible) == 0 && (unavailable || otherTarget):
		return nil, "unsupported_capability"
	case len(eligible) == 0:
		return nil, "no_relevant_benchmarks"
	default:
		slices.SortFunc(eligible, compareCandidates)
		return eligible, ""
	}
}

func allowedProvider(options collectiondesign.Options, id string) bool {
	if options.ProviderFilter == "" {
		return true
	}
	_, ok := options.ProviderIDs[id]
	return ok
}

// Missing runtime metadata gives unknown readiness. An explicit configuration
// with only a local adapter or an empty Kubernetes image cannot run here.
func providerUnavailable(provider api.ProviderResource) bool {
	return provider.Runtime != nil && (provider.Runtime.K8s == nil || strings.TrimSpace(provider.Runtime.K8s.Image) == "")
}

func compareCandidates(a, b candidate) int {
	if a.score != b.score {
		return b.score - a.score
	}
	if a.key.provider != b.key.provider {
		return strings.Compare(a.key.provider, b.key.provider)
	}
	return strings.Compare(a.key.id, b.key.id)
}

func explicitTarget(goal string) string {
	terms := " " + strings.Join(strings.FieldsFunc(strings.ToLower(goal), func(r rune) bool {
		return !unicode.IsLetter(r)
	}), " ") + " "
	switch {
	case strings.Contains(terms, " inference server "):
		return "inference_server"
	case strings.Contains(terms, " ai agent "), strings.Contains(terms, " agent benchmark "):
		return "agent"
	default:
		return ""
	}
}

// The generation prompt and model reasons are not evidence. Match meaningful
// goal terms against documented benchmark metadata, with a small set of
// common synonyms. Generic evaluation language cannot match every benchmark.
func normalizedTerms(text string) []string {
	var terms []string
	for _, word := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}) {
		switch word {
		case "a", "an", "and", "are", "as", "at", "be", "by", "can", "for", "from", "how", "i", "in", "is", "it", "me", "my", "of", "on", "or", "our", "that", "the", "to", "we", "what", "with", "would", "you",
			"ai", "llm", "llms", "model", "models", "benchmark", "benchmarks", "eval", "evals", "evaluation", "evaluations", "evaluate", "evaluating", "assess", "assessment", "test", "tests", "testing", "measure",
			"add", "assemble", "build", "check", "collection", "collections", "compare", "compact", "create", "curate", "design", "find", "generate", "give", "make", "need", "pick", "please", "select", "suite", "using", "want",
			"baseline", "balanced", "broad", "everyday", "general", "generalist", "purpose", "quality", "new", "starter", "small", "short", "help", "use", "assistant", "assistants", "chat", "quick", "support":
			continue
		case "safe", "safety", "unsafe":
			word = "safety"
		case "toxic", "toxicity":
			word = "toxicity"
		case "math", "maths", "mathematics":
			word = "math"
		case "coding", "code", "programming":
			word = "code"
		case "tools":
			word = "tool"
		case "functions":
			word = "function"
		case "visual", "vision", "images":
			word = "image"
		case "multimodal":
			word = "image"
		default:
			if len(word) > 4 {
				word = strings.TrimSuffix(word, "s")
			}
		}
		if len(word) > 1 && !slices.Contains(terms, word) {
			terms = append(terms, word)
		}
	}
	return terms
}

func relevance(goal []string, benchmark api.BenchmarkResource) int {
	if len(goal) == 0 {
		// Broad goals can use a catalog-described benchmark. The guardrail
		// gate must pass even for a terse goal.
		if benchmark.Name != "" || len(benchmark.Domains) > 0 || len(benchmark.Tags) > 0 {
			return 1
		}
		return 0
	}
	identity := normalizedTerms(benchmark.ID + " " + benchmark.Name)
	classification := normalizedTerms(strings.Join(slices.Concat(benchmark.Domains,
		benchmark.Tasks, benchmark.Tags, benchmark.Modalities, benchmark.EvaluationTargets,
		[]string{benchmark.Category}), " "))
	description := normalizedTerms(benchmark.Description)
	score := 0
	for _, term := range goal {
		switch {
		case slices.Contains(identity, term):
			score += 4
		case slices.Contains(classification, term):
			score += 3
		case slices.Contains(description, term):
			score++
		}
	}
	return score
}
