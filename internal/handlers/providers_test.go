package handlers_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/constants"
	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"github.com/eval-hub/eval-hub/internal/handlers"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/go-playground/validator/v10"
)

type providersRequest struct {
	*MockRequest
	queryValues map[string][]string
	pathValues  map[string]string
}

func (r *providersRequest) PathValue(name string) string {
	return r.pathValues[name]
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
func (f *fakeStorage) GetProvider(_ string) (*api.ProviderResource, error) {
	return nil, fmt.Errorf("provider not found")
}
func (f *fakeStorage) DeleteProvider(_ string) error {
	return nil
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

	fmt.Println(recorder.Body.String())
	if recorder.Code != 404 {
		t.Fatalf("expected status 404, got %d", recorder.Code)
	}

}
