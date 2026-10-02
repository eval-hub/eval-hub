package collectiondesign

import (
	"context"
	"errors"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/evalhubclient"
)

func TestParseOptions(t *testing.T) {
	t.Parallel()
	options, err := ParseOptions("  evaluate safety  ", " p2, p1, ,p2 ", " 7 ", " strict ")
	if err != nil {
		t.Fatal(err)
	}
	if options.Goal != "evaluate safety" || options.ProviderFilter != "p2, p1, ,p2" || options.MaxBenchmarks != 7 || options.Strictness != StrictnessStrict {
		t.Fatalf("unexpected options: %+v", options)
	}
	if len(options.ProviderIDs) != 2 {
		t.Fatalf("provider IDs were not deduplicated: %v", options.ProviderIDs)
	}
	for _, id := range []string{"p1", "p2"} {
		if _, ok := options.ProviderIDs[id]; !ok {
			t.Errorf("provider ID %q missing from lookup set: %v", id, options.ProviderIDs)
		}
	}
	if got := options.Summary(); got != "Provider filter: p2, p1, ,p2 | Max benchmarks: 7 | Strictness: strict" {
		t.Fatalf("MCP option summary changed: %q", got)
	}

	defaults, err := ParseOptions("goal", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if defaults.MaxBenchmarks != 12 || defaults.Strictness != StrictnessModerate || defaults.Summary() != "Max benchmarks: 12 | Strictness: moderate" {
		t.Fatalf("unexpected defaults: %+v", defaults)
	}

	for _, tc := range []struct{ goal, filter, max, strictness, want string }{
		{" ", "", "", "", "evaluation_goal is required"},
		{"goal", "", "abc", "", "max_benchmarks"},
		{"goal", "", "0", "", "max_benchmarks"},
		{"goal", "", "-1", "", "max_benchmarks"},
		{"goal", "", "", "extreme", "strictness"},
	} {
		_, err := ParseOptions(tc.goal, tc.filter, tc.max, tc.strictness)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("ParseOptions(%q,%q,%q,%q) = %v, want %q", tc.goal, tc.filter, tc.max, tc.strictness, err, tc.want)
		}
	}
	if got := strings.Join(ValidStrictness(), ", "); got != strings.Join([]string{StrictnessLenient, StrictnessModerate, StrictnessStrict}, ", ") {
		t.Fatalf("strictness values changed: %q", got)
	}
}

func TestNeutralGuidanceDoesNotContainMCPInstructions(t *testing.T) {
	t.Parallel()
	options, err := ParseOptions("safety", "", "", "strict")
	if err != nil {
		t.Fatal(err)
	}
	guidance := NeutralGuidance(options)
	for _, wanted := range []string{"live provider catalog", "Sets appropriate weights", "Strictness: **strict**", "Domain Signal Mapping", "CollectionConfig"} {
		if !strings.Contains(guidance, wanted) {
			t.Errorf("neutral guidance missing %q", wanted)
		}
	}
	for _, forbidden := range []string{"create_collection", "search_benchmarks", "get_benchmark", "AVAILABLE BENCHMARKS catalog below", "{strictness}"} {
		if strings.Contains(guidance, forbidden) {
			t.Errorf("neutral guidance contains MCP-only text %q", forbidden)
		}
	}
}

func TestExtractGuidanceSectionRejectsMalformedMarkdown(t *testing.T) {
	for _, tc := range []struct {
		name     string
		markdown string
	}{
		{name: "missing section", markdown: "some prose"},
		{name: "missing terminator", markdown: "<!-- BEGIN requirements -->\ncontent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected malformed embedded guidance to panic")
				}
			}()
			extractGuidanceSection(tc.markdown, "requirements")
		})
	}
}

type fakeCatalogSource struct {
	providers         []api.ProviderResource
	collections       []api.CollectionResource
	providerErr       error
	collectionErr     error
	providerOffsets   []int
	collectionOffsets []int
	afterProviderPage func()
}

