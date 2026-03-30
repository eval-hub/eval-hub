package handlers_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/constants"
	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/handlers"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/internal/eval_hub/validation"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// collection methods for fakeStorage - required for Storage interface
func (f *fakeStorage) GetCollections(_ *abstractions.QueryFilter) (*abstractions.QueryResults[api.CollectionResource], error) {
	return &abstractions.QueryResults[api.CollectionResource]{Items: []api.CollectionResource{}, TotalCount: 0}, nil
}

func (f *fakeStorage) CreateCollection(_ *api.CollectionResource) error {
	return nil
}

func (f *fakeStorage) GetCollection(id string) (*api.CollectionResource, error) {
	return nil, serviceerrors.NewServiceError(messages.ResourceNotFound, "Type", "collection", "ResourceId", id)
}

func (f *fakeStorage) UpdateCollection(_ string, _ *api.CollectionConfig) (*api.CollectionResource, error) {
	return nil, nil
}

func (f *fakeStorage) PatchCollection(_ string, _ *api.Patch) (*api.CollectionResource, error) {
	return nil, nil
}

func (f *fakeStorage) DeleteCollection(_ string) error {
	return nil
}

type listCollectionsStorage struct {
	*fakeStorage
	collections []api.CollectionResource
	err         error
}

func (s *listCollectionsStorage) WithLogger(_ *slog.Logger) abstractions.Storage {
	copy := *s
	return &copy
}
func (s *listCollectionsStorage) WithContext(_ context.Context) abstractions.Storage {
	copy := *s
	return &copy
}
func (s *listCollectionsStorage) WithTenant(_ api.Tenant) abstractions.Storage {
	copy := *s
	return &copy
}
func (s *listCollectionsStorage) WithOwner(_ api.User) abstractions.Storage {
	copy := *s
	return &copy
}

func (s *listCollectionsStorage) GetCollections(_ *abstractions.QueryFilter) (*abstractions.QueryResults[api.CollectionResource], error) {
	if s.err != nil {
		return nil, s.err
	}
	return &abstractions.QueryResults[api.CollectionResource]{
		Items:      s.collections,
		TotalCount: len(s.collections),
	}, nil
}

type getCollectionStorage struct {
	*fakeStorage
	collection *api.CollectionResource
	err        error
}

func (s *getCollectionStorage) WithLogger(_ *slog.Logger) abstractions.Storage {
	copy := *s
	return &copy
}
func (s *getCollectionStorage) WithContext(_ context.Context) abstractions.Storage {
	copy := *s
	return &copy
}
func (s *getCollectionStorage) WithTenant(_ api.Tenant) abstractions.Storage {
	copy := *s
	return &copy
}
func (s *getCollectionStorage) WithOwner(_ api.User) abstractions.Storage {
	copy := *s
	return &copy
}

func (s *getCollectionStorage) GetCollection(id string) (*api.CollectionResource, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.collection != nil && s.collection.Resource.ID == id {
		return s.collection, nil
	}
	return nil, serviceerrors.NewServiceError(messages.ResourceNotFound, "Type", "collection", "ResourceId", id)
}

type createCollectionStorage struct {
	*fakeStorage
	created *api.CollectionResource
	err     error
}

func (s *createCollectionStorage) WithLogger(_ *slog.Logger) abstractions.Storage {
	copy := *s
	return &copy
}
func (s *createCollectionStorage) WithContext(_ context.Context) abstractions.Storage {
	copy := *s
	return &copy
}
func (s *createCollectionStorage) WithTenant(_ api.Tenant) abstractions.Storage {
	copy := *s
	return &copy
}
func (s *createCollectionStorage) WithOwner(_ api.User) abstractions.Storage {
	copy := *s
	return &copy
}

func (s *createCollectionStorage) CreateCollection(c *api.CollectionResource) error {
	if s.err != nil {
		return s.err
	}
	s.created = c
	return nil
}

type updatePatchDeleteCollectionStorage struct {
	*fakeStorage
	collection *api.CollectionResource
	updateErr  error
	patchErr   error
	deleteErr  error
}

