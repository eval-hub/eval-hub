package handlers_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"

	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/handlers"
)

func TestHandleOpenAPI(t *testing.T) {
	h := handlers.New(nil, nil, nil, nil, nil, nil, nil)

	t.Run("GET request returns OpenAPI spec", func(t *testing.T) {
		ctx := createExecutionContext()
		w := httptest.NewRecorder()

		h.HandleOpenAPI(ctx, createMockRequest("GET", "/openapi.yaml"), &MockResponseWrapper{w})

		if w.Code != 200 {
			t.Errorf("Expected status code %d, got %d", 200, w.Code)
		}

		contentType := w.Header().Get("Content-Type")
		if contentType != "application/yaml" && contentType != "application/json" {
			t.Errorf("Expected Content-Type application/yaml or application/json, got %s", contentType)
		}

		if len(w.Body.Bytes()) == 0 {
			t.Error("Response body is empty")
		}

		// Check if response contains OpenAPI keywords
		body := w.Body.String()
		if !strings.Contains(body, "openapi") && !strings.Contains(body, "OpenAPI") {
			t.Error("Response does not appear to be an OpenAPI specification")
		}
	})

	t.Run("JSON content type when Accept header is application/json", func(t *testing.T) {
		ctx := createExecutionContext()
		req := createMockRequest("GET", "/openapi.yaml")
		req.SetHeader("Accept", "application/json")
		w := httptest.NewRecorder()

		h.HandleOpenAPI(ctx, req, &MockResponseWrapper{w})

		contentType := w.Header().Get("Content-Type")
		if contentType != "application/json" {
			t.Errorf("Expected Content-Type application/json, got %s", contentType)
		}
	})
}

func TestHandleOpenAPI_SpecNotFound(t *testing.T) {
	h := handlers.New(nil, nil, nil, nil, nil, nil, nil)

	base := t.TempDir()
	deep := filepath.Join(base, "a", "b", "c", "d")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(base)
	})

	ctx := &executioncontext.ExecutionContext{
		Ctx:    context.Background(),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	w := httptest.NewRecorder()
	h.HandleOpenAPI(ctx, createMockRequest("GET", "/openapi.yaml"), &MockResponseWrapper{w}, deep)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected application/json error body, got Content-Type %q", ct)
	}
}

func TestHandleDocs(t *testing.T) {
	h := handlers.New(nil, nil, nil, nil, nil, nil, nil)

	t.Run("GET request returns HTML documentation", func(t *testing.T) {
		ctx := createExecutionContext()
		w := httptest.NewRecorder()

		h.HandleDocs(ctx, createMockRequest("GET", "/docs"), &MockResponseWrapper{w})

		if w.Code != 200 {
			t.Errorf("Expected status code %d, got %d", 200, w.Code)
		}

		contentType := w.Header().Get("Content-Type")
		if contentType != "text/html; charset=utf-8" {
			t.Errorf("Expected Content-Type text/html; charset=utf-8, got %s", contentType)
		}

		body := w.Body.String()
		if !strings.Contains(body, "swagger-ui") && !strings.Contains(body, "SwaggerUI") {
			t.Error("Response does not appear to be Swagger UI HTML")
		}

		if !strings.Contains(body, "openapi.yaml") {
			t.Error("Response does not reference openapi.yaml")
		}

		for _, key := range []string{"Cache-Control", "Pragma", "Expires"} {
			if w.Header().Get(key) == "" {
				t.Errorf("expected cache-disabling header %q to be set", key)
			}
		}
	})

	t.Run("SWAGGER_VERSION header selects Swagger UI asset version", func(t *testing.T) {
		ctx := createExecutionContext()
		req := createMockRequest("GET", "/docs")
		req.SetHeader("SWAGGER_VERSION", "9.8.7")
		w := httptest.NewRecorder()

		h.HandleDocs(ctx, req, &MockResponseWrapper{w})

		body := w.Body.String()
		if !strings.Contains(body, "swagger-ui-dist@9.8.7") {
			snippet := body
			if len(snippet) > 200 {
				snippet = snippet[:200]
			}
			t.Fatalf("expected custom Swagger UI version in HTML, got body snippet: %s", snippet)
		}
	})

	t.Run("SWAGGER_VERSION header can not insert malicious code", func(t *testing.T) {
		badScript := `<script>alert("Hello, world!");</script>`
		ctx := createExecutionContext()
		req := createMockRequest("GET", "/docs")
		req.SetHeader("SWAGGER_VERSION", badScript)
		w := httptest.NewRecorder()

		h.HandleDocs(ctx, req, &MockResponseWrapper{w})

		body := w.Body.String()
		if !strings.Contains(body, "swagger-ui-dist@&lt;script&gt;") {
			snippet := body
			if len(snippet) > 200 {
				snippet = snippet[:200]
			}
			t.Fatalf("expected custom Swagger UI version in HTML, got body snippet: %s", snippet)
		}
	})

	t.Run("base URL strips request path for spec URL", func(t *testing.T) {
		ctx := createExecutionContext()
		req := &MockRequest{
			TestMethod: "GET",
			TestURI:    "/api/v1/docs",
			TestPath:   "/docs",
			headers:    map[string]string{},
		}
		w := httptest.NewRecorder()

		h.HandleDocs(ctx, req, &MockResponseWrapper{w})

		body := w.Body.String()
		want := `url: "/api/v1/openapi.yaml"`
		if !strings.Contains(body, want) {
			t.Fatalf("expected %q in HTML, got: %s", want, body)
		}
	})
}

