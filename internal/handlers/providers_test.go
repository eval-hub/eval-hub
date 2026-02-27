package handlers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/constants"
	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"github.com/eval-hub/eval-hub/internal/handlers"
	"github.com/eval-hub/eval-hub/internal/messages"
	"github.com/eval-hub/eval-hub/internal/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/go-playground/validator/v10"
)

type providersRequest struct {
	*MockRequest
	queryValues map[string][]string
	pathValues  map[string]string
	body        []byte
}

func (r *providersRequest) PathValue(name string) string {
	return r.pathValues[name]
}

func (r *providersRequest) BodyAsBytes() ([]byte, error) {
	if r.body != nil {
		return r.body, nil
	}
	return r.MockRequest.BodyAsBytes()
}

func (r *providersRequest) SetBody(b []byte) {
	r.body = b
}

func (r *providersRequest) Query(key string) []string {
	if values, ok := r.queryValues[key]; ok {
		return values
	}
	return []string{}
}

func (f *fakeStorage) CreateProvider(_ *api.ProviderResource) error {
	return nil
}
func (f *fakeStorage) GetProvider(id string) (*api.ProviderResource, error) {
	return nil, serviceerrors.NewServiceError(messages.ResourceNotFound, "Type", "provider", "ResourceId", id)
}
func (f *fakeStorage) DeleteProvider(_ string) error {
	return nil
}
func (f *fakeStorage) UpdateProvider(_ string, provider *api.ProviderResource) (*api.ProviderResource, error) {
	return nil, fmt.Errorf("not implemented")
}
func (f *fakeStorage) PatchProvider(_ string, _ *api.Patch) (*api.ProviderResource, error) {
	return nil, fmt.Errorf("not implemented")
}
func (f *fakeStorage) GetProviders(_ *abstractions.QueryFilter) (*abstractions.QueryResults[api.ProviderResource], error) {
	return &abstractions.QueryResults[api.ProviderResource]{Items: []api.ProviderResource{}, TotalStored: 0}, nil
}

type updatePatchProviderStorage struct {
	*fakeStorage
	provider *api.ProviderResource
}

func (s *updatePatchProviderStorage) WithLogger(_ *slog.Logger) abstractions.Storage { return s }
func (s *updatePatchProviderStorage) WithContext(_ context.Context) abstractions.Storage {
	return s
}
func (s *updatePatchProviderStorage) WithTenant(_ api.Tenant) abstractions.Storage { return s }

func (s *updatePatchProviderStorage) GetProvider(id string) (*api.ProviderResource, error) {
	if s.provider != nil && s.provider.Resource.ID == id {
		return s.provider, nil
	}
	return nil, fmt.Errorf("provider not found")
}
func (s *updatePatchProviderStorage) UpdateProvider(id string, provider *api.ProviderResource) (*api.ProviderResource, error) {
	updated := &api.ProviderResource{
		Resource:       s.provider.Resource,
		ProviderConfig: provider.ProviderConfig,
	}
	s.provider = updated
	return updated, nil
}
func (s *updatePatchProviderStorage) PatchProvider(id string, patches *api.Patch) (*api.ProviderResource, error) {
	// Simple patch: for replace on /name or /description
	if s.provider == nil {
		return nil, fmt.Errorf("provider not found")
	}
	patched := *s.provider
	for _, p := range *patches {
		if p.Op == api.PatchOpReplace {
			switch p.Path {
			case "/name":
				if v, ok := p.Value.(string); ok {
					patched.Name = v
				}
			case "/description":
				if v, ok := p.Value.(string); ok {
					patched.Description = v
				}
			}
		}
	}
	s.provider = &patched
	return &patched, nil
}

type listProvidersStorage struct {
	*fakeStorage
	providers []api.ProviderResource
	err       error
}