func (s *updatePatchDeleteCollectionStorage) WithLogger(_ *slog.Logger) abstractions.Storage {
	return s
}
func (s *updatePatchDeleteCollectionStorage) WithContext(_ context.Context) abstractions.Storage {
	return s
}
func (s *updatePatchDeleteCollectionStorage) WithTenant(_ api.Tenant) abstractions.Storage { return s }
func (s *updatePatchDeleteCollectionStorage) WithOwner(_ api.User) abstractions.Storage    { return s }

func (s *updatePatchDeleteCollectionStorage) GetCollection(id string) (*api.CollectionResource, error) {
	if s.collection != nil && s.collection.Resource.ID == id {
		return s.collection, nil
	}
	return nil, serviceerrors.NewServiceError(messages.ResourceNotFound, "Type", "collection", "ResourceId", id)
}

func (s *updatePatchDeleteCollectionStorage) UpdateCollection(id string, c *api.CollectionConfig) (*api.CollectionResource, error) {
	if s.updateErr != nil {
		return nil, s.updateErr
	}
	s.collection = &api.CollectionResource{
		Resource: api.Resource{
			ID: id,
		},
		CollectionConfig: *c,
	}
	return s.collection, nil
}

func (s *updatePatchDeleteCollectionStorage) PatchCollection(id string, patches *api.Patch) (*api.CollectionResource, error) {
	if s.patchErr != nil {
		return nil, s.patchErr
	}
	if s.collection != nil && s.collection.Resource.ID == id {
		for _, p := range *patches {
			if p.Op == api.PatchOpReplace && p.Path == "/name" {
				if v, ok := p.Value.(string); ok {
					s.collection.Name = v
				}
			}
		}
	}
	return s.collection, nil
}

func (s *updatePatchDeleteCollectionStorage) DeleteCollection(id string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	return nil
}

func TestHandleListCollections(t *testing.T) {
	collections := []api.CollectionResource{
		{
			Resource: api.Resource{ID: "coll-1"},
			CollectionConfig: api.CollectionConfig{
				Name:        "Collection 1",
				Description: "Test collection",
				Benchmarks:  []api.CollectionBenchmarkConfig{{Ref: api.Ref{ID: "b1"}, ProviderID: "p1"}},
			},
		},
	}
	storage := &listCollectionsStorage{
		fakeStorage: &fakeStorage{},
		collections: collections,
	}
	validate := validation.NewValidator()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.New(storage, validate, &fakeRuntime{}, nil, nil)

	req := &providersRequest{
		MockRequest: createMockRequest("GET", "/api/v1/evaluations/collections"),
		queryValues: map[string][]string{},
		pathValues:  map[string]string{},
	}
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, time.Second, "test-user", "test-tenant")

	h.HandleListCollections(ctx, req, resp)

	if recorder.Code != 200 {
		t.Fatalf("expected status 200, got %d body %s", recorder.Code, recorder.Body.String())
	}
	var got api.CollectionResourceList
	if err := json.NewDecoder(recorder.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.TotalCount != 1 {
		t.Errorf("expected TotalCount 1, got %d", got.TotalCount)
	}
	if len(got.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(got.Items))
	}
	if got.Items[0].Resource.ID != "coll-1" {
		t.Errorf("expected id coll-1, got %s", got.Items[0].Resource.ID)
	}
	if got.Items[0].Name != "Collection 1" {
		t.Errorf("expected name Collection 1, got %s", got.Items[0].Name)
	}
}

