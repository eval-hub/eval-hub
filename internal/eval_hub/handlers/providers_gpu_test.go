package handlers_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/constants"
	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/handlers"
	"github.com/eval-hub/eval-hub/internal/eval_hub/validation"
	"github.com/eval-hub/eval-hub/internal/logging"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func gpuTestProvider() api.ProviderResource {
	return api.ProviderResource{
		Resource: api.Resource{ID: "gpu_test_provider"},
		ProviderConfig: api.ProviderConfig{
			Name: "GPU Test Provider",
			Runtime: &api.Runtime{
				K8s: &api.K8sRuntime{
					Image:      "quay.io/example/adapter:latest",
					Entrypoint: []string{"/bin/true"},
					GPU: &api.GPUConfig{
						Resource: "nvidia.com/gpu",
						Count:    1,
						NodeSelector: map[string]string{
							"nvidia.com/gpu.product": "A100-SXM4-40GB",
						},
					},
				},
			},
			Benchmarks: []api.BenchmarkResource{{ID: "arc_easy", Name: "ARC Easy"}},
		},
	}
}

func TestHandleGetProvider_ReturnsGPUConfig(t *testing.T) {
	provider := gpuTestProvider()
	storage := &fakeStorage{providerConfigs: map[string]api.ProviderResource{
		provider.Resource.ID: provider,
	}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.New(storage, validation.NewValidator(), &fakeRuntime{}, nil, nil)

	req := &providersRequest{
		MockRequest: createMockRequest("GET", "/api/v1/evaluations/providers/"+provider.Resource.ID),
		pathValues:  map[string]string{constants.PATH_PARAMETER_PROVIDER_ID: provider.Resource.ID},
	}
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, "test-user", "test-tenant")

	h.HandleGetProvider(ctx, req, resp)

	if recorder.Code != 200 {
		t.Fatalf("expected status 200, got %d body %s", recorder.Code, recorder.Body.String())
	}
	var got api.ProviderResource
	if err := json.NewDecoder(recorder.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	assertProviderGPUConfig(t, got)
}

func TestHandleListProviders_ReturnsGPUConfig(t *testing.T) {
	provider := gpuTestProvider()
	storage := &listProvidersStorage{
		fakeStorage: &fakeStorage{},
		providers:   []api.ProviderResource{provider},
	}
	logger := logging.FallbackLogger()
	h := handlers.New(storage, validation.NewValidator(), &fakeRuntime{}, nil, nil)

	req := &providersRequest{
		MockRequest: createMockRequest("GET", "/api/v1/evaluations/providers"),
		queryValues: map[string][]string{},
		pathValues:  map[string]string{},
	}
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, "test-user", "test-tenant")

	h.HandleListProviders(ctx, req, resp)

	if recorder.Code != 200 {
		t.Fatalf("expected status 200, got %d body %s", recorder.Code, recorder.Body.String())
	}
	var got api.ProviderResourceList
	if err := json.NewDecoder(recorder.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got.Items) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(got.Items))
	}
	assertProviderGPUConfig(t, got.Items[0])
}

func TestHandleUpdateProvider_PreservesGPUConfig(t *testing.T) {
	providerID := "gpu-user-provider"
	existing := gpuTestProvider()
	existing.Resource.ID = providerID
	storage := &updatePatchProviderStorage{
		fakeStorage: &fakeStorage{},
		provider:    &existing,
	}

	logger := logging.FallbackLogger()
	h := handlers.New(storage, validation.NewValidator(), &fakeRuntime{}, nil, nil)

	body := `{
		"name":"GPU Provider Updated",
		"description":"updated",
		"runtime":{
			"k8s":{
				"image":"quay.io/example/adapter:v2",
				"entrypoint":["/bin/true"],
				"gpu":{
					"resource":"nvidia.com/gpu",
					"count":2,
					"node_selector":{"nvidia.com/gpu.product":"NVIDIA-H100-80GB-HBM3"}
				}
			}
		},
		"benchmarks":[{"id":"arc_easy","name":"ARC Easy","category":"reasoning"}]
	}`
	req := &providersRequest{
		MockRequest: createMockRequest("PUT", "/api/v1/evaluations/providers/"+providerID),
		pathValues:  map[string]string{constants.PATH_PARAMETER_PROVIDER_ID: providerID},
	}
	req.SetBody([]byte(body))
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, "test-user", "test-tenant")

	h.HandleUpdateProvider(ctx, req, resp)

	if recorder.Code != 200 {
		t.Fatalf("expected status 200, got %d body %s", recorder.Code, recorder.Body.String())
	}
	var got api.ProviderResource
	if err := json.NewDecoder(recorder.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Name != "GPU Provider Updated" {
		t.Errorf("name = %q, want GPU Provider Updated", got.Name)
	}
	if got.Runtime.K8s.GPU.Count != 2 {
		t.Errorf("gpu count = %d, want 2", got.Runtime.K8s.GPU.Count)
	}
	if got.Runtime.K8s.GPU.NodeSelector["nvidia.com/gpu.product"] != "NVIDIA-H100-80GB-HBM3" {
		t.Errorf("node_selector = %v", got.Runtime.K8s.GPU.NodeSelector)
	}
}

