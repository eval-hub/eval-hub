package handlers_test

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/eval-hub/eval-hub/internal/handlers"
	"github.com/eval-hub/eval-hub/pkg/api"
)

type providersRequest struct {
	*MockRequest
	queryValues map[string][]string
}

func (r *providersRequest) Query(key string) []string {
	if values, ok := r.queryValues[key]; ok {
		return values
	}
	return []string{}
}

func TestHandleListProvidersReturnsEmptyForInvalidProviderID(t *testing.T) {
	providerConfigs := map[string]api.ProviderResource{
		"garak": {
			ID: "garak",
			Benchmarks: []api.BenchmarkResource{
				{ID: "bench-1"},
			},
		},
	}
	h := handlers.New(nil, nil, nil, nil, providerConfigs, nil)

	req := &providersRequest{
		MockRequest: createMockRequest("GET", "/api/v1/evaluations/providers?id=unknown"),
		queryValues: map[string][]string{"id": {"unknown"}},
	}
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}
	ctx := createExecutionContext()

	h.HandleListProviders(ctx, req, resp)

	if recorder.Code != 200 {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}

	var body api.ProviderResourceList
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if body.TotalCount != 0 {
		t.Fatalf("expected total_count 0, got %d", body.TotalCount)
	}
}