func TestEnrichBenchmarkURLsFromProviders(t *testing.T) {
	t.Parallel()
	t.Run("fills URL from provider", func(t *testing.T) {
		t.Parallel()
		storage := &fakeStorage{
			providerConfigs: map[string]api.ProviderResource{
				"prov": {
					Resource: api.Resource{ID: "prov"},
					ProviderConfig: api.ProviderConfig{
						Benchmarks: []api.BenchmarkResource{{ID: "b1", URL: "https://u.example/b1"}},
					},
				},
			},
		}
		coll := &api.CollectionResource{
			CollectionConfig: api.CollectionConfig{
				Benchmarks: []api.CollectionBenchmarkConfig{{Ref: api.Ref{ID: "b1"}, ProviderID: "prov"}},
			},
		}
		handlers.EnrichBenchmarkURLsFromProviders(storage, coll)
		if coll.Benchmarks[0].URL != "https://u.example/b1" {
			t.Fatalf("URL = %q", coll.Benchmarks[0].URL)
		}
	})
	t.Run("clears stale URL when provider missing", func(t *testing.T) {
		t.Parallel()
		storage := &fakeStorage{providerConfigs: map[string]api.ProviderResource{}}
		coll := &api.CollectionResource{
			CollectionConfig: api.CollectionConfig{
				Benchmarks: []api.CollectionBenchmarkConfig{{
					Ref:        api.Ref{ID: "b1"},
					ProviderID: "missing",
					URL:        "https://stale.example/old",
				}},
			},
		}
		handlers.EnrichBenchmarkURLsFromProviders(storage, coll)
		if coll.Benchmarks[0].URL != "" {
			t.Fatalf("want empty URL, got %q", coll.Benchmarks[0].URL)
		}
	})
	t.Run("clears stale URL when benchmark not on provider", func(t *testing.T) {
		t.Parallel()
		storage := &fakeStorage{
			providerConfigs: map[string]api.ProviderResource{
				"prov": {
					Resource: api.Resource{ID: "prov"},
					ProviderConfig: api.ProviderConfig{
						Benchmarks: []api.BenchmarkResource{{ID: "other", URL: "https://u.example/other"}},
					},
				},
			},
		}
		coll := &api.CollectionResource{
			CollectionConfig: api.CollectionConfig{
				Benchmarks: []api.CollectionBenchmarkConfig{{
					Ref:        api.Ref{ID: "b1"},
					ProviderID: "prov",
					URL:        "https://stale.example/old",
				}},
			},
		}
		handlers.EnrichBenchmarkURLsFromProviders(storage, coll)
		if coll.Benchmarks[0].URL != "" {
			t.Fatalf("want empty URL, got %q", coll.Benchmarks[0].URL)
		}
	})
}

func TestHandleListCollections_ReturnsStoredBenchmarkURL(t *testing.T) {
	collections := []api.CollectionResource{
		{
			Resource: api.Resource{ID: "coll-1"},
			CollectionConfig: api.CollectionConfig{
				Name: "C",
				Benchmarks: []api.CollectionBenchmarkConfig{{
					Ref:        api.Ref{ID: "sweep"},
					ProviderID: "guidellm",
					URL:        "https://example.com/sweep",
				}},
			},
		},
	}
	storage := &listCollectionsStorage{
		fakeStorage: &fakeStorage{},
		collections: collections,
	}
	validate := validation.NewValidator()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.New(storage, validate, &fakeRuntime{}, nil, nil)

	req := &providersRequest{
		MockRequest: createMockRequest("GET", "/api/v1/evaluations/collections"),
		queryValues: map[string][]string{},
		pathValues:  map[string]string{},
	}
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, time.Second, "test-user", "test-tenant")

	h.HandleListCollections(ctx, req, resp)

	if recorder.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var got api.CollectionResourceList
	if err := json.NewDecoder(recorder.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Items) != 1 || len(got.Items[0].Benchmarks) != 1 {
		t.Fatalf("unexpected items: %+v", got.Items)
	}
	if got.Items[0].Benchmarks[0].URL != "https://example.com/sweep" {
		t.Errorf("benchmark url = %q, want https://example.com/sweep", got.Items[0].Benchmarks[0].URL)
	}
}

func TestHandleGetCollection_ReturnsStoredBenchmarkURL(t *testing.T) {
	coll := &api.CollectionResource{
		Resource: api.Resource{ID: "coll-1"},
		CollectionConfig: api.CollectionConfig{
			Name: "C",
			Benchmarks: []api.CollectionBenchmarkConfig{{
				Ref:        api.Ref{ID: "sweep"},
				ProviderID: "guidellm",
				URL:        "https://example.com/sweep",
			}},
		},
	}
	storage := &getCollectionStorage{
		fakeStorage: &fakeStorage{},
		collection:  coll,
	}
	validate := validation.NewValidator()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.New(storage, validate, &fakeRuntime{}, nil, nil)

	req := &providersRequest{
		MockRequest: createMockRequest("GET", "/api/v1/evaluations/collections/coll-1"),
		queryValues: map[string][]string{},
		pathValues:  map[string]string{constants.PATH_PARAMETER_COLLECTION_ID: "coll-1"},
	}
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, time.Second, "test-user", "test-tenant")

	h.HandleGetCollection(ctx, req, resp)

	if recorder.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var got api.CollectionResource
	if err := json.NewDecoder(recorder.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Benchmarks) != 1 {
		t.Fatalf("unexpected benchmarks: %+v", got.Benchmarks)
	}
	if got.Benchmarks[0].URL != "https://example.com/sweep" {
		t.Errorf("benchmark url = %q, want https://example.com/sweep", got.Benchmarks[0].URL)
	}
}

