package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/handlers"
	"github.com/eval-hub/eval-hub/pkg/api"
)

type generationMockRequest struct {
	method    string
	uri       string
	path      string
	query     url.Values
	body      []byte
	bodyErr   error
	pathValue map[string]string
	headers   map[string]string
}

func (r generationMockRequest) Method() string            { return r.method }
func (r generationMockRequest) URI() string               { return r.uri }
func (r generationMockRequest) Path() string              { return r.path }
func (r generationMockRequest) Header(key string) string  { return r.headers[key] }
func (r generationMockRequest) Query(key string) []string { return r.query[key] }
func (r generationMockRequest) BodyAsBytes() ([]byte, error) {
	return r.body, r.bodyErr
}
func (r generationMockRequest) PathValue(name string) string { return r.pathValue[name] }
func (r generationMockRequest) SetHeader(key, value string)  { r.headers[key] = value }

func newGenerationMockHandler() *handlers.Handlers {
	return handlers.New(nil, nil, nil, nil, nil, nil, nil)
}

func newGenerationMockContext() *executioncontext.ExecutionContext {
	return &executioncontext.ExecutionContext{
		Ctx:       context.Background(),
		RequestID: "request-id",
		Tenant:    api.Tenant("tenant-a"),
		User:      api.User("user-a"),
	}
}

func newGenerationMockRequest(method, query string, body []byte) generationMockRequest {
	parsed, _ := url.Parse("/api/v1/evaluations/collections/generation" + query)
	return generationMockRequest{
		method:    method,
		uri:       parsed.String(),
		path:      parsed.Path,
		query:     parsed.Query(),
		body:      body,
		pathValue: map[string]string{"id": "550e8400-e29b-41d4-a716-446655440099"},
		headers:   make(map[string]string),
	}
}