func (s *fakeCatalogSource) ListProviders(_ context.Context, offset, limit int) (Page[api.ProviderResource], error) {
	s.providerOffsets = append(s.providerOffsets, offset)
	if s.providerErr != nil {
		return Page[api.ProviderResource]{}, s.providerErr
	}
	page := Page[api.ProviderResource]{Items: pageItems(s.providers, offset, limit), TotalCount: len(s.providers)}
	if s.afterProviderPage != nil {
		s.afterProviderPage()
	}
	return page, nil
}

func (s *fakeCatalogSource) ListCollections(_ context.Context, offset, limit int) (Page[api.CollectionResource], error) {
	s.collectionOffsets = append(s.collectionOffsets, offset)
	if s.collectionErr != nil {
		return Page[api.CollectionResource]{}, s.collectionErr
	}
	return Page[api.CollectionResource]{Items: pageItems(s.collections, offset, limit), TotalCount: len(s.collections)}, nil
}

func pageItems[T any](all []T, offset, limit int) []T {
	if offset >= len(all) {
		return nil
	}
	end := min(offset+limit, len(all))
	return all[offset:end]
}

func TestGatherPaginatesAndPreservesFilteringAndOrder(t *testing.T) {
	t.Parallel()
	providers := make([]api.ProviderResource, 201)
	providers[0] = api.ProviderResource{
		Resource:       api.Resource{ID: "p1"},
		ProviderConfig: api.ProviderConfig{Benchmarks: []api.BenchmarkResource{{ID: "b1", Name: "B1"}}},
	}
	providers[200] = api.ProviderResource{
		Resource:       api.Resource{ID: "p2"},
		ProviderConfig: api.ProviderConfig{Benchmarks: []api.BenchmarkResource{{ID: "b2", Name: "B2", Metrics: []string{"acc"}}}},
	}
	collections := make([]api.CollectionResource, 202)
	collections[0] = api.CollectionResource{
		Resource:         api.Resource{ID: "first", Owner: "system"},
		CollectionConfig: api.CollectionConfig{Name: "First", Benchmarks: []api.CollectionBenchmarkConfig{{Ref: api.Ref{ID: "b1"}, ProviderID: "p1"}}},
	}
	collections[200] = api.CollectionResource{
		Resource:         api.Resource{ID: "second", Owner: "system"},
		CollectionConfig: api.CollectionConfig{Name: "Second", Benchmarks: []api.CollectionBenchmarkConfig{{Ref: api.Ref{ID: "b1"}, ProviderID: "p1"}, {Ref: api.Ref{ID: "b2"}, ProviderID: "p2"}}},
	}
	collections[201] = api.CollectionResource{Resource: api.Resource{ID: "tenant", Owner: "user"}, CollectionConfig: api.CollectionConfig{Name: "Private"}}
	source := &fakeCatalogSource{providers: providers, collections: collections}
	options, err := ParseOptions("goal", "p2,p2", "", "")
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := Gather(context.Background(), source, options)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(source.providerOffsets, []int{0, 200}) || !reflect.DeepEqual(source.collectionOffsets, []int{0, 200}) {
		t.Fatalf("pagination skipped a page: providers=%v collections=%v", source.providerOffsets, source.collectionOffsets)
	}
	if len(catalog.Benchmarks) != 1 || catalog.Benchmarks[0].ID != "b2" || catalog.Benchmarks[0].ProviderID != "p2" || !reflect.DeepEqual(catalog.Benchmarks[0].Metrics, []string{"acc"}) {
		t.Fatalf("wrong filtered benchmarks: %+v", catalog.Benchmarks)
	}
	if len(catalog.Examples) != 1 || catalog.Examples[0].ID != "second" || len(catalog.Examples[0].Benchmarks) != 1 || catalog.Examples[0].Benchmarks[0].ID != "b2" {
		t.Fatalf("wrong filtered system examples: %+v", catalog.Examples)
	}

	unfiltered, err := ParseOptions("goal", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	all, err := Gather(context.Background(), &fakeCatalogSource{providers: providers, collections: collections}, unfiltered)
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Benchmarks) != 2 || all.Benchmarks[0].ID != "b1" || all.Benchmarks[1].ID != "b2" || len(all.Examples) != 2 || all.Examples[0].ID != "first" || all.Examples[1].ID != "second" {
		t.Fatalf("source ordering or system-only examples changed: %+v", all)
	}
}