func TestHandleListCollections_StorageError(t *testing.T) {
	storage := &listCollectionsStorage{
		fakeStorage: &fakeStorage{},
		err:         serviceerrors.NewServiceError(messages.InternalServerError, "Error", "db error"),
	}
	validate := validation.NewValidator()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.New(storage, validate, &fakeRuntime{}, nil, nil)

	req := &providersRequest{
		MockRequest: createMockRequest("GET", "/api/v1/evaluations/collections"),
		queryValues: map[string][]string{},
		pathValues:  map[string]string{},
	}
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, time.Second, "test-user", "test-tenant")

	h.HandleListCollections(ctx, req, resp)

	if recorder.Code < 400 {
		t.Fatalf("expected error status, got %d", recorder.Code)
	}
}

func TestHandleCreateCollection(t *testing.T) {
	storage := &createCollectionStorage{fakeStorage: &fakeStorage{
		providerConfigs: map[string]api.ProviderResource{
			"p1": {
				Resource: api.Resource{ID: "p1"},
				ProviderConfig: api.ProviderConfig{
					Benchmarks: []api.BenchmarkResource{{ID: "b1", URL: "https://example.com/b1"}},
				},
			},
		},
	}}
	validate := validation.NewValidator()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.New(storage, validate, &fakeRuntime{}, nil, nil)

	body := `
	{
	  "name": "My Collection",
	  "description": "A test collection",
	  "category": "test",
	  "benchmarks":[
	    {
	      "id": "b1",
		  "provider_id": "p1"
		}
	  ]
	}`

	req := &providersRequest{
		MockRequest: createMockRequest("POST", "/api/v1/evaluations/collections"),
		queryValues: map[string][]string{},
		pathValues:  map[string]string{},
	}
	req.SetBody([]byte(body))
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, time.Second, "test-user", "test-tenant")

	h.HandleCreateCollection(ctx, req, resp)

	if recorder.Code != 201 {
		t.Fatalf("expected status 201, got %d body %s", recorder.Code, recorder.Body.String())
	}
	var got api.CollectionResource
	if err := json.NewDecoder(recorder.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Resource.ID == "" {
		t.Error("expected non-empty resource ID")
	}
	if got.Name != "My Collection" {
		t.Errorf("expected name My Collection, got %s", got.Name)
	}
	if len(got.Benchmarks) != 1 {
		t.Fatalf("expected 1 benchmark, got %d", len(got.Benchmarks))
	}
	if got.Benchmarks[0].URL != "https://example.com/b1" {
		t.Errorf("benchmark url = %q, want https://example.com/b1", got.Benchmarks[0].URL)
	}
}

func TestHandleGetCollection(t *testing.T) {
	coll := &api.CollectionResource{
		Resource: api.Resource{ID: "coll-123"},
		CollectionConfig: api.CollectionConfig{
			Name:        "Found Collection",
			Description: "Test",
			Benchmarks:  []api.CollectionBenchmarkConfig{},
		},
	}
	storage := &getCollectionStorage{fakeStorage: &fakeStorage{}, collection: coll}
	validate := validation.NewValidator()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.New(storage, validate, &fakeRuntime{}, nil, nil)

	req := &providersRequest{
		MockRequest: createMockRequest("GET", "/api/v1/evaluations/collections/coll-123"),
		queryValues: map[string][]string{},
		pathValues:  map[string]string{constants.PATH_PARAMETER_COLLECTION_ID: "coll-123"},
	}
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, time.Second, "test-user", "test-tenant")

	h.HandleGetCollection(ctx, req, resp)

	if recorder.Code != 200 {
		t.Fatalf("expected status 200, got %d body %s", recorder.Code, recorder.Body.String())
	}
	var got api.CollectionResource
	if err := json.NewDecoder(recorder.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Resource.ID != "coll-123" {
		t.Errorf("expected id coll-123, got %s", got.Resource.ID)
	}
}