func (s *listProvidersStorage) WithLogger(_ *slog.Logger) abstractions.Storage { return s }
func (s *listProvidersStorage) WithContext(_ context.Context) abstractions.Storage {
	return s
}
func (s *listProvidersStorage) WithTenant(_ api.Tenant) abstractions.Storage { return s }

func (s *listProvidersStorage) GetProviders(_ *abstractions.QueryFilter) (*abstractions.QueryResults[api.ProviderResource], error) {
	if s.err != nil {
		return nil, s.err
	}
	return &abstractions.QueryResults[api.ProviderResource]{
		Items:       s.providers,
		TotalStored: len(s.providers),
	}, nil
}

func TestHandleListProviders_ReturnsSystemProviders(t *testing.T) {
	providerConfigs := map[string]api.ProviderResource{
		"lm_evaluation_harness": {
			Resource: api.Resource{ID: "lm_evaluation_harness"},
			ProviderConfig: api.ProviderConfig{
				Name:        "LM Eval Harness",
				Description: "System provider",
				Benchmarks: []api.BenchmarkResource{
					{ID: "arc_easy"},
				},
			},
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.New(&fakeStorage{}, validator.New(), &fakeRuntime{}, nil, providerConfigs, nil)

	req := &providersRequest{
		MockRequest: createMockRequest("GET", "/api/v1/evaluations/providers"),
		queryValues: map[string][]string{},
		pathValues:  map[string]string{},
	}
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, time.Second, "test-user", "test-tenant")

	h.HandleListProviders(ctx, req, resp)

	if recorder.Code != 200 {
		t.Fatalf("expected status 200, got %d body %s", recorder.Code, recorder.Body.String())
	}
	var got api.ProviderResourceList
	if err := json.NewDecoder(recorder.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.TotalCount != 1 {
		t.Errorf("expected TotalCount 1, got %d", got.TotalCount)
	}
	if len(got.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(got.Items))
	}
	if got.Items[0].Resource.ID != "lm_evaluation_harness" {
		t.Errorf("expected provider id lm_evaluation_harness, got %s", got.Items[0].Resource.ID)
	}
	if got.Items[0].Name != "LM Eval Harness" {
		t.Errorf("expected name LM Eval Harness, got %s", got.Items[0].Name)
	}
}

func TestHandleListProviders_ExcludesSystemProvidersWhenParamFalse(t *testing.T) {
	providerConfigs := map[string]api.ProviderResource{
		"lm_evaluation_harness": {
			Resource:       api.Resource{ID: "lm_evaluation_harness"},
			ProviderConfig: api.ProviderConfig{Name: "System"},
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.New(&fakeStorage{}, validator.New(), &fakeRuntime{}, nil, providerConfigs, nil)

	req := &providersRequest{
		MockRequest: createMockRequest("GET", "/api/v1/evaluations/providers"),
		queryValues: map[string][]string{"system_defined": {"false"}},
		pathValues:  map[string]string{},
	}
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, time.Second, "test-user", "test-tenant")

	h.HandleListProviders(ctx, req, resp)

	if recorder.Code != 200 {
		t.Fatalf("expected status 200, got %d body %s", recorder.Code, recorder.Body.String())
	}
	var got api.ProviderResourceList
	if err := json.NewDecoder(recorder.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.TotalCount != 0 {
		t.Errorf("expected TotalCount 0 when system_defined=false, got %d", got.TotalCount)
	}
	if len(got.Items) != 0 {
		t.Errorf("expected 0 items when system_defined=false, got %d", len(got.Items))
	}
}

func TestHandleListProviders_ExcludesBenchmarksWhenParamFalse(t *testing.T) {
	providerConfigs := map[string]api.ProviderResource{
		"lm_evaluation_harness": {
			Resource: api.Resource{ID: "lm_evaluation_harness"},
			ProviderConfig: api.ProviderConfig{
				Name:       "LM Eval",
				Benchmarks: []api.BenchmarkResource{{ID: "arc_easy"}},
			},
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.New(&fakeStorage{}, validator.New(), &fakeRuntime{}, nil, providerConfigs, nil)

	req := &providersRequest{
		MockRequest: createMockRequest("GET", "/api/v1/evaluations/providers"),
		queryValues: map[string][]string{"benchmarks": {"false"}},
		pathValues:  map[string]string{},
	}
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, time.Second, "test-user", "test-tenant")

	h.HandleListProviders(ctx, req, resp)

	if recorder.Code != 200 {
		t.Fatalf("expected status 200, got %d body %s", recorder.Code, recorder.Body.String())
	}
	var got api.ProviderResourceList
	if err := json.NewDecoder(recorder.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(got.Items))
	}
	if len(got.Items[0].Benchmarks) != 0 {
		t.Errorf("expected empty benchmarks when benchmarks=false, got %d", len(got.Items[0].Benchmarks))
	}
}

func TestHandleListProviders_ReturnsUserProvidersFromStorage(t *testing.T) {
	userProvider := api.ProviderResource{
		Resource:       api.Resource{ID: "user-provider-1"},
		ProviderConfig: api.ProviderConfig{Name: "User Provider", Description: "Created by user"},
	}
	storage := &listProvidersStorage{
		fakeStorage: &fakeStorage{},
		providers:   []api.ProviderResource{userProvider},
	}
	providerConfigs := map[string]api.ProviderResource{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.New(storage, validator.New(), &fakeRuntime{}, nil, providerConfigs, nil)

	req := &providersRequest{
		MockRequest: createMockRequest("GET", "/api/v1/evaluations/providers"),
		queryValues: map[string][]string{},
		pathValues:  map[string]string{},
	}
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, time.Second, "test-user", "test-tenant")

	h.HandleListProviders(ctx, req, resp)

	if recorder.Code != 200 {
		t.Fatalf("expected status 200, got %d body %s", recorder.Code, recorder.Body.String())
	}
	var got api.ProviderResourceList
	if err := json.NewDecoder(recorder.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.TotalCount != 1 {
		t.Errorf("expected TotalCount 1, got %d", got.TotalCount)
	}
	if len(got.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(got.Items))
	}
	if got.Items[0].Resource.ID != "user-provider-1" {
		t.Errorf("expected provider id user-provider-1, got %s", got.Items[0].Resource.ID)
	}
	if got.Items[0].Name != "User Provider" {
		t.Errorf("expected name User Provider, got %s", got.Items[0].Name)
	}
}

func TestHandleListProviders_ReturnsErrorWhenStorageFails(t *testing.T) {
	storage := &listProvidersStorage{
		fakeStorage: &fakeStorage{},
		err:         fmt.Errorf("storage unavailable"),
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.New(storage, validator.New(), &fakeRuntime{}, nil, map[string]api.ProviderResource{}, nil)

	req := &providersRequest{
		MockRequest: createMockRequest("GET", "/api/v1/evaluations/providers"),
		queryValues: map[string][]string{},
		pathValues:  map[string]string{},
	}
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, time.Second, "test-user", "test-tenant")

	h.HandleListProviders(ctx, req, resp)

	if recorder.Code == 200 {
		t.Fatalf("expected error status when storage fails, got 200")
	}
}

func TestHandleListProviders_Returns400WhenInvalidLimit(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.New(&fakeStorage{}, validator.New(), &fakeRuntime{}, nil, map[string]api.ProviderResource{}, nil)

	req := &providersRequest{
		MockRequest: createMockRequest("GET", "/api/v1/evaluations/providers"),
		queryValues: map[string][]string{"limit": {"-1"}},
		pathValues:  map[string]string{},
	}
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, time.Second, "test-user", "test-tenant")

	h.HandleListProviders(ctx, req, resp)

	if recorder.Code != 400 {
		t.Fatalf("expected status 400 for invalid limit, got %d body %s", recorder.Code, recorder.Body.String())
	}
}

func TestHandleListProvidersReturnsEmptyForInvalidProviderID(t *testing.T) {
	providerConfigs := map[string]api.ProviderResource{
		"garak": {
			Resource: api.Resource{ID: "garak"},
			ProviderConfig: api.ProviderConfig{
				Benchmarks: []api.BenchmarkResource{
					{ID: "bench-1"},
				},
			},
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.New(&fakeStorage{}, validator.New(), &fakeRuntime{}, nil, providerConfigs, nil)

	req := &providersRequest{
		MockRequest: createMockRequest("GET", "/api/v1/evaluations/providers/unknown"),
		pathValues:  map[string]string{constants.PATH_PARAMETER_PROVIDER_ID: "unknown"},
	}
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, time.Second, "test-user", "test-tenant")

	h.HandleGetProvider(ctx, req, resp)

	if recorder.Code != 404 {
		t.Fatalf("expected status 404, got %d", recorder.Code)
	}
}

func TestHandleUpdateProvider(t *testing.T) {
	providerID := "user-provider-1"
	base := &fakeStorage{}
	base.job = &api.EvaluationJobResource{} // satisfy other methods
	storage := &updatePatchProviderStorage{
		fakeStorage: base,
		provider: &api.ProviderResource{
			Resource: api.Resource{ID: providerID},
			ProviderConfig: api.ProviderConfig{
				Name:        "Original",
				Description: "Original desc",
			},
		},
	}
	// providerConfigs empty so getSystemProvider returns nil
	providerConfigs := map[string]api.ProviderResource{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.New(storage, validator.New(), &fakeRuntime{}, nil, providerConfigs, nil)

	body := `{"name":"Updated Name","description":"Updated desc","benchmarks":[]}`
	req := &providersRequest{
		MockRequest: createMockRequest("PUT", "/api/v1/evaluations/providers/"+providerID),
		pathValues:  map[string]string{constants.PATH_PARAMETER_PROVIDER_ID: providerID},
	}
	req.SetBody([]byte(body))
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, time.Second, "test-user", "test-tenant")

	h.HandleUpdateProvider(ctx, req, resp)

	if recorder.Code != 200 {
		t.Fatalf("expected status 200, got %d body %s", recorder.Code, recorder.Body.String())
	}
	// Decode response and verify
	var got api.ProviderResource
	if err := json.NewDecoder(recorder.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Name != "Updated Name" {
		t.Errorf("expected Name Updated Name, got %s", got.Name)
	}
	if got.Description != "Updated desc" {
		t.Errorf("expected Description Updated desc, got %s", got.Description)
	}
}

func TestHandleUpdateProviderRejectsSystemProvider(t *testing.T) {
	providerConfigs := map[string]api.ProviderResource{
		"lm_evaluation_harness": {
			Resource:       api.Resource{ID: "lm_evaluation_harness"},
			ProviderConfig: api.ProviderConfig{Name: "System"},
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.New(&fakeStorage{}, validator.New(), &fakeRuntime{}, nil, providerConfigs, nil)

	body := `{"name":"Hacked","description":"","benchmarks":[]}`
	req := &providersRequest{
		MockRequest: createMockRequest("PUT", "/api/v1/evaluations/providers/lm_evaluation_harness"),
		pathValues:  map[string]string{constants.PATH_PARAMETER_PROVIDER_ID: "lm_evaluation_harness"},
	}
	req.SetBody([]byte(body))
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, time.Second, "test-user", "test-tenant")

	h.HandleUpdateProvider(ctx, req, resp)

	if recorder.Code == 200 {
		t.Fatal("expected error when updating system provider")
	}
}

func TestHandlePatchProvider(t *testing.T) {
	providerID := "user-provider-2"
	base := &fakeStorage{}
	base.job = &api.EvaluationJobResource{}
	storage := &updatePatchProviderStorage{
		fakeStorage: base,
		provider: &api.ProviderResource{
			Resource: api.Resource{ID: providerID},
			ProviderConfig: api.ProviderConfig{
				Name:        "Original",
				Description: "Original desc",
			},
		},
	}
	providerConfigs := map[string]api.ProviderResource{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.New(storage, validator.New(), &fakeRuntime{}, nil, providerConfigs, nil)

	body := `[{"op":"replace","path":"/description","value":"Patched description"}]`
	req := &providersRequest{
		MockRequest: createMockRequest("PATCH", "/api/v1/evaluations/providers/"+providerID),
		pathValues:  map[string]string{constants.PATH_PARAMETER_PROVIDER_ID: providerID},
	}
	req.SetBody([]byte(body))
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, time.Second, "test-user", "test-tenant")

	h.HandlePatchProvider(ctx, req, resp)

	if recorder.Code != 200 {
		t.Fatalf("expected status 200, got %d body %s", recorder.Code, recorder.Body.String())
	}
	var got api.ProviderResource
	if err := json.NewDecoder(recorder.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Description != "Patched description" {
		t.Errorf("expected Description Patched description, got %s", got.Description)
	}
	if got.Name != "Original" {
		t.Errorf("expected Name unchanged Original, got %s", got.Name)
	}
}

func TestHandlePatchProviderRejectsImmutablePaths(t *testing.T) {
	providerID := "user-provider-immutable"
	storage := &updatePatchProviderStorage{
		fakeStorage: &fakeStorage{},
		provider: &api.ProviderResource{
			Resource:       api.Resource{ID: providerID},
			ProviderConfig: api.ProviderConfig{Name: "Original", Description: "Desc"},
		},
	}
	providerConfigs := map[string]api.ProviderResource{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.New(storage, validator.New(), &fakeRuntime{}, nil, providerConfigs, nil)

	immutablePaths := []string{"/resource", "/resource/id", "/resource/tenant", "/created_at", "/updated_at"}
	for _, path := range immutablePaths {
		body := fmt.Sprintf(`[{"op":"replace","path":"%s","value":"hacked"}]`, path)
		req := &providersRequest{
			MockRequest: createMockRequest("PATCH", "/api/v1/evaluations/providers/"+providerID),
			pathValues:  map[string]string{constants.PATH_PARAMETER_PROVIDER_ID: providerID},
		}
		req.SetBody([]byte(body))
		recorder := httptest.NewRecorder()
		resp := MockResponseWrapper{recorder: recorder}
		ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, time.Second, "test-user", "test-tenant")
		h.HandlePatchProvider(ctx, req, resp)
		if recorder.Code != 400 {
			t.Errorf("path %q: expected 400, got %d body %s", path, recorder.Code, recorder.Body.String())
		}
		if !strings.Contains(recorder.Body.String(), "immutable") {
			t.Errorf("path %q: expected response to mention immutable, got %s", path, recorder.Body.String())
		}
	}
}

func TestHandlePatchProviderRejectsSystemProvider(t *testing.T) {
	providerConfigs := map[string]api.ProviderResource{
		"lm_evaluation_harness": {
			Resource:       api.Resource{ID: "lm_evaluation_harness"},
			ProviderConfig: api.ProviderConfig{Name: "System"},
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.New(&fakeStorage{}, validator.New(), &fakeRuntime{}, nil, providerConfigs, nil)

	body := `[{"op":"replace","path":"/name","value":"Hacked"}]`
	req := &providersRequest{
		MockRequest: createMockRequest("PATCH", "/api/v1/evaluations/providers/lm_evaluation_harness"),
		pathValues:  map[string]string{constants.PATH_PARAMETER_PROVIDER_ID: "lm_evaluation_harness"},
	}
	req.SetBody([]byte(body))
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, time.Second, "test-user", "test-tenant")

	h.HandlePatchProvider(ctx, req, resp)

	if recorder.Code == 200 {
		t.Fatal("expected error when patching system provider")
	}
}