func TestCollectionGenerationMockHandlers(t *testing.T) {
	t.Parallel()

	handler := newGenerationMockHandler()
	ctx := newGenerationMockContext()
	validBody := []byte(`{"evaluation_goal":"goal","collection_name":"  Mock generated collection  ","collection_description":"  Temporary stateless collection-generation fixture.  ","model":{"name":"generator","url":"https://generator.example/v1"}}`)

	t.Run("create request and mock validation branches", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name       string
			request    generationMockRequest
			wantStatus int
		}{
			{name: "mock option omitted", request: newGenerationMockRequest("POST", "", validBody), wantStatus: 503},
			{name: "mock option invalid", request: newGenerationMockRequest("POST", "?mock=false", validBody), wantStatus: 400},
			{name: "body read error", request: func() generationMockRequest {
				r := newGenerationMockRequest("POST", "?mock=true", validBody)
				r.bodyErr = errors.New("read failed")
				return r
			}(), wantStatus: 500},
			{name: "invalid request body", request: newGenerationMockRequest("POST", "?mock=true", []byte(`{"evaluation_goal":"goal"}`)), wantStatus: 400},
			{name: "credentials in model parameters", request: newGenerationMockRequest("POST", "?mock=true", []byte(`{"evaluation_goal":"goal","model":{"name":"generator","url":"https://generator.example/v1","parameters":{"api_key":"plaintext"}}}`)), wantStatus: 400},
		}
		for _, test := range tests {
			test := test
			t.Run(test.name, func(t *testing.T) {
				t.Parallel()
				recorder := httptestRecorder()
				handler.HandleCreateCollectionGeneration(ctx, test.request, MockResponseWrapper{recorder: recorder})
				if recorder.Code != test.wantStatus {
					t.Fatalf("status = %d, want %d; response=%s", recorder.Code, test.wantStatus, recorder.Body.String())
				}
			})
		}
	})

	t.Run("create returns a pending resource and normalized request", func(t *testing.T) {
		t.Parallel()
		recorder := httptestRecorder()
		request := newGenerationMockRequest("POST", "?mock=true", validBody)
		handler.HandleCreateCollectionGeneration(ctx, request, MockResponseWrapper{recorder: recorder})
		if recorder.Code != 202 {
			t.Fatalf("status = %d, want 202; response=%s", recorder.Code, recorder.Body.String())
		}
		if got := recorder.Header().Get("Location"); !strings.Contains(got, "?mock=complete") {
			t.Fatalf("Location = %q, want completion fixture URL", got)
		}
		if recorder.Header().Get("X-EvalHub-Mock") != "true" || recorder.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("mock headers missing: %v", recorder.Header())
		}
		var response map[string]any
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		resource := response["resource"].(map[string]any)
		if resource["tenant"] != "tenant-a" || resource["owner"] != "user-a" || response["status"].(map[string]any)["state"] != "pending" {
			t.Fatalf("unexpected create response: %#v", response)
		}
		requestSummary := response["request"].(map[string]any)
		if requestSummary["collection_name"] != "Mock generated collection" || requestSummary["collection_description"] != "Temporary stateless collection-generation fixture." {
			t.Fatalf("requested collection fields missing or not trimmed: %#v", requestSummary)
		}
	})

	t.Run("get progress, completed result, failure, invalid ID, and invalid option", func(t *testing.T) {
		t.Parallel()
		progress := newGenerationMockRequest("GET", "?mock=progress", nil)
		progress.pathValue["id"] = "550e8400-e29b-41d4-a716-446655440099"
		progressRecorder := httptestRecorder()
		handler.HandleGetCollectionGeneration(ctx, progress, MockResponseWrapper{recorder: progressRecorder})
		if progressRecorder.Code != 200 {
			t.Fatalf("progress status = %d; response=%s", progressRecorder.Code, progressRecorder.Body.String())
		}
		var progressBody map[string]any
		if err := json.Unmarshal(progressRecorder.Body.Bytes(), &progressBody); err != nil {
			t.Fatalf("decode progress response: %v", err)
		}
		if progressBody["status"].(map[string]any)["state"] != "running" || progressBody["status"].(map[string]any)["progress"] == nil {
			t.Fatalf("progress response missing running progress: %#v", progressBody)
		}

		complete := newGenerationMockRequest("GET", "?mock=complete", nil)
		completeRecorder := httptestRecorder()
		handler.HandleGetCollectionGeneration(ctx, complete, MockResponseWrapper{recorder: completeRecorder})
		if completeRecorder.Code != 200 {
			t.Fatalf("complete status = %d; response=%s", completeRecorder.Code, completeRecorder.Body.String())
		}
		var completeBody map[string]any
		if err := json.Unmarshal(completeRecorder.Body.Bytes(), &completeBody); err != nil {
			t.Fatalf("decode complete response: %v", err)
		}
		result := completeBody["result"].(map[string]any)
		if result["collection"] == nil || result["yaml"] == nil || completeBody["status"].(map[string]any)["state"] != "completed" {
			t.Fatalf("completed response missing collection proposal: %#v", completeBody)
		}
		completedRequest := completeBody["request"].(map[string]any)
		collection := result["collection"].(map[string]any)
		if completedRequest["collection_name"] != collection["name"] || completedRequest["collection_description"] != collection["description"] ||
			!strings.Contains(result["yaml"].(string), "name: Mock generated collection") ||
			!strings.Contains(result["yaml"].(string), "description: Temporary stateless collection-generation fixture.") {
			t.Fatalf("completed fixture request, collection, and YAML disagree: %#v", completeBody)
		}

		failure := newGenerationMockRequest("GET", "?mock=failure", nil)
		failureRecorder := httptestRecorder()
		handler.HandleGetCollectionGeneration(ctx, failure, MockResponseWrapper{recorder: failureRecorder})
		if failureRecorder.Code != 200 {
			t.Fatalf("failure status = %d; response=%s", failureRecorder.Code, failureRecorder.Body.String())
		}
		var failureBody map[string]any
		if err := json.Unmarshal(failureRecorder.Body.Bytes(), &failureBody); err != nil {
			t.Fatalf("decode failure response: %v", err)
		}
		failureStatus := failureBody["status"].(map[string]any)
		failureError := failureStatus["error"].(map[string]any)
		if failureStatus["state"] != "failed" || failureStatus["completed_at"] == nil ||
			failureError["code"] != "guardrails_rejected" || failureError["category"] != "topic_relevance" ||
			failureError["message"] == "" || len(failureError) != 3 ||
			failureBody["result"] != nil || failureStatus["progress"] != nil ||
			failureRecorder.Header().Get("X-EvalHub-Mock") != "true" {
			t.Fatalf("failed response does not match terminal error contract: %#v", failureBody)
		}

		noRelevant := newGenerationMockRequest("GET", "?mock=no_relevant_benchmarks", nil)
		noRelevantRecorder := httptestRecorder()
		handler.HandleGetCollectionGeneration(ctx, noRelevant, MockResponseWrapper{recorder: noRelevantRecorder})
		if noRelevantRecorder.Code != 200 {
			t.Fatalf("no-relevant status = %d; response=%s", noRelevantRecorder.Code, noRelevantRecorder.Body.String())
		}
		var noRelevantBody map[string]any
		if err := json.Unmarshal(noRelevantRecorder.Body.Bytes(), &noRelevantBody); err != nil {
			t.Fatalf("decode no-relevant response: %v", err)
		}
		noRelevantStatus := noRelevantBody["status"].(map[string]any)
		noRelevantError := noRelevantStatus["error"].(map[string]any)
		if noRelevantStatus["state"] != "failed" || noRelevantError["code"] != "no_relevant_benchmarks" ||
			noRelevantError["message"] == "" || len(noRelevantError) != 2 ||
			noRelevantBody["result"] != nil || noRelevantStatus["progress"] != nil {
			t.Fatalf("no-relevant response does not match terminal error contract: %#v", noRelevantBody)
		}

		unknown := newGenerationMockRequest("GET", "?mock=complete", nil)
		unknown.pathValue["id"] = "unknown"
		unknownRecorder := httptestRecorder()
		handler.HandleGetCollectionGeneration(ctx, unknown, MockResponseWrapper{recorder: unknownRecorder})
		if unknownRecorder.Code != 404 {
			t.Fatalf("unknown ID status = %d, want 404", unknownRecorder.Code)
		}

		invalid := newGenerationMockRequest("GET", "?mock=invalid", nil)
		invalidRecorder := httptestRecorder()
		handler.HandleGetCollectionGeneration(ctx, invalid, MockResponseWrapper{recorder: invalidRecorder})
		if invalidRecorder.Code != 400 {
			t.Fatalf("invalid option status = %d, want 400", invalidRecorder.Code)
		}
	})

	t.Run("delete validates mock and resource ID and is a no-op", func(t *testing.T) {
		t.Parallel()
		for _, test := range []struct {
			name       string
			query      string
			id         string
			wantStatus int
		}{
			{name: "option omitted", wantStatus: 503},
			{name: "option invalid", query: "?mock=false", wantStatus: 400},
			{name: "unknown ID", query: "?mock=true", id: "unknown", wantStatus: 404},
			{name: "known ID", query: "?mock=true", id: "550e8400-e29b-41d4-a716-446655440099", wantStatus: 204},
		} {
			test := test
			t.Run(test.name, func(t *testing.T) {
				t.Parallel()
				request := newGenerationMockRequest("DELETE", test.query, nil)
				request.pathValue["id"] = test.id
				recorder := httptestRecorder()
				handler.HandleDeleteCollectionGeneration(ctx, request, MockResponseWrapper{recorder: recorder})
				if recorder.Code != test.wantStatus {
					t.Fatalf("status = %d, want %d; response=%s", recorder.Code, test.wantStatus, recorder.Body.String())
				}
				if test.wantStatus == 204 && (recorder.Body.Len() != 0 || recorder.Header().Get("X-EvalHub-Mock") != "true") {
					t.Fatalf("successful DELETE response body=%q headers=%v", recorder.Body.String(), recorder.Header())
				}
			})
		}
	})
}

func httptestRecorder() *httptest.ResponseRecorder {
	return httptest.NewRecorder()
}