func TestHandleGetCollection_MissingPathParam(t *testing.T) {
	storage := &fakeStorage{}
	validate := validation.NewValidator()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.New(storage, validate, &fakeRuntime{}, nil, nil)

	req := &providersRequest{
		MockRequest: createMockRequest("GET", "/api/v1/evaluations/collections/"),
		queryValues: map[string][]string{},
		pathValues:  map[string]string{}, // no collection_id
	}
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, time.Second, "test-user", "test-tenant")

	h.HandleGetCollection(ctx, req, resp)

	if recorder.Code != 400 {
		t.Fatalf("expected status 400 for missing path param, got %d", recorder.Code)
	}
}

func TestHandleUpdateCollection(t *testing.T) {
	storage := &updatePatchDeleteCollectionStorage{
		fakeStorage: &fakeStorage{
			providerConfigs: map[string]api.ProviderResource{
				"p1": {
					Resource: api.Resource{ID: "p1"},
					ProviderConfig: api.ProviderConfig{
						Benchmarks: []api.BenchmarkResource{{ID: "b1", URL: "https://example.com/b1"}},
					},
				},
			},
		},
		collection: &api.CollectionResource{
			Resource: api.Resource{ID: "coll-update"},
			CollectionConfig: api.CollectionConfig{
				Name:        "Original",
				Description: "Original",
				Benchmarks:  []api.CollectionBenchmarkConfig{{Ref: api.Ref{ID: "b1"}, ProviderID: "p1"}},
			},
		},
	}
	validate := validation.NewValidator()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.New(storage, validate, &fakeRuntime{}, nil, nil)

	body := `
	{
	  "name": "Updated Name",
	  "description": "Updated desc",
	  "category": "test",
	  "benchmarks":[
	    {
	      "id": "b1",
		  "provider_id": "p1"
		}
	  ]
	}`

	req := &providersRequest{
		MockRequest: createMockRequest("PUT", "/api/v1/evaluations/collections/coll-update"),
		pathValues:  map[string]string{constants.PATH_PARAMETER_COLLECTION_ID: "coll-update"},
	}
	req.SetBody([]byte(body))
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, time.Second, "test-user", "test-tenant")

	h.HandleUpdateCollection(ctx, req, resp)

	if recorder.Code != 200 {
		t.Fatalf("expected status 200, got %d body %s", recorder.Code, recorder.Body.String())
	}
	var got api.CollectionResource
	if err := json.NewDecoder(recorder.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Name != "Updated Name" {
		t.Errorf("expected name Updated Name, got %s", got.Name)
	}
	if len(got.Benchmarks) != 1 || got.Benchmarks[0].URL != "https://example.com/b1" {
		t.Errorf("expected benchmark URL from provider on update, got %+v", got.Benchmarks)
	}
}

// patchReceivedHook holds the patch pointer last passed to PatchCollection (shared across WithContext copies).
type patchReceivedHook struct {
	patches *api.Patch
}

type patchCaptureCollectionStorage struct {
	*fakeStorage
	collection *api.CollectionResource
	hook       *patchReceivedHook
}

func (s *patchCaptureCollectionStorage) WithLogger(_ *slog.Logger) abstractions.Storage {
	c := *s
	return &c
}
func (s *patchCaptureCollectionStorage) WithContext(_ context.Context) abstractions.Storage {
	c := *s
	return &c
}
func (s *patchCaptureCollectionStorage) WithTenant(_ api.Tenant) abstractions.Storage {
	c := *s
	return &c
}
func (s *patchCaptureCollectionStorage) WithOwner(_ api.User) abstractions.Storage {
	c := *s
	return &c
}

func (s *patchCaptureCollectionStorage) PatchCollection(_ string, patches *api.Patch) (*api.CollectionResource, error) {
	if s.hook != nil {
		s.hook.patches = patches
	}
	return s.collection, nil
}

