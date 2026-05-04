package server

import (
	"context"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/evalhubclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// --- test helpers ---

func connectWithCompletions(t *testing.T, ds EvalHubDiscovery) (context.Context, *mcp.ClientSession) {
	t.Helper()

	srv := New(&ServerInfo{Version: "test"}, discardLogger, CompletionHandlerOption(ds, discardLogger))
	registerResources(srv, ds, discardLogger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	serverSession, err := srv.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect failed: %v", err)
	}
	t.Cleanup(func() { serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect failed: %v", err)
	}
	t.Cleanup(func() { clientSession.Close() })

	return ctx, clientSession
}

func complete(t *testing.T, ctx context.Context, cs *mcp.ClientSession, uri, argName, argValue string) *mcp.CompleteResult {
	t.Helper()
	result, err := cs.Complete(ctx, &mcp.CompleteParams{
		Ref: &mcp.CompleteReference{
			Type: "ref/resource",
			URI:  uri,
		},
		Argument: mcp.CompleteParamsArgument{
			Name:  argName,
			Value: argValue,
		},
	})
	if err != nil {
		t.Fatalf("Complete(%q, %q=%q) failed: %v", uri, argName, argValue, err)
	}
	return result
}

// --- provider ID completion ---

func TestCompleteProviderIDs(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithCompletions(t, testDataSource())

	result := complete(t, ctx, cs, "evalhub://providers/{id}", "id", "")
	if len(result.Completion.Values) != 2 {
		t.Fatalf("expected 2 provider IDs, got %d: %v", len(result.Completion.Values), result.Completion.Values)
	}
	want := map[string]bool{"lighteval": false, "unitxt": false}
	for _, v := range result.Completion.Values {
		want[v] = true
	}
	for id, found := range want {
		if !found {
			t.Errorf("missing provider ID %q", id)
		}
	}
}

func TestCompleteProviderIDsPrefix(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithCompletions(t, testDataSource())

	result := complete(t, ctx, cs, "evalhub://providers/{id}", "id", "light")
	if len(result.Completion.Values) != 1 {
		t.Fatalf("expected 1 match for prefix 'light', got %d", len(result.Completion.Values))
	}
	if result.Completion.Values[0] != "lighteval" {
		t.Errorf("expected 'lighteval', got %q", result.Completion.Values[0])
	}
}

func TestCompleteProviderIDsNoMatch(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithCompletions(t, testDataSource())

	result := complete(t, ctx, cs, "evalhub://providers/{id}", "id", "zzz")
	if len(result.Completion.Values) != 0 {
		t.Errorf("expected 0 matches for 'zzz', got %d", len(result.Completion.Values))
	}
}

// --- benchmark ID completion ---

func TestCompleteBenchmarkIDs(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithCompletions(t, testDataSource())

	result := complete(t, ctx, cs, "evalhub://benchmarks/{id}", "id", "")
	if len(result.Completion.Values) != 4 {
		t.Fatalf("expected 4 benchmark IDs, got %d", len(result.Completion.Values))
	}
}

func TestCompleteBenchmarkIDsPrefix(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithCompletions(t, testDataSource())

	result := complete(t, ctx, cs, "evalhub://benchmarks/{id}", "id", "mm")
	if len(result.Completion.Values) != 1 {
		t.Fatalf("expected 1 match for 'mm', got %d", len(result.Completion.Values))
	}
	if result.Completion.Values[0] != "mmlu" {
		t.Errorf("expected 'mmlu', got %q", result.Completion.Values[0])
	}
}

// --- collection ID completion ---

func TestCompleteCollectionIDs(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithCompletions(t, testDataSource())

	result := complete(t, ctx, cs, "evalhub://collections/{id}", "id", "")
	if len(result.Completion.Values) != 2 {
		t.Fatalf("expected 2 collection IDs, got %d", len(result.Completion.Values))
	}
}

func TestCompleteCollectionIDsPrefix(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithCompletions(t, testDataSource())

	result := complete(t, ctx, cs, "evalhub://collections/{id}", "id", "safe")
	if len(result.Completion.Values) != 1 {
		t.Fatalf("expected 1 match for 'safe', got %d", len(result.Completion.Values))
	}
	if result.Completion.Values[0] != "safety-suite" {
		t.Errorf("expected 'safety-suite', got %q", result.Completion.Values[0])
	}
}

// --- job ID completion ---

func TestCompleteJobIDs(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithCompletions(t, testDataSource())

	result := complete(t, ctx, cs, "evalhub://jobs/{id}", "id", "")
	if len(result.Completion.Values) != 3 {
		t.Fatalf("expected 3 job IDs, got %d", len(result.Completion.Values))
	}
}

func TestCompleteJobIDsPrefix(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithCompletions(t, testDataSource())

	result := complete(t, ctx, cs, "evalhub://jobs/{id}", "id", "job-2")
	if len(result.Completion.Values) != 1 {
		t.Fatalf("expected 1 match for 'job-2', got %d", len(result.Completion.Values))
	}
	if result.Completion.Values[0] != "job-2" {
		t.Errorf("expected 'job-2', got %q", result.Completion.Values[0])
	}
}

// --- status completion (static values) ---

func TestCompleteStatus(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithCompletions(t, testDataSource())

	result := complete(t, ctx, cs, "evalhub://jobs{?status}", "status", "")
	if len(result.Completion.Values) != 6 {
		t.Fatalf("expected 6 status values, got %d: %v", len(result.Completion.Values), result.Completion.Values)
	}
	want := []string{"pending", "running", "completed", "failed", "cancelled", "partially_failed"}
	for _, w := range want {
		found := false
		for _, v := range result.Completion.Values {
			if v == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing status value %q", w)
		}
	}
}

func TestCompleteStatusPrefix(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithCompletions(t, testDataSource())

	result := complete(t, ctx, cs, "evalhub://jobs{?status}", "status", "p")
	sort.Strings(result.Completion.Values)
	if len(result.Completion.Values) != 2 {
		t.Fatalf("expected 2 matches for 'p' (pending, partially_failed), got %d: %v",
			len(result.Completion.Values), result.Completion.Values)
	}
}

func TestCompleteStatusNoAPICall(t *testing.T) {
	t.Parallel()
	ds := &callCountDataSource{inner: testDataSource()}
	ctx, cs := connectWithCompletions(t, ds)

	_ = complete(t, ctx, cs, "evalhub://jobs{?status}", "status", "")
	if ds.listJobsCalls > 0 {
		t.Error("status completion should not call ListJobs")
	}
}

// --- label completion ---

func TestCompleteLabels(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithCompletions(t, testDataSource())

	result := complete(t, ctx, cs, "evalhub://benchmarks{?label*}", "label", "")
	want := map[string]bool{"reasoning": false, "general": false, "knowledge": false, "rag": false, "safety": false}
	for _, v := range result.Completion.Values {
		want[v] = true
	}
	for label, found := range want {
		if !found {
			t.Errorf("missing label %q", label)
		}
	}
}

func TestCompleteLabelsPrefix(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithCompletions(t, testDataSource())

	result := complete(t, ctx, cs, "evalhub://benchmarks{?label*}", "label", "ra")
	if len(result.Completion.Values) != 1 {
		t.Fatalf("expected 1 match for 'ra', got %d", len(result.Completion.Values))
	}
	if result.Completion.Values[0] != "rag" {
		t.Errorf("expected 'rag', got %q", result.Completion.Values[0])
	}
}

// --- caching ---

func TestCompletionCachePreventsRedundantCalls(t *testing.T) {
	t.Parallel()
	ds := &callCountDataSource{inner: testDataSource()}
	ctx, cs := connectWithCompletions(t, ds)

	_ = complete(t, ctx, cs, "evalhub://providers/{id}", "id", "")
	_ = complete(t, ctx, cs, "evalhub://providers/{id}", "id", "light")

	if ds.listProvidersCalls != 1 {
		t.Errorf("expected 1 ListProviders call (cached), got %d", ds.listProvidersCalls)
	}
}

// --- cache expiry ---

func TestCompletionCacheExpiry(t *testing.T) {
	t.Parallel()

	ds := &callCountDataSource{inner: testDataSource()}
	cp := newCompletionProvider(ds, discardLogger)

	now := time.Now()
	cp.cache.now = func() time.Time { return now }

	cp.resolveValues("evalhub://providers/{id}", "id")
	if ds.listProvidersCalls != 1 {
		t.Fatalf("expected 1 call, got %d", ds.listProvidersCalls)
	}

	cp.resolveValues("evalhub://providers/{id}", "id")
	if ds.listProvidersCalls != 1 {
		t.Fatalf("expected still 1 call (cached), got %d", ds.listProvidersCalls)
	}

	cp.cache.now = func() time.Time { return now.Add(defaultCacheTTL + time.Second) }

	cp.resolveValues("evalhub://providers/{id}", "id")
	if ds.listProvidersCalls != 2 {
		t.Errorf("expected 2 calls after TTL expiry, got %d", ds.listProvidersCalls)
	}
}

// --- graceful degradation ---

func TestCompletionAPIErrorReturnsEmpty(t *testing.T) {
	t.Parallel()
	ds := &errorDataSource{}
	ctx, cs := connectWithCompletions(t, ds)

	result := complete(t, ctx, cs, "evalhub://providers/{id}", "id", "")
	if len(result.Completion.Values) != 0 {
		t.Errorf("expected empty result on API error, got %v", result.Completion.Values)
	}
}

func TestCompletionAPIErrorBenchmarks(t *testing.T) {
	t.Parallel()
	ds := &errorDataSource{}
	ctx, cs := connectWithCompletions(t, ds)

	result := complete(t, ctx, cs, "evalhub://benchmarks/{id}", "id", "")
	if len(result.Completion.Values) != 0 {
		t.Errorf("expected empty result on API error, got %v", result.Completion.Values)
	}
}

func TestCompletionAPIErrorLabels(t *testing.T) {
	t.Parallel()
	ds := &errorDataSource{}
	ctx, cs := connectWithCompletions(t, ds)

	result := complete(t, ctx, cs, "evalhub://benchmarks{?label*}", "label", "")
	if len(result.Completion.Values) != 0 {
		t.Errorf("expected empty result on API error, got %v", result.Completion.Values)
	}
}

func TestCompletionAPIErrorNotCached(t *testing.T) {
	t.Parallel()

	calls := 0
	ds := &switchableDataSource{
		listProvidersFn: func() (*api.ProviderResourceList, error) {
			calls++
			if calls == 1 {
				return nil, &evalhubclient.APIError{StatusCode: http.StatusInternalServerError, Message: "transient"}
			}
			return &api.ProviderResourceList{
				Items: []api.ProviderResource{{Resource: api.Resource{ID: "recovered"}}},
				Page:  api.Page{TotalCount: 1},
			}, nil
		},
	}
	cp := newCompletionProvider(ds, discardLogger)

	got := cp.resolveValues("evalhub://providers/{id}", "id")
	if len(got) != 0 {
		t.Fatalf("expected empty on first (errored) call, got %v", got)
	}

	got = cp.resolveValues("evalhub://providers/{id}", "id")
	if len(got) != 1 || got[0] != "recovered" {
		t.Fatalf("expected [recovered] on retry, got %v", got)
	}
	if calls != 2 {
		t.Errorf("expected 2 API calls (error not cached), got %d", calls)
	}
}

// --- empty data source ---

func TestCompletionEmptyDataSource(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithCompletions(t, emptyDataSource())

	result := complete(t, ctx, cs, "evalhub://providers/{id}", "id", "")
	if result.Completion.Values == nil {
		t.Fatal("expected non-nil empty slice")
	}
	if len(result.Completion.Values) != 0 {
		t.Errorf("expected 0 values, got %d", len(result.Completion.Values))
	}
}

// --- unknown URI template ---

func TestCompletionUnknownTemplate(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithCompletions(t, testDataSource())

	result := complete(t, ctx, cs, "evalhub://unknown/{id}", "id", "")
	if len(result.Completion.Values) != 0 {
		t.Errorf("expected 0 values for unknown template, got %d", len(result.Completion.Values))
	}
}

// --- case-insensitive prefix matching ---

func TestCompletePrefixCaseInsensitive(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithCompletions(t, testDataSource())

	result := complete(t, ctx, cs, "evalhub://providers/{id}", "id", "LIGHT")
	if len(result.Completion.Values) != 1 {
		t.Fatalf("expected 1 match for 'LIGHT', got %d", len(result.Completion.Values))
	}
	if result.Completion.Values[0] != "lighteval" {
		t.Errorf("expected 'lighteval', got %q", result.Completion.Values[0])
	}
}

// --- mock data sources ---

type callCountDataSource struct {
	inner              EvalHubDiscovery
	listProvidersCalls int
	listJobsCalls      int
}

func (d *callCountDataSource) ListProviders(opts ...evalhubclient.ListOption) (*api.ProviderResourceList, error) {
	d.listProvidersCalls++
	return d.inner.ListProviders(opts...)
}

func (d *callCountDataSource) GetProvider(id string) (*api.ProviderResource, error) {
	return d.inner.GetProvider(id)
}

func (d *callCountDataSource) ListBenchmarks() ([]api.BenchmarkResource, error) {
	return d.inner.ListBenchmarks()
}

func (d *callCountDataSource) GetBenchmark(id string) (*api.BenchmarkResource, error) {
	return d.inner.GetBenchmark(id)
}

func (d *callCountDataSource) ListBenchmarksByLabel(labels []string) ([]api.BenchmarkResource, error) {
	return d.inner.ListBenchmarksByLabel(labels)
}

func (d *callCountDataSource) ListCollections(opts ...evalhubclient.ListOption) (*api.CollectionResourceList, error) {
	return d.inner.ListCollections(opts...)
}

func (d *callCountDataSource) GetCollection(id string) (*api.CollectionResource, error) {
	return d.inner.GetCollection(id)
}

func (d *callCountDataSource) ListJobs(opts ...evalhubclient.ListOption) (*api.EvaluationJobResourceList, error) {
	d.listJobsCalls++
	return d.inner.ListJobs(opts...)
}

func (d *callCountDataSource) GetJob(id string) (*api.EvaluationJobResource, error) {
	return d.inner.GetJob(id)
}

func (d *callCountDataSource) ListJobsByStatus(status api.OverallState, opts ...evalhubclient.ListOption) (*api.EvaluationJobResourceList, error) {
	return d.inner.ListJobsByStatus(status, opts...)
}

type errorDataSource struct{}

func (d *errorDataSource) ListProviders(_ ...evalhubclient.ListOption) (*api.ProviderResourceList, error) {
	return nil, &evalhubclient.APIError{StatusCode: http.StatusInternalServerError, Message: "server error"}
}

func (d *errorDataSource) GetProvider(_ string) (*api.ProviderResource, error) {
	return nil, &evalhubclient.APIError{StatusCode: http.StatusInternalServerError, Message: "server error"}
}

func (d *errorDataSource) ListBenchmarks() ([]api.BenchmarkResource, error) {
	return nil, &evalhubclient.APIError{StatusCode: http.StatusInternalServerError, Message: "server error"}
}

func (d *errorDataSource) GetBenchmark(_ string) (*api.BenchmarkResource, error) {
	return nil, &evalhubclient.APIError{StatusCode: http.StatusInternalServerError, Message: "server error"}
}

func (d *errorDataSource) ListBenchmarksByLabel(_ []string) ([]api.BenchmarkResource, error) {
	return nil, &evalhubclient.APIError{StatusCode: http.StatusInternalServerError, Message: "server error"}
}

func (d *errorDataSource) ListCollections(_ ...evalhubclient.ListOption) (*api.CollectionResourceList, error) {
	return nil, &evalhubclient.APIError{StatusCode: http.StatusInternalServerError, Message: "server error"}
}

func (d *errorDataSource) GetCollection(_ string) (*api.CollectionResource, error) {
	return nil, &evalhubclient.APIError{StatusCode: http.StatusInternalServerError, Message: "server error"}
}

func (d *errorDataSource) ListJobs(_ ...evalhubclient.ListOption) (*api.EvaluationJobResourceList, error) {
	return nil, &evalhubclient.APIError{StatusCode: http.StatusInternalServerError, Message: "server error"}
}

func (d *errorDataSource) GetJob(_ string) (*api.EvaluationJobResource, error) {
	return nil, &evalhubclient.APIError{StatusCode: http.StatusInternalServerError, Message: "server error"}
}

func (d *errorDataSource) ListJobsByStatus(_ api.OverallState, _ ...evalhubclient.ListOption) (*api.EvaluationJobResourceList, error) {
	return nil, &evalhubclient.APIError{StatusCode: http.StatusInternalServerError, Message: "server error"}
}

// switchableDataSource delegates ListProviders to a caller-supplied function,
// allowing tests to change behavior between calls (e.g. error then success).
type switchableDataSource struct {
	listProvidersFn func() (*api.ProviderResourceList, error)
}

func (d *switchableDataSource) ListProviders(_ ...evalhubclient.ListOption) (*api.ProviderResourceList, error) {
	return d.listProvidersFn()
}
func (d *switchableDataSource) GetProvider(_ string) (*api.ProviderResource, error) {
	return nil, &evalhubclient.APIError{StatusCode: http.StatusNotFound}
}
func (d *switchableDataSource) ListBenchmarks() ([]api.BenchmarkResource, error) { return nil, nil }
func (d *switchableDataSource) GetBenchmark(_ string) (*api.BenchmarkResource, error) {
	return nil, &evalhubclient.APIError{StatusCode: http.StatusNotFound}
}
func (d *switchableDataSource) ListBenchmarksByLabel(_ []string) ([]api.BenchmarkResource, error) {
	return nil, nil
}
func (d *switchableDataSource) ListCollections(_ ...evalhubclient.ListOption) (*api.CollectionResourceList, error) {
	return nil, nil
}
func (d *switchableDataSource) GetCollection(_ string) (*api.CollectionResource, error) {
	return nil, &evalhubclient.APIError{StatusCode: http.StatusNotFound}
}
func (d *switchableDataSource) ListJobs(_ ...evalhubclient.ListOption) (*api.EvaluationJobResourceList, error) {
	return nil, nil
}
func (d *switchableDataSource) GetJob(_ string) (*api.EvaluationJobResource, error) {
	return nil, &evalhubclient.APIError{StatusCode: http.StatusNotFound}
}
func (d *switchableDataSource) ListJobsByStatus(_ api.OverallState, _ ...evalhubclient.ListOption) (*api.EvaluationJobResourceList, error) {
	return nil, nil
}

// Verify errorDataSource implements EvalHubDiscovery at compile time.
var _ EvalHubDiscovery = (*errorDataSource)(nil)
var _ EvalHubDiscovery = (*callCountDataSource)(nil)
var _ EvalHubDiscovery = (*switchableDataSource)(nil)

// --- filterByPrefix unit tests ---

func TestFilterByPrefix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		values []string
		prefix string
		want   int
	}{
		{"empty prefix returns all", []string{"a", "b", "c"}, "", 3},
		{"exact match", []string{"foo", "bar"}, "foo", 1},
		{"partial match", []string{"foo", "foobar", "baz"}, "foo", 2},
		{"no match", []string{"foo", "bar"}, "xyz", 0},
		{"case insensitive", []string{"FooBar", "foobar", "baz"}, "foo", 2},
		{"nil values", nil, "foo", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := filterByPrefix(tt.values, tt.prefix)
			if len(got) != tt.want {
				t.Errorf("filterByPrefix(%v, %q) = %d results, want %d", tt.values, tt.prefix, len(got), tt.want)
			}
		})
	}
}