func TestGatherErrorsAndCancellation(t *testing.T) {
	t.Parallel()
	options, _ := ParseOptions("goal", "", "", "")
	providerErr := errors.New("provider read failed")
	_, err := Gather(context.Background(), &fakeCatalogSource{providerErr: providerErr}, options)
	if !errors.Is(err, providerErr) || !strings.Contains(err.Error(), "fetching benchmark catalog") {
		t.Fatalf("provider error was not preserved: %v", err)
	}

	source := &fakeCatalogSource{}
	_, err = Gather(context.Background(), source, options)
	if err == nil || !strings.Contains(err.Error(), "benchmark catalog is empty") || len(source.collectionOffsets) != 0 {
		t.Fatalf("empty catalog should fail before collection read: %v %+v", err, source)
	}

	emptyFilter, _ := ParseOptions("goal", ", ,", "", "")
	_, err = Gather(context.Background(), &fakeCatalogSource{providers: []api.ProviderResource{{Resource: api.Resource{ID: "p1"}, ProviderConfig: api.ProviderConfig{Benchmarks: []api.BenchmarkResource{{ID: "b1"}}}}}}, emptyFilter)
	if err == nil || !strings.Contains(err.Error(), "provider filter") {
		t.Fatalf("comma-only filter should match nothing: %v", err)
	}

	collectionErr := errors.New("collection read failed")
	_, err = Gather(context.Background(), &fakeCatalogSource{
		providers:     []api.ProviderResource{{Resource: api.Resource{ID: "p1"}, ProviderConfig: api.ProviderConfig{Benchmarks: []api.BenchmarkResource{{ID: "b1"}}}}},
		collectionErr: collectionErr,
	}, options)
	if !errors.Is(err, collectionErr) || !strings.Contains(err.Error(), "fetching collection examples") {
		t.Fatalf("collection error was not preserved: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	providers := make([]api.ProviderResource, 201)
	source = &fakeCatalogSource{providers: providers, afterProviderPage: cancel}
	_, err = Gather(ctx, source, options)
	if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(source.providerOffsets, []int{0}) {
		t.Fatalf("cancellation should stop pagination: err=%v offsets=%v", err, source.providerOffsets)
	}
}

type fakeClientReader struct {
	providerOptions   url.Values
	collectionOptions url.Values
	providerCalls     int
	collectionCalls   int
	providers         *api.ProviderResourceList
	providerErr       error
	providerNil       bool
	collections       *api.CollectionResourceList
	collectionErr     error
	collectionNil     bool
}

func (c *fakeClientReader) ListProviders(opts ...evalhubclient.ListOption) (*api.ProviderResourceList, error) {
	c.providerCalls++
	c.providerOptions = url.Values{}
	for _, opt := range opts {
		opt(c.providerOptions)
	}
	if c.providerErr != nil || c.providers != nil || c.providerNil {
		return c.providers, c.providerErr
	}
	return &api.ProviderResourceList{Page: api.Page{TotalCount: 1}, Items: []api.ProviderResource{{Resource: api.Resource{ID: "p"}}}}, nil
}

func (c *fakeClientReader) ListCollections(opts ...evalhubclient.ListOption) (*api.CollectionResourceList, error) {
	c.collectionCalls++
	c.collectionOptions = url.Values{}
	for _, opt := range opts {
		opt(c.collectionOptions)
	}
	if c.collectionErr != nil || c.collections != nil || c.collectionNil {
		return c.collections, c.collectionErr
	}
	return &api.CollectionResourceList{Page: api.Page{TotalCount: 1}, Items: []api.CollectionResource{{Resource: api.Resource{ID: "c"}}}}, nil
}

func TestClientSourcePagesAndCancellation(t *testing.T) {
	t.Parallel()
	client := &fakeClientReader{}
	source := NewClientSource(client)
	providerPage, err := source.ListProviders(context.Background(), 20, 10)
	if err != nil || providerPage.TotalCount != 1 || providerPage.Items[0].Resource.ID != "p" || client.providerOptions.Get("offset") != "20" || client.providerOptions.Get("limit") != "10" {
		t.Fatalf("provider page mismatch: %+v %v %v", providerPage, client.providerOptions, err)
	}
	collectionPage, err := source.ListCollections(context.Background(), 40, 5)
	if err != nil || collectionPage.TotalCount != 1 || collectionPage.Items[0].Resource.ID != "c" || client.collectionOptions.Get("offset") != "40" || client.collectionOptions.Get("limit") != "5" {
		t.Fatalf("collection page mismatch: %+v %v %v", collectionPage, client.collectionOptions, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = source.ListProviders(ctx, 0, 1)
	if !errors.Is(err, context.Canceled) || client.providerCalls != 1 {
		t.Fatalf("canceled request reached HTTP client: %v calls=%d", err, client.providerCalls)
	}
	_, err = source.ListCollections(ctx, 0, 1)
	if !errors.Is(err, context.Canceled) || client.collectionCalls != 1 {
		t.Fatalf("canceled request reached collection HTTP client: %v calls=%d", err, client.collectionCalls)
	}
}

func TestClientSourceErrorsAndNilResults(t *testing.T) {
	t.Parallel()
	providerErr := errors.New("provider client failed")
	providerCases := []struct {
		name   string
		client *fakeClientReader
		want   error
	}{
		{name: "client error", client: &fakeClientReader{providerErr: providerErr}, want: providerErr},
		{name: "nil result", client: &fakeClientReader{providerNil: true}, want: errors.New("provider list is nil")},
	}
	for _, tc := range providerCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewClientSource(tc.client).ListProviders(context.Background(), 0, 1)
			if err == nil || err.Error() != tc.want.Error() {
				t.Fatalf("ListProviders error = %v, want %v", err, tc.want)
			}
		})
	}

	collectionErr := errors.New("collection client failed")
	collectionCases := []struct {
		name   string
		client *fakeClientReader
		want   error
	}{
		{name: "client error", client: &fakeClientReader{collectionErr: collectionErr}, want: collectionErr},
		{name: "nil result", client: &fakeClientReader{collectionNil: true}, want: errors.New("collection list is nil")},
	}
	for _, tc := range collectionCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewClientSource(tc.client).ListCollections(context.Background(), 0, 1)
			if err == nil || err.Error() != tc.want.Error() {
				t.Fatalf("ListCollections error = %v, want %v", err, tc.want)
			}
		})
	}
}