func TestHandlePatchCollection_EnrichesFullBenchmarkElementBeforeStorage(t *testing.T) {
	hook := &patchReceivedHook{}
	storage := &patchCaptureCollectionStorage{
		fakeStorage: &fakeStorage{
			providerConfigs: map[string]api.ProviderResource{
				"p1": {
					Resource: api.Resource{ID: "p1"},
					ProviderConfig: api.ProviderConfig{
						Benchmarks: []api.BenchmarkResource{{ID: "b1", URL: "https://patch.example/b1"}},
					},
				},
			},
		},
		collection: &api.CollectionResource{
			Resource: api.Resource{ID: "coll-patch-url"},
			CollectionConfig: api.CollectionConfig{
				Name:     "n",
				Category: "c",
				Benchmarks: []api.CollectionBenchmarkConfig{
					{Ref: api.Ref{ID: "b1"}, ProviderID: "p1", URL: "https://patch.example/b1"},
				},
			},
		},
		hook: hook,
	}
	validate := validation.NewValidator()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.New(storage, validate, &fakeRuntime{}, nil, nil)

	body := `[{"op":"replace","path":"/benchmarks/0","value":{"id":"b1","provider_id":"p1"}}]`
	req := &providersRequest{
		MockRequest: createMockRequest("PATCH", "/api/v1/evaluations/collections/coll-patch-url"),
		pathValues:  map[string]string{constants.PATH_PARAMETER_COLLECTION_ID: "coll-patch-url"},
	}
	req.SetBody([]byte(body))
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, time.Second, "test-user", "test-tenant")

	h.HandlePatchCollection(ctx, req, resp)

	if recorder.Code != 200 {
		t.Fatalf("expected status 200, got %d body %s", recorder.Code, recorder.Body.String())
	}
	if hook.patches == nil || len(*hook.patches) != 1 {
		t.Fatalf("expected one patch passed to storage, got %#v", hook.patches)
	}
	m, ok := (*hook.patches)[0].Value.(map[string]any)
	if !ok {
		t.Fatalf("expected patch value map, got %T", (*hook.patches)[0].Value)
	}
	if m["url"] != "https://patch.example/b1" {
		t.Fatalf("storage should receive enriched url, got %#v", m["url"])
	}
}

func TestHandlePatchCollection_EnrichesFullBenchmarksArrayBeforeStorage(t *testing.T) {
	hook := &patchReceivedHook{}
	storage := &patchCaptureCollectionStorage{
		fakeStorage: &fakeStorage{
			providerConfigs: map[string]api.ProviderResource{
				"p1": {
					Resource: api.Resource{ID: "p1"},
					ProviderConfig: api.ProviderConfig{
						Benchmarks: []api.BenchmarkResource{{ID: "b1", URL: "https://patch.example/b1"}},
					},
				},
				"p2": {
					Resource: api.Resource{ID: "p2"},
					ProviderConfig: api.ProviderConfig{
						Benchmarks: []api.BenchmarkResource{{ID: "b2", URL: "https://patch.example/b2"}},
					},
				},
			},
		},
		collection: &api.CollectionResource{
			Resource: api.Resource{ID: "coll-patch-url-array"},
			CollectionConfig: api.CollectionConfig{
				Name:     "n",
				Category: "c",
				Benchmarks: []api.CollectionBenchmarkConfig{
					{Ref: api.Ref{ID: "b1"}, ProviderID: "p1", URL: "https://patch.example/b1"},
				},
			},
		},
		hook: hook,
	}
	validate := validation.NewValidator()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.New(storage, validate, &fakeRuntime{}, nil, nil)

	body := `[{"op":"replace","path":"/benchmarks","value":[{"id":"b1","provider_id":"p1"},{"id":"b2","provider_id":"p2"}]}]`
	req := &providersRequest{
		MockRequest: createMockRequest("PATCH", "/api/v1/evaluations/collections/coll-patch-url-array"),
		pathValues:  map[string]string{constants.PATH_PARAMETER_COLLECTION_ID: "coll-patch-url-array"},
	}
	req.SetBody([]byte(body))
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, time.Second, "test-user", "test-tenant")

	h.HandlePatchCollection(ctx, req, resp)

	if recorder.Code != 200 {
		t.Fatalf("expected status 200, got %d body %s", recorder.Code, recorder.Body.String())
	}
	if hook.patches == nil || len(*hook.patches) != 1 {
		t.Fatalf("expected one patch passed to storage, got %#v", hook.patches)
	}
	arr, ok := (*hook.patches)[0].Value.([]any)
	if !ok {
		t.Fatalf("expected patch value array, got %T", (*hook.patches)[0].Value)
	}
	if len(arr) != 2 {
		t.Fatalf("expected two benchmarks in patch value, got %d", len(arr))
	}
	m0, _ := arr[0].(map[string]any)
	m1, _ := arr[1].(map[string]any)
	if m0["url"] != "https://patch.example/b1" || m1["url"] != "https://patch.example/b2" {
		t.Fatalf("storage should receive enriched urls, got %#v and %#v", m0["url"], m1["url"])
	}
}