// --- completionCache unit tests ---

func TestCompletionCacheGetMiss(t *testing.T) {
	t.Parallel()
	c := newCompletionCache(time.Minute)
	_, ok := c.get("nonexistent")
	if ok {
		t.Error("expected cache miss for nonexistent key")
	}
}

func TestCompletionCacheGetHit(t *testing.T) {
	t.Parallel()
	c := newCompletionCache(time.Minute)
	c.set("key", []string{"a", "b"})
	vals, ok := c.get("key")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if len(vals) != 2 {
		t.Errorf("expected 2 values, got %d", len(vals))
	}
}

func TestCompletionCacheExpired(t *testing.T) {
	t.Parallel()
	c := newCompletionCache(time.Second)
	now := time.Now()
	c.now = func() time.Time { return now }

	c.set("key", []string{"a"})
	_, ok := c.get("key")
	if !ok {
		t.Fatal("expected cache hit before expiry")
	}

	c.now = func() time.Time { return now.Add(2 * time.Second) }
	_, ok = c.get("key")
	if ok {
		t.Error("expected cache miss after expiry")
	}
}

// --- matchesTemplate ---

func TestMatchesTemplate(t *testing.T) {
	t.Parallel()
	if !matchesTemplate("evalhub://providers/{id}", "evalhub://providers/{id}") {
		t.Error("expected match")
	}
	if matchesTemplate("evalhub://providers/{id}", "evalhub://benchmarks/{id}") {
		t.Error("expected no match")
	}
}

// --- status completion prefix "comp" ---

func TestCompleteStatusPrefixComp(t *testing.T) {
	t.Parallel()
	ctx, cs := connectWithCompletions(t, testDataSource())

	result := complete(t, ctx, cs, "evalhub://jobs{?status}", "status", "comp")
	if len(result.Completion.Values) != 1 {
		t.Fatalf("expected 1 match for 'comp', got %d: %v", len(result.Completion.Values), result.Completion.Values)
	}
	if result.Completion.Values[0] != "completed" {
		t.Errorf("expected 'completed', got %q", result.Completion.Values[0])
	}
}
