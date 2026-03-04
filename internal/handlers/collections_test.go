package handlers_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/constants"
	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"github.com/eval-hub/eval-hub/internal/handlers"
	"github.com/eval-hub/eval-hub/internal/messages"
	"github.com/eval-hub/eval-hub/internal/serviceerrors"
	"github.com/eval-hub/eval-hub/internal/validation"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// collection methods for fakeStorage - required for Storage interface
func (f *fakeStorage) GetCollections(_ *abstractions.QueryFilter) (*abstractions.QueryResults[api.CollectionResource], error) {
	return &abstractions.QueryResults[api.CollectionResource]{Items: []api.CollectionResource{}, TotalStored: 0}, nil
}

func (f *fakeStorage) CreateCollection(_ *api.CollectionResource) error {
	return nil
}

func (f *fakeStorage) GetCollection(id string) (*api.CollectionResource, error) {
	return nil, serviceerrors.NewServiceError(messages.ResourceNotFound, "Type", "collection", "ResourceId", id)
}

func (f *fakeStorage) UpdateCollection(_ *api.CollectionResource) error {
	return nil
}

func (f *fakeStorage) PatchCollection(_ string, _ *api.Patch) error {
	return nil
}

func (f *fakeStorage) DeleteCollection(_ string) error {
	return nil
}

type listCollectionsStorage struct {
	*fakeStorage
	collections []api.CollectionResource
	err         error
}

func (s *listCollectionsStorage) WithLogger(_ *slog.Logger) abstractions.Storage { return s }
func (s *listCollectionsStorage) WithContext(_ context.Context) abstractions.Storage {
	return s
}
func (s *listCollectionsStorage) WithTenant(_ api.Tenant) abstractions.Storage { return s }

func (s *listCollectionsStorage) GetCollections(_ *abstractions.QueryFilter) (*abstractions.QueryResults[api.CollectionResource], error) {
	if s.err != nil {
		return nil, s.err
	}
	return &abstractions.QueryResults[api.CollectionResource]{
		Items:       s.collections,
		TotalStored: len(s.collections),
	}, nil
}

type getCollectionStorage struct {
	*fakeStorage
	collection *api.CollectionResource
	err        error
}

func (s *getCollectionStorage) WithLogger(_ *slog.Logger) abstractions.Storage { return s }
func (s *getCollectionStorage) WithContext(_ context.Context) abstractions.Storage {
	return s
}
func (s *getCollectionStorage) WithTenant(_ api.Tenant) abstractions.Storage { return s }

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

func (s *createCollectionStorage) WithLogger(_ *slog.Logger) abstractions.Storage { return s }
func (s *createCollectionStorage) WithContext(_ context.Context) abstractions.Storage {
	return s
}
func (s *createCollectionStorage) WithTenant(_ api.Tenant) abstractions.Storage { return s }

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

func (s *updatePatchDeleteCollectionStorage) GetCollection(id string) (*api.CollectionResource, error) {
	if s.collection != nil && s.collection.Resource.ID == id {
		return s.collection, nil
	}
	return nil, serviceerrors.NewServiceError(messages.ResourceNotFound, "Type", "collection", "ResourceId", id)
}

func (s *updatePatchDeleteCollectionStorage) UpdateCollection(c *api.CollectionResource) error {
	if s.updateErr != nil {
		return s.updateErr
	}
	s.collection = c
	return nil
}

func (s *updatePatchDeleteCollectionStorage) PatchCollection(id string, patches *api.Patch) error {
	if s.patchErr != nil {
		return s.patchErr
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
	return nil
}

func (s *updatePatchDeleteCollectionStorage) DeleteCollection(id string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	return nil
}

func TestHandleListCollections(t *testing.T) {
	desc := "Test collection"
	collections := []api.CollectionResource{
		{
			Resource: api.Resource{ID: "coll-1"},
			CollectionConfig: api.CollectionConfig{
				Name:        "Collection 1",
				Description: &desc,
				Benchmarks:  []api.BenchmarkConfig{{Ref: api.Ref{ID: "b1"}, ProviderID: "p1"}},
			},
		},
	}
	storage := &listCollectionsStorage{
		fakeStorage: &fakeStorage{},
		collections: collections,
	}
	validate, err := validation.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.New(storage, validate, &fakeRuntime{}, nil, nil, nil)

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

func TestHandleListCollections_StorageError(t *testing.T) {
	storage := &listCollectionsStorage{
		fakeStorage: &fakeStorage{},
		err:         serviceerrors.NewServiceError(messages.InternalServerError, "Error", "db error"),
	}
	validate, _ := validation.NewValidator()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.New(storage, validate, &fakeRuntime{}, nil, nil, nil)

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
	storage := &createCollectionStorage{fakeStorage: &fakeStorage{}}
	validate, err := validation.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.New(storage, validate, &fakeRuntime{}, nil, nil, nil)

	body := `{"name":"My Collection","description":"A test collection","benchmarks":[{"id":"b1","provider_id":"p1"}]}`
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

	if recorder.Code != 202 {
		t.Fatalf("expected status 202, got %d body %s", recorder.Code, recorder.Body.String())
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
}

func TestHandleGetCollection(t *testing.T) {
	desc := "Test"
	coll := &api.CollectionResource{
		Resource: api.Resource{ID: "coll-123"},
		CollectionConfig: api.CollectionConfig{
			Name:        "Found Collection",
			Description: &desc,
			Benchmarks:  []api.BenchmarkConfig{},
		},
	}
	storage := &getCollectionStorage{fakeStorage: &fakeStorage{}, collection: coll}
	validate, _ := validation.NewValidator()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.New(storage, validate, &fakeRuntime{}, nil, nil, nil)

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
	validate, _ := validation.NewValidator()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.New(storage, validate, &fakeRuntime{}, nil, nil, nil)

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
	desc := "Original"
	storage := &updatePatchDeleteCollectionStorage{
		fakeStorage: &fakeStorage{},
		collection: &api.CollectionResource{
			Resource: api.Resource{ID: "coll-update"},
			CollectionConfig: api.CollectionConfig{
				Name:        "Original",
				Description: &desc,
				Benchmarks:  []api.BenchmarkConfig{{Ref: api.Ref{ID: "b1"}, ProviderID: "p1"}},
			},
		},
	}
	validate, _ := validation.NewValidator()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.New(storage, validate, &fakeRuntime{}, nil, nil, nil)

	body := `{"name":"Updated Name","description":"Updated desc","benchmarks":[{"id":"b1","provider_id":"p1"}]}`
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
}

func TestHandlePatchCollection(t *testing.T) {
	desc := "Original"
	storage := &updatePatchDeleteCollectionStorage{
		fakeStorage: &fakeStorage{},
		collection: &api.CollectionResource{
			Resource: api.Resource{ID: "coll-patch"},
			CollectionConfig: api.CollectionConfig{
				Name:        "Original",
				Description: &desc,
				Benchmarks:  []api.BenchmarkConfig{},
			},
		},
	}
	validate, _ := validation.NewValidator()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.New(storage, validate, &fakeRuntime{}, nil, nil, nil)

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
	validate, _ := validation.NewValidator()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.New(storage, validate, &fakeRuntime{}, nil, nil, nil)

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