func TestHandlePatchCollection(t *testing.T) {
	storage := &updatePatchDeleteCollectionStorage{
		fakeStorage: &fakeStorage{},
		collection: &api.CollectionResource{
			Resource: api.Resource{ID: "coll-patch"},
			CollectionConfig: api.CollectionConfig{
				Name:        "Original",
				Description: "Original",
				Benchmarks:  []api.CollectionBenchmarkConfig{},
			},
		},
	}
	validate := validation.NewValidator()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.New(storage, validate, &fakeRuntime{}, nil, nil)

	body := `[{"op":"replace","path":"/name","value":"Patched Name"}]`
	req := &providersRequest{
		MockRequest: createMockRequest("PATCH", "/api/v1/evaluations/collections/coll-patch"),
		pathValues:  map[string]string{constants.PATH_PARAMETER_COLLECTION_ID: "coll-patch"},
	}
	req.SetBody([]byte(body))
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, time.Second, "test-user", "test-tenant")

	h.HandlePatchCollection(ctx, req, resp)

	if recorder.Code != 200 {
		t.Fatalf("expected status 200, got %d body %s", recorder.Code, recorder.Body.String())
	}
}

func TestHandleDeleteCollection(t *testing.T) {
	storage := &updatePatchDeleteCollectionStorage{fakeStorage: &fakeStorage{}}
	validate := validation.NewValidator()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.New(storage, validate, &fakeRuntime{}, nil, nil)

	req := &providersRequest{
		MockRequest: createMockRequest("DELETE", "/api/v1/evaluations/collections/coll-del"),
		pathValues:  map[string]string{constants.PATH_PARAMETER_COLLECTION_ID: "coll-del"},
	}
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, time.Second, "test-user", "test-tenant")

	h.HandleDeleteCollection(ctx, req, resp)

	if recorder.Code != 204 {
		t.Fatalf("expected status 204, got %d", recorder.Code)
	}
}

// tenantTrackingStorage records tenant and owner passed via WithTenant/WithOwner.
type tenantTrackingStorage struct {
	*fakeStorage
	tenant api.Tenant
	owner  api.User
}

func (s *tenantTrackingStorage) WithLogger(_ *slog.Logger) abstractions.Storage     { return s }
func (s *tenantTrackingStorage) WithContext(_ context.Context) abstractions.Storage { return s }
func (s *tenantTrackingStorage) WithTenant(t api.Tenant) abstractions.Storage       { s.tenant = t; return s }
func (s *tenantTrackingStorage) WithOwner(u api.User) abstractions.Storage          { s.owner = u; return s }
func (s *tenantTrackingStorage) GetCollections(_ *abstractions.QueryFilter) (*abstractions.QueryResults[api.CollectionResource], error) {
	return &abstractions.QueryResults[api.CollectionResource]{Items: []api.CollectionResource{}, TotalCount: 0}, nil
}
func (s *tenantTrackingStorage) GetCollection(id string) (*api.CollectionResource, error) {
	return &api.CollectionResource{Resource: api.Resource{ID: id}}, nil
}
func (s *tenantTrackingStorage) CreateCollection(_ *api.CollectionResource) error { return nil }
func (s *tenantTrackingStorage) UpdateCollection(_ string, _ *api.CollectionConfig) (*api.CollectionResource, error) {
	return nil, nil
}
func (s *tenantTrackingStorage) PatchCollection(_ string, _ *api.Patch) (*api.CollectionResource, error) {
	return nil, nil
}
func (s *tenantTrackingStorage) DeleteCollection(_ string) error { return nil }