type scopedStorage struct {
	abstractions.Storage
	ctx    context.Context
	tenant api.Tenant
	owner  api.User
	reads  *storageReads
}

type storageReads struct {
	providerFilter   *abstractions.QueryFilter
	collectionFilter *abstractions.QueryFilter
	providerErr      error
	providerNil      bool
	collectionErr    error
	collectionNil    bool
}

func (s *scopedStorage) WithContext(ctx context.Context) abstractions.Storage {
	copy := *s
	copy.ctx = ctx
	return &copy
}

func (s *scopedStorage) WithTenant(tenant api.Tenant) abstractions.Storage {
	copy := *s
	copy.tenant = tenant
	return &copy
}

func (s *scopedStorage) WithOwner(owner api.User) abstractions.Storage {
	copy := *s
	copy.owner = owner
	return &copy
}

func (s *scopedStorage) GetProviders(filter *abstractions.QueryFilter) (*abstractions.QueryResults[api.ProviderResource], error) {
	if s.tenant != "tenant-a" || s.owner != "alice" || s.ctx == nil || s.ctx.Value(requestKey{}) != "request-a" {
		return nil, errors.New("provider read lost request scope")
	}
	s.reads.providerFilter = filter
	if s.reads.providerErr != nil || s.reads.providerNil {
		return nil, s.reads.providerErr
	}
	return &abstractions.QueryResults[api.ProviderResource]{Items: []api.ProviderResource{{Resource: api.Resource{ID: "p"}}}, TotalCount: 1}, nil
}

