package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

const collectionGenerationMockID = "550e8400-e29b-41d4-a716-446655440099"

func TestCollectionGenerationMockMatchesOpenAPIContract(t *testing.T) {
	srv, err := createServer(t, 8080)
	if err != nil {
		t.Fatalf("createServer: %v", err)
	}
	handler, err := srv.SetupRoutes()
	if err != nil {
		t.Fatalf("SetupRoutes: %v", err)
	}

	t.Run("removed models endpoint is not exposed", func(t *testing.T) {
		response := serveGenerationRequest(handler, http.MethodGet, "/api/v1/evaluations/collections/generation/models?mock=true", "", "")
		if response.Code != http.StatusNotFound {
			t.Fatalf("models path status = %d, want 404", response.Code)
		}
	})

	t.Run("unsupported collection route methods return structured errors", func(t *testing.T) {
		for _, test := range []struct {
			method string
			path   string
		}{
			{method: http.MethodGet, path: "/api/v1/evaluations/collections/generation"},
			{method: http.MethodPut, path: "/api/v1/evaluations/collections/generation/" + collectionGenerationMockID},
			{method: http.MethodGet, path: "/api/v1/evaluations/collections/custom-collection/clones"},
		} {
			response := serveGenerationRequest(handler, test.method, test.path, "", "")
			if response.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s status = %d, want 405; body = %s", test.method, test.path, response.Code, response.Body.String())
			}
			assertMessageCode(t, response, "method_not_allowed")
		}
	})

	t.Run("post applies normalized OpenAPI request defaults", func(t *testing.T) {
		withoutMock := serveGenerationRequest(handler, http.MethodPost, "/api/v1/evaluations/collections/generation", `{"evaluation_goal":"goal"}`, "application/json")
		assertMessageCode(t, withoutMock, "collection_generation_not_configured")

		badBody := serveGenerationRequest(handler, http.MethodPost, "/api/v1/evaluations/collections/generation?mock=true", `{"evaluation_goal":"goal","model":{"name":"generator","url":"https://generator.example/v1"},"unknown":true}`, "application/json")
		assertMessageCode(t, badBody, "request_validation_failed")

		response := serveGenerationRequest(handler, http.MethodPost, "/api/v1/evaluations/collections/generation?mock=true", `{"evaluation_goal":"  assess safety  ","model":{"name":"chosen-generator","url":"https://generator.example/v1","auth":{"secret_ref":"generator-credentials"}},"provider_filter":" inspect ","generation_effort":"high"}`, "application/json")
		if response.Code != http.StatusAccepted {
			t.Fatalf("POST status = %d, body = %s", response.Code, response.Body.String())
		}
		assertMockHeaders(t, response)
		if got, want := response.Header().Get("Location"), "/api/v1/evaluations/collections/generation/"+collectionGenerationMockID+"?mock=true"; got != want {
			t.Fatalf("Location = %q, want %q", got, want)
		}
		var body map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode POST response: %v", err)
		}
		resource := body["resource"].(map[string]any)
		if resource["id"] != collectionGenerationMockID || resource["tenant"] != "local" || resource["owner"] != "local" {
			t.Fatalf("POST resource does not match the contract: %#v", resource)
		}
		request := body["request"].(map[string]any)
		model := request["model"].(map[string]any)
		if request["evaluation_goal"] != "assess safety" || request["provider_filter"] != "inspect" || request["max_benchmarks"] != float64(12) || request["strictness"] != "moderate" || model["name"] != "chosen-generator" || model["url"] != "https://generator.example/v1" || model["auth"].(map[string]any)["secret_ref"] != "generator-credentials" || request["generation_effort"] != "high" {
			t.Fatalf("POST normalized request does not match the OpenAPI summary: %#v", request)
		}
		if body["status"].(map[string]any)["state"] != "pending" {
			t.Fatalf("POST response should be pending: %#v", body["status"])
		}
		if _, exists := body["result"]; exists {
			t.Fatal("pending resource must not include result")
		}

		defaults := serveGenerationRequest(handler, http.MethodPost, "/api/v1/evaluations/collections/generation?mock=true", `{"evaluation_goal":"minimal goal","model":{"name":"other-generator","url":"http://other.example/v1"}}`, "application/json")
		if defaults.Code != http.StatusAccepted {
			t.Fatalf("default POST status = %d, body = %s", defaults.Code, defaults.Body.String())
		}
		var defaultsBody map[string]any
		if err := json.Unmarshal(defaults.Body.Bytes(), &defaultsBody); err != nil {
			t.Fatalf("decode default POST response: %v", err)
		}
		defaultRequest := defaultsBody["request"].(map[string]any)
		if defaultRequest["max_benchmarks"] != float64(12) || defaultRequest["strictness"] != "moderate" || defaultRequest["model"].(map[string]any)["name"] != "other-generator" || defaultRequest["generation_effort"] != "standard" {
			t.Fatalf("defaulted request does not match the OpenAPI defaults: %#v", defaultRequest)
		}
	})

	t.Run("poll result and delete match resource schemas", func(t *testing.T) {
		path := "/api/v1/evaluations/collections/generation/" + collectionGenerationMockID
		withoutMock := serveGenerationRequest(handler, http.MethodGet, path, "", "")
		assertMessageCode(t, withoutMock, "collection_generation_not_configured")

		unknown := serveGenerationRequest(handler, http.MethodGet, "/api/v1/evaluations/collections/generation/unknown?mock=true", "", "")
		if unknown.Code != http.StatusNotFound {
			t.Fatalf("unknown generation GET status = %d, body = %s", unknown.Code, unknown.Body.String())
		}

		response := serveGenerationRequest(handler, http.MethodGet, path+"?mock=true", "", "")
		if response.Code != http.StatusOK {
			t.Fatalf("GET status = %d, body = %s", response.Code, response.Body.String())
		}
		assertMockHeaders(t, response)
		var body struct {
			Resource map[string]any `json:"resource"`
			Request  map[string]any `json:"request"`
			Status   map[string]any `json:"status"`
			Result   struct {
				Collection  map[string]any `json:"collection"`
				YAML        string         `json:"yaml"`
				Rationale   string         `json:"rationale"`
				Explanation struct {
					Selected []map[string]any `json:"selected_benchmarks"`
					Rejected []map[string]any `json:"rejected_benchmarks"`
				} `json:"explanation"`
				Actions []map[string]any `json:"actions"`
			} `json:"result"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode GET response: %v", err)
		}
		if body.Resource["id"] != collectionGenerationMockID || body.Resource["tenant"] != "local" || body.Resource["owner"] != "local" {
			t.Fatalf("GET resource does not match Resource schema: %#v", body.Resource)
		}
		if body.Status["state"] != "completed" || body.Status["started_at"] == nil || body.Status["completed_at"] == nil || body.Status["progress"] != nil || body.Status["error"] != nil {
			t.Fatalf("GET status does not match completed status schema: %#v", body.Status)
		}
		if body.Request["evaluation_goal"] == nil || body.Request["max_benchmarks"] == nil || body.Request["strictness"] == nil || body.Request["model"] == nil || body.Request["generation_effort"] == nil {
			t.Fatalf("GET request omits normalized required fields: %#v", body.Request)
		}
		if body.Result.Rationale == "" || len(body.Result.Explanation.Selected) == 0 || len(body.Result.Actions) == 0 {
			t.Fatalf("GET result omits required fields: %+v", body.Result)
		}
		var yamlCollection map[string]any
		if err := yaml.Unmarshal([]byte(body.Result.YAML), &yamlCollection); err != nil {
			t.Fatalf("parse result.yaml: %v", err)
		}
		if !reflect.DeepEqual(yamlCollection, body.Result.Collection) {
			t.Fatalf("result.yaml does not serialize result.collection:\nyaml: %#v\njson: %#v", yamlCollection, body.Result.Collection)
		}
		benchmarks := body.Result.Collection["benchmarks"].([]any)
		if len(body.Result.Explanation.Selected) != len(benchmarks) {
			t.Fatalf("selected explanation count = %d, collection benchmark count = %d", len(body.Result.Explanation.Selected), len(benchmarks))
		}
		for i, selected := range body.Result.Explanation.Selected {
			benchmark := benchmarks[i].(map[string]any)
			if selected["id"] != benchmark["id"] || selected["provider_id"] != benchmark["provider_id"] || selected["reason"] == "" {
				t.Fatalf("selected explanation %d does not describe collection benchmark: %#v vs %#v", i, selected, benchmark)
			}
		}

		deleteNoMock := serveGenerationRequest(handler, http.MethodDelete, path, "", "")
		assertMessageCode(t, deleteNoMock, "collection_generation_not_configured")
		invalidDelete := serveGenerationRequest(handler, http.MethodDelete, path+"?mock=false", "", "")
		assertMessageCode(t, invalidDelete, "query_parameter_value_invalid")
		deleted := serveGenerationRequest(handler, http.MethodDelete, path+"?mock=true", "", "")
		if deleted.Code != http.StatusNoContent || deleted.Body.Len() != 0 {
			t.Fatalf("DELETE = status %d body %q, want 204 with empty body", deleted.Code, deleted.Body.String())
		}
		assertMockHeaders(t, deleted)
		if afterDelete := serveGenerationRequest(handler, http.MethodGet, path+"?mock=true", "", ""); afterDelete.Code != http.StatusOK {
			t.Fatalf("mock DELETE should be a no-op; subsequent GET status = %d", afterDelete.Code)
		}
	})
}

func serveGenerationRequest(handler http.Handler, method, path, body, contentType string) *httptest.ResponseRecorder {
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	request := httptest.NewRequest(method, path, reader)
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func assertMockHeaders(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if got := response.Header().Get("X-EvalHub-Mock"); got != "true" {
		t.Errorf("X-EvalHub-Mock = %q, want true", got)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}