func TestHandleCreateProvider_RejectsInvalidImagePullPolicy(t *testing.T) {
	storage := &fakeStorage{}
	logger := logging.FallbackLogger()
	h := handlers.New(storage, validation.NewValidator(), &fakeRuntime{}, nil, nil)

	body := `{
		"name":"My Provider",
		"description":"A test provider",
		"runtime":{
			"k8s":{
				"image":"quay.io/example/adapter:latest",
				"entrypoint":["/bin/true"],
				"image_pull_policy":"random"
			}
		},
		"benchmarks":[{"id":"bench-1","name":"Bench 1"}]
	}`
	req := &providersRequest{
		MockRequest: createMockRequest("POST", "/api/v1/evaluations/providers"),
		queryValues: map[string][]string{},
		pathValues:  map[string]string{},
	}
	req.SetBody([]byte(body))
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, "test-user", "test-tenant")

	h.HandleCreateProvider(ctx, req, resp)

	if recorder.Code != 400 {
		t.Fatalf("expected status 400, got %d body %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "if_not_present") || !strings.Contains(recorder.Body.String(), "always") {
		t.Fatalf("expected allowed values in error response, got: %s", recorder.Body.String())
	}
}

func TestHandlePatchProvider_RejectsInvalidImagePullPolicy(t *testing.T) {
	providerID := "my-provider"
	storage := &updatePatchProviderStorage{
		fakeStorage: &fakeStorage{},
		provider: &api.ProviderResource{
			Resource: api.Resource{ID: providerID},
			ProviderConfig: api.ProviderConfig{
				Name: "My Provider",
				Runtime: &api.Runtime{
					K8s: &api.K8sRuntime{
						Image:      "quay.io/example/adapter:latest",
						Entrypoint: []string{"/bin/true"},
					},
				},
				Benchmarks: []api.BenchmarkResource{{ID: "bench-1", Name: "Bench 1"}},
			},
		},
	}
	logger := logging.FallbackLogger()
	h := handlers.New(storage, validation.NewValidator(), &fakeRuntime{}, nil, nil)

	body := `[{
		"op":"replace",
		"path":"/runtime",
		"value":{
			"k8s":{
				"image":"quay.io/example/adapter:latest",
				"entrypoint":["/bin/true"],
				"image_pull_policy":"random"
			}
		}
	}]`
	req := &providersRequest{
		MockRequest: createMockRequest("PATCH", "/api/v1/evaluations/providers/"+providerID),
		pathValues:  map[string]string{constants.PATH_PARAMETER_PROVIDER_ID: providerID},
	}
	req.SetBody([]byte(body))
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, "test-user", "test-tenant")

	h.HandlePatchProvider(ctx, req, resp)

	if recorder.Code != 400 {
		t.Fatalf("expected status 400, got %d body %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "if_not_present") || !strings.Contains(recorder.Body.String(), "always") {
		t.Fatalf("expected allowed values in error response, got: %s", recorder.Body.String())
	}
}

func assertProviderGPUConfig(t *testing.T, got api.ProviderResource) {
	t.Helper()
	if got.Runtime == nil || got.Runtime.K8s == nil || got.Runtime.K8s.GPU == nil {
		t.Fatal("expected runtime.k8s.gpu in response")
	}
	if got.Runtime.K8s.GPU.Resource != "nvidia.com/gpu" {
		t.Errorf("gpu resource = %q, want nvidia.com/gpu", got.Runtime.K8s.GPU.Resource)
	}
	if got.Runtime.K8s.GPU.Count != 1 {
		t.Errorf("gpu count = %d, want 1", got.Runtime.K8s.GPU.Count)
	}
	if got.Runtime.K8s.GPU.NodeSelector["nvidia.com/gpu.product"] != "A100-SXM4-40GB" {
		t.Errorf("node_selector = %v", got.Runtime.K8s.GPU.NodeSelector)
	}
}