func (s *scopedStorage) GetCollections(filter *abstractions.QueryFilter) (*abstractions.QueryResults[api.CollectionResource], error) {
	if s.tenant != "tenant-a" || s.owner != "alice" || s.ctx == nil || s.ctx.Value(requestKey{}) != "request-a" {
		return nil, errors.New("collection read lost request scope")
	}
	s.reads.collectionFilter = filter
	if s.reads.collectionErr != nil || s.reads.collectionNil {
		return nil, s.reads.collectionErr
	}
	return &abstractions.QueryResults[api.CollectionResource]{Items: []api.CollectionResource{{Resource: api.Resource{ID: "c"}}}, TotalCount: 1}, nil
}

type requestKey struct{}

func TestStorageSourceRetainsRequestScope(t *testing.T) {
	t.Parallel()
	reads := &storageReads{}
	store := (&scopedStorage{reads: reads}).WithTenant("tenant-a").WithOwner("alice").(*scopedStorage)
	source := NewStorageSource(store)
	ctx := context.WithValue(context.Background(), requestKey{}, "request-a")
	providers, err := source.ListProviders(ctx, 10, 5)
	if err != nil || providers.TotalCount != 1 || providers.Items[0].Resource.ID != "p" {
		t.Fatalf("provider read: %+v %v", providers, err)
	}
	collections, err := source.ListCollections(ctx, 20, 3)
	if err != nil || collections.TotalCount != 1 || collections.Items[0].Resource.ID != "c" {
		t.Fatalf("collection read: %+v %v", collections, err)
	}
	if reads.providerFilter == nil || reads.providerFilter.Offset != 10 || reads.providerFilter.Limit != 5 ||
		reads.collectionFilter == nil || reads.collectionFilter.Offset != 20 || reads.collectionFilter.Limit != 3 {
		t.Fatalf("storage queries lost pagination: provider=%+v collection=%+v", reads.providerFilter, reads.collectionFilter)
	}
	if store.ctx != nil {
		t.Fatal("adapter mutated the caller's scoped storage context")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := source.ListProviders(canceled, 0, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled provider storage read: %v", err)
	}
	if _, err := source.ListCollections(canceled, 0, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled storage read: %v", err)
	}
}

func TestStorageSourceErrorsAndNilResults(t *testing.T) {
	t.Parallel()
	providerErr := errors.New("provider storage failed")
	collectionErr := errors.New("collection storage failed")
	for _, tc := range []struct {
		name  string
		reads *storageReads
		call  func(CatalogSource, context.Context) error
		want  string
	}{
		{name: "provider error", reads: &storageReads{providerErr: providerErr}, call: func(source CatalogSource, ctx context.Context) error {
			_, err := source.ListProviders(ctx, 0, 1)
			return err
		}, want: providerErr.Error()},
		{name: "provider nil result", reads: &storageReads{providerNil: true}, call: func(source CatalogSource, ctx context.Context) error {
			_, err := source.ListProviders(ctx, 0, 1)
			return err
		}, want: "provider query result is nil"},
		{name: "collection error", reads: &storageReads{collectionErr: collectionErr}, call: func(source CatalogSource, ctx context.Context) error {
			_, err := source.ListCollections(ctx, 0, 1)
			return err
		}, want: collectionErr.Error()},
		{name: "collection nil result", reads: &storageReads{collectionNil: true}, call: func(source CatalogSource, ctx context.Context) error {
			_, err := source.ListCollections(ctx, 0, 1)
			return err
		}, want: "collection query result is nil"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := (&scopedStorage{reads: tc.reads}).WithTenant("tenant-a").WithOwner("alice")
			ctx := context.WithValue(context.Background(), requestKey{}, "request-a")
			err := tc.call(NewStorageSource(store), ctx)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("storage adapter error = %v, want %q", err, tc.want)
			}
		})
	}
}