func TestCollectionHandlers_PropagateTenantAndOwner(t *testing.T) {
	validate := validation.NewValidator()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	tests := []struct {
		name    string
		method  string
		path    string
		body    string
		pathVal map[string]string
		handler func(h *handlers.Handlers, ctx *executioncontext.ExecutionContext, req *providersRequest, resp MockResponseWrapper)
	}{
		{
			name:   "ListCollections",
			method: "GET",
			path:   "/api/v1/evaluations/collections",
			handler: func(h *handlers.Handlers, ctx *executioncontext.ExecutionContext, req *providersRequest, resp MockResponseWrapper) {
				h.HandleListCollections(ctx, req, resp)
			},
		},
		{
			name:   "CreateCollection",
			method: "POST",
			path:   "/api/v1/evaluations/collections",
			body:   `{"name":"Test","benchmarks":[{"id":"b1","provider_id":"p1"}]}`,
			handler: func(h *handlers.Handlers, ctx *executioncontext.ExecutionContext, req *providersRequest, resp MockResponseWrapper) {
				h.HandleCreateCollection(ctx, req, resp)
			},
		},
		{
			name:    "GetCollection",
			method:  "GET",
			path:    "/api/v1/evaluations/collections/coll-1",
			pathVal: map[string]string{constants.PATH_PARAMETER_COLLECTION_ID: "coll-1"},
			handler: func(h *handlers.Handlers, ctx *executioncontext.ExecutionContext, req *providersRequest, resp MockResponseWrapper) {
				h.HandleGetCollection(ctx, req, resp)
			},
		},
		{
			name:    "UpdateCollection",
			method:  "PUT",
			path:    "/api/v1/evaluations/collections/coll-1",
			body:    `{"name":"Updated","benchmarks":[{"id":"b1","provider_id":"p1"}]}`,
			pathVal: map[string]string{constants.PATH_PARAMETER_COLLECTION_ID: "coll-1"},
			handler: func(h *handlers.Handlers, ctx *executioncontext.ExecutionContext, req *providersRequest, resp MockResponseWrapper) {
				h.HandleUpdateCollection(ctx, req, resp)
			},
		},
		{
			name:    "PatchCollection",
			method:  "PATCH",
			path:    "/api/v1/evaluations/collections/coll-1",
			body:    `[{"op":"replace","path":"/name","value":"Patched"}]`,
			pathVal: map[string]string{constants.PATH_PARAMETER_COLLECTION_ID: "coll-1"},
			handler: func(h *handlers.Handlers, ctx *executioncontext.ExecutionContext, req *providersRequest, resp MockResponseWrapper) {
				h.HandlePatchCollection(ctx, req, resp)
			},
		},
		{
			name:    "DeleteCollection",
			method:  "DELETE",
			path:    "/api/v1/evaluations/collections/coll-1",
			pathVal: map[string]string{constants.PATH_PARAMETER_COLLECTION_ID: "coll-1"},
			handler: func(h *handlers.Handlers, ctx *executioncontext.ExecutionContext, req *providersRequest, resp MockResponseWrapper) {
				h.HandleDeleteCollection(ctx, req, resp)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := &tenantTrackingStorage{fakeStorage: &fakeStorage{}}
			h := handlers.New(storage, validate, &fakeRuntime{}, nil, nil)

			req := &providersRequest{
				MockRequest: createMockRequest(tt.method, tt.path),
				queryValues: map[string][]string{},
				pathValues:  tt.pathVal,
			}
			if tt.body != "" {
				req.SetBody([]byte(tt.body))
			}
			recorder := httptest.NewRecorder()
			resp := MockResponseWrapper{recorder: recorder}
			ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, time.Second, "my-user", "my-tenant")

			tt.handler(h, ctx, req, resp)

			if storage.tenant != "my-tenant" {
				t.Errorf("expected tenant 'my-tenant', got '%s'", storage.tenant)
			}
			if storage.owner != "my-user" {
				t.Errorf("expected owner 'my-user', got '%s'", storage.owner)
			}
		})
	}
}
