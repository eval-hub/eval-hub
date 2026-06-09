package server

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/evalhubclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const defaultCacheTTL = 30 * time.Second

var statusValues = []string{
	string(api.OverallStatePending),
	string(api.OverallStateRunning),
	string(api.OverallStateCompleted),
	string(api.OverallStateFailed),
	string(api.OverallStateCancelled),
	string(api.OverallStatePartiallyFailed),
}

type completionCache struct {
	mu      sync.Mutex
	entries map[string]*cacheEntry
	ttl     time.Duration
	now     func() time.Time
}

type cacheEntry struct {
	values    []string
	expiresAt time.Time
}

func newCompletionCache(ttl time.Duration) *completionCache {
	return &completionCache{
		entries: make(map[string]*cacheEntry),
		ttl:     ttl,
		now:     time.Now,
	}
}

func (c *completionCache) get(key string) ([]string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || c.now().After(e.expiresAt) {
		return nil, false
	}
	return e.values, true
}

func (c *completionCache) set(key string, values []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = &cacheEntry{
		values:    values,
		expiresAt: c.now().Add(c.ttl),
	}
}

type completionProvider struct {
	ds            EvalHubDiscovery
	cache         *completionCache
	logger        *slog.Logger
	listPageLimit int
}

func newCompletionProvider(ds EvalHubDiscovery, logger *slog.Logger, listPageLimit int) *completionProvider {
	return &completionProvider{
		ds:            ds,
		cache:         newCompletionCache(defaultCacheTTL),
		logger:        logger,
		listPageLimit: listPageLimit,
	}
}

func (cp *completionProvider) handle(ctx context.Context, req *mcp.CompleteRequest) (*mcp.CompleteResult, error) {
	if req.Params.Ref == nil || req.Params.Ref.Type != "ref/resource" {
		return emptyResult(), nil
	}

	uri := req.Params.Ref.URI
	argName := req.Params.Argument.Name
	prefix := req.Params.Argument.Value

	values := cp.resolveValues(ctx, uri, argName)
	filtered := filterByPrefix(values, prefix)

	return &mcp.CompleteResult{
		Completion: mcp.CompletionResultDetails{
			Values:  filtered,
			Total:   len(filtered),
			HasMore: false,
		},
	}, nil
}

func (cp *completionProvider) resolveValues(ctx context.Context, uri, argName string) []string {
	switch {
	case matchesTemplate(uri, "evalhub://providers/{id}") && argName == "id":
		return cp.cachedFetch("providers", func() []string { return cp.fetchProviderIDs(ctx) })
	case matchesTemplate(uri, "evalhub://benchmarks/{id}") && argName == "id":
		return cp.cachedFetch("benchmarks", func() []string { return cp.fetchBenchmarkIDs(ctx) })
	case matchesTemplate(uri, "evalhub://collections/{id}") && argName == "id":
		return cp.cachedFetch("collections", func() []string { return cp.fetchCollectionIDs(ctx) })
	case matchesTemplate(uri, "evalhub://jobs/{id}") && argName == "id":
		return cp.cachedFetch("jobs", func() []string { return cp.fetchJobIDs(ctx) })
	case matchesTemplate(uri, "evalhub://jobs{?status}") && argName == "status":
		return statusValues
	case matchesTemplate(uri, "evalhub://benchmarks{?label*}") && argName == "label":
		return cp.cachedFetch("labels", func() []string { return cp.fetchLabels(ctx) })
	default:
		return nil
	}
}

func matchesTemplate(uri, template string) bool {
	return uri == template
}

func (cp *completionProvider) cachedFetch(key string, fetch func() []string) []string {
	if values, ok := cp.cache.get(key); ok {
		return values
	}
	values := fetch()
	if values != nil {
		cp.cache.set(key, values)
	}
	return values
}

func (cp *completionProvider) fetchProviderIDs(ctx context.Context) []string {
	list, err := cp.ds.ListProviders(evalhubclient.WithLimit(cp.listPageLimit))
	if err != nil {
		requestLogger(ctx, cp.logger).Warn("completion: failed to list providers", "error", err)
		return nil
	}
	ids := make([]string, len(list.Items))
	for i, p := range list.Items {
		ids[i] = p.Resource.ID
	}
	return ids
}

func (cp *completionProvider) fetchBenchmarkIDs(ctx context.Context) []string {
	benchmarks, err := cp.ds.ListBenchmarks()
	if err != nil {
		requestLogger(ctx, cp.logger).Warn("completion: failed to list benchmarks", "error", err)
		return nil
	}
	ids := make([]string, len(benchmarks))
	for i, b := range benchmarks {
		ids[i] = b.ID
	}
	return ids
}

func (cp *completionProvider) fetchCollectionIDs(ctx context.Context) []string {
	list, err := cp.ds.ListCollections(evalhubclient.WithLimit(cp.listPageLimit))
	if err != nil {
		requestLogger(ctx, cp.logger).Warn("completion: failed to list collections", "error", err)
		return nil
	}
	ids := make([]string, len(list.Items))
	for i, c := range list.Items {
		ids[i] = c.Resource.ID
	}
	return ids
}

func (cp *completionProvider) fetchJobIDs(ctx context.Context) []string {
	list, err := cp.ds.ListJobs(evalhubclient.WithLimit(cp.listPageLimit))
	if err != nil {
		requestLogger(ctx, cp.logger).Warn("completion: failed to list jobs", "error", err)
		return nil
	}
	ids := make([]string, len(list.Items))
	for i, j := range list.Items {
		ids[i] = j.Resource.ID
	}
	return ids
}

func (cp *completionProvider) fetchLabels(ctx context.Context) []string {
	benchmarks, err := cp.ds.ListBenchmarks()
	if err != nil {
		requestLogger(ctx, cp.logger).Warn("completion: failed to list benchmarks for labels", "error", err)
		return nil
	}
	seen := make(map[string]struct{})
	var labels []string
	for _, b := range benchmarks {
		for _, tag := range b.Tags {
			if _, ok := seen[tag]; !ok {
				seen[tag] = struct{}{}
				labels = append(labels, tag)
			}
		}
	}
	return labels
}

func filterByPrefix(values []string, prefix string) []string {
	if prefix == "" {
		return values
	}
	lower := strings.ToLower(prefix)
	var result []string
	for _, v := range values {
		if strings.HasPrefix(strings.ToLower(v), lower) {
			result = append(result, v)
		}
	}
	return result
}

func emptyResult() *mcp.CompleteResult {
	return &mcp.CompleteResult{
		Completion: mcp.CompletionResultDetails{
			Values: []string{},
		},
	}
}