// TestOpenAPIGenerationContract checks the public bundle actually served by
// EvalHub. It protects the UI contract before the generation handlers exist.
func TestOpenAPIGenerationContract(t *testing.T) {
	h := handlers.New(nil, nil, nil, nil, nil, nil, nil)
	req := createMockRequest("GET", "/openapi.yaml")
	req.SetHeader("Accept", "application/json")
	w := httptest.NewRecorder()
	h.HandleOpenAPI(createExecutionContext(), req, &MockResponseWrapper{w})
	if w.Code != http.StatusOK {
		t.Fatalf("expected public OpenAPI JSON, got HTTP %d: %s", w.Code, w.Body.String())
	}

	type mediaType struct {
		Schema struct {
			Ref string `json:"$ref"`
		} `json:"schema"`
		Examples map[string]struct {
			Value json.RawMessage `json:"value"`
		} `json:"examples"`
	}
	type response struct {
		Ref     string `json:"$ref"`
		Headers map[string]struct {
			Example string `json:"example"`
		} `json:"headers"`
		Content map[string]mediaType `json:"content"`
	}
	type operation struct {
		RequestBody struct {
			Content map[string]mediaType `json:"content"`
		} `json:"requestBody"`
		Responses map[string]response `json:"responses"`
	}
	var spec struct {
		Paths      map[string]map[string]operation `json:"paths"`
		Components struct {
			Responses map[string]response        `json:"responses"`
			Schemas   map[string]json.RawMessage `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatalf("decode public OpenAPI JSON: %v", err)
	}

	collectionPath := "/api/v1/evaluations/collections/generation"
	itemPath := collectionPath + "/{id}"
	post, ok := spec.Paths[collectionPath]["post"]
	if !ok {
		t.Fatal("missing generation POST in public spec")
	}
	get, ok := spec.Paths[itemPath]["get"]
	if !ok {
		t.Fatal("missing generation GET in public spec")
	}
	deleteOp, ok := spec.Paths[itemPath]["delete"]
	if !ok {
		t.Fatal("missing generation DELETE in public spec")
	}
	if got := post.RequestBody.Content["application/json"].Schema.Ref; got != "#/components/schemas/CollectionGenerationRequest" {
		t.Errorf("POST request schema = %q", got)
	}
	for _, check := range []struct {
		method string
		op     operation
		codes  []string
	}{
		{"POST", post, []string{"202", "400", "401", "403", "503"}},
		{"GET", get, []string{"200", "401", "403", "404"}},
		{"DELETE", deleteOp, []string{"204", "401", "403", "404"}},
	} {
		for _, code := range check.codes {
			if _, ok := check.op.Responses[code]; !ok {
				t.Errorf("%s is missing response %s", check.method, code)
			}
		}
	}
	if len(deleteOp.Responses["204"].Content) != 0 {
		t.Error("DELETE 204 must have no response body")
	}
	if got := post.Responses["503"].Ref; got != "#/components/responses/ServiceUnavailable" {
		t.Errorf("POST 503 response reference = %q", got)
	}
	var unavailable struct {
		MessageCode string `json:"message_code"`
	}
	serviceUnavailable := spec.Components.Responses["ServiceUnavailable"]
	if err := json.Unmarshal(serviceUnavailable.Content["application/json"].Examples["generation_not_configured"].Value, &unavailable); err != nil {
		t.Fatalf("decode 503 example: %v", err)
	}
	if unavailable.MessageCode != "collection_generation_not_configured" {
		t.Errorf("503 example message_code = %q", unavailable.MessageCode)
	}

	type benchmarkReason struct {
		ProviderID string `json:"provider_id"`
		ID         string `json:"id"`
		Reason     string `json:"reason"`
	}
	type generationExample struct {
		Resource struct {
			ID        string `json:"id"`
			Tenant    string `json:"tenant"`
			Owner     string `json:"owner"`
			CreatedAt string `json:"created_at"`
			UpdatedAt string `json:"updated_at"`
		} `json:"resource"`
		Request struct {
			EvaluationGoal string `json:"evaluation_goal"`
			MaxBenchmarks  int    `json:"max_benchmarks"`
			Strictness     string `json:"strictness"`
		} `json:"request"`
		Status struct {
			State string `json:"state"`
			Error *struct {
				Code     string `json:"code"`
				Message  string `json:"message"`
				Category string `json:"category"`
			} `json:"error"`
		} `json:"status"`
		Result *struct {
			Collection  map[string]any `json:"collection"`
			YAML        string         `json:"yaml"`
			Explanation struct {
				Selected []benchmarkReason `json:"selected_benchmarks"`
				Rejected []benchmarkReason `json:"rejected_benchmarks"`
			} `json:"explanation"`
		} `json:"result"`
	}
	pending := post.Responses["202"].Content["application/json"].Examples["pending"].Value
	var accepted generationExample
	if err := json.Unmarshal(pending, &accepted); err != nil {
		t.Fatalf("decode POST 202 example: %v", err)
	}
	if accepted.Status.State != "pending" || accepted.Resource.ID == "" ||
		accepted.Resource.Tenant == "" || accepted.Resource.Owner == "" ||
		accepted.Resource.CreatedAt == "" || accepted.Resource.UpdatedAt == "" {
		t.Fatalf("POST 202 example must be pending with job and tenant metadata: %+v", accepted)
	}
	if want := collectionPath + "/" + accepted.Resource.ID; post.Responses["202"].Headers["Location"].Example != want {
		t.Errorf("Location example does not point at the returned job ID: want %q", want)
	}

	examples := get.Responses["200"].Content["application/json"].Examples
	for name, wantState := range map[string]string{
		"pending": "pending", "running": "running", "completed": "completed",
		"failed_guardrail_rejected": "failed", "failed_guardrail_unavailable": "failed",
		"cancelled": "cancelled",
	} {
		t.Run(name, func(t *testing.T) {
			raw, ok := examples[name]
			if !ok {
				t.Fatalf("GET 200 is missing %s example", name)
			}
			var example generationExample
			if err := json.Unmarshal(raw.Value, &example); err != nil {
				t.Fatal(err)
			}
			if example.Resource.ID != accepted.Resource.ID || example.Request.EvaluationGoal == "" ||
				example.Request.MaxBenchmarks < 1 || example.Request.Strictness == "" {
				t.Error("example lacks the backing job ID or normalized request")
			}
			if example.Status.State != wantState {
				t.Errorf("state = %q, want %q", example.Status.State, wantState)
			}
			if (example.Result != nil) != (wantState == "completed") {
				t.Error("result must appear only for a completed generation")
			}
			if (example.Status.Error != nil) != (wantState == "failed") {
				t.Error("status.error must appear only for a failed generation")
			}
			if wantState == "failed" {
				if example.Status.Error == nil {
					t.Fatal("failed generation must have status.error")
				}
				if example.Status.Error.Code == "" || example.Status.Error.Message == "" {
					t.Error("failed generation must have a safe code and message")
				}
			}
			if name == "failed_guardrail_rejected" &&
				(example.Status.Error.Code != "guardrails_rejected" || example.Status.Error.Category == "") {
				t.Error("guardrail rejection must include its stable code and category")
			}
			if example.Result == nil {
				return
			}
			var yamlCollection map[string]any
			if err := yaml.Unmarshal([]byte(example.Result.YAML), &yamlCollection); err != nil {
				t.Fatalf("decode result YAML: %v", err)
			}
			if !reflect.DeepEqual(yamlCollection, example.Result.Collection) {
				t.Error("result.yaml and result.collection describe different configs")
			}
			benchmarks, ok := example.Result.Collection["benchmarks"].([]any)
			if !ok || len(benchmarks) != len(example.Result.Explanation.Selected) {
				t.Fatal("selected_benchmarks must match the final collection")
			}
			selected := make(map[string]bool)
			for i, item := range benchmarks {
				benchmark, ok := item.(map[string]any)
				if !ok {
					t.Fatalf("benchmark %d is not an object", i)
				}
				reason := example.Result.Explanation.Selected[i]
				if reason.ProviderID != benchmark["provider_id"] || reason.ID != benchmark["id"] ||
					reason.Reason == "" || len(reason.Reason) > 300 {
					t.Errorf("selected reason %d does not match final benchmark", i)
				}
				selected[reason.ProviderID+"/"+reason.ID] = true
			}
			if len(example.Result.Explanation.Rejected) > 5 {
				t.Error("rejected_benchmarks exceeds five entries")
			}
			for _, reason := range example.Result.Explanation.Rejected {
				if reason.Reason == "" || len(reason.Reason) > 300 ||
					selected[reason.ProviderID+"/"+reason.ID] {
					t.Errorf("invalid rejected reason for %s/%s", reason.ProviderID, reason.ID)
				}
			}
		})
	}
	if len(spec.Components.Schemas["CollectionGenerationExplanation"]) == 0 {
		t.Error("public spec is missing the explanation schema")
	}
	if strings.Contains(string(spec.Components.Schemas["CollectionConfig"]), "selected_benchmarks") {
		t.Error("generation-only explanation leaked into CollectionConfig")
	}
}
