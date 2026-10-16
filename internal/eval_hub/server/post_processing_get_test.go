package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/postprocessing"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestPostProcessingGetMapsEvaluationJobResults(t *testing.T) {
	handler, store, runtime := newPostProcessingServer(t)
	source := createPostProcessingSource(t, store, api.OverallStateCompleted)
	body := postProcessingBody(fmt.Sprintf(`{"eval_job":{"id":%q}}`, source.Resource.ID))
	submitted := postProcessingRequest(handler, http.MethodPost, postProcessingPath, body)
	if submitted.Code != http.StatusAccepted {
		t.Fatalf("submit status %d: %s", submitted.Code, submitted.Body.String())
	}
	var created api.PostProcessingResource
	if err := json.Unmarshal(submitted.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	event := &api.StatusEvent{BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
		ProviderID: postprocessing.ProviderID,
		ID:         postprocessing.BenchmarkID,
		Status:     api.StateCompleted,
		AdditionalInfo: map[string]any{"confidence_interval": map[string]any{
			"benchmarks": []any{map[string]any{
				"id": "accuracy", "provider_id": "source-provider", "benchmark_index": 0,
				"confidence_interval": map[string]any{"lower": 0.82, "upper": 0.94},
			}},
		}},
	}}
	if err := runtime.storage.UpdateEvaluationJob(created.Resource.ID, event); err != nil {
		t.Fatal(err)
	}

	fetched := postProcessingRequest(handler, http.MethodGet, postProcessingPath+"/"+created.Resource.ID, "")
	if fetched.Code != http.StatusOK {
		t.Fatalf("get status %d: %s", fetched.Code, fetched.Body.String())
	}
	var resource api.PostProcessingResource
	if err := json.Unmarshal(fetched.Body.Bytes(), &resource); err != nil {
		t.Fatal(err)
	}
	if resource.Status.State != api.StateCompleted || resource.Results == nil || len(resource.Results.Benchmarks) != 1 {
		t.Fatalf("GET response is missing mapped results: %+v", resource)
	}
	var responseJSON map[string]json.RawMessage
	if err := json.Unmarshal(fetched.Body.Bytes(), &responseJSON); err != nil {
		t.Fatal(err)
	}
	var statusJSON map[string]json.RawMessage
	if err := json.Unmarshal(responseJSON["status"], &statusJSON); err != nil {
		t.Fatal(err)
	}
	if _, exists := statusJSON["benchmarks"]; exists {
		t.Fatal("post-processing status must not expose per-benchmark status")
	}

	wrongEvaluationGet := postProcessingRequest(handler, http.MethodGet, "/api/v1/evaluations/jobs/"+created.Resource.ID, "")
	if wrongEvaluationGet.Code != http.StatusNotFound {
		t.Fatalf("evaluation-job GET for post-processing ID returned %d, want 404: %s", wrongEvaluationGet.Code, wrongEvaluationGet.Body.String())
	}
	wrongPostProcessingGet := postProcessingRequest(handler, http.MethodGet, postProcessingPath+"/"+source.Resource.ID, "")
	if wrongPostProcessingGet.Code != http.StatusNotFound {
		t.Fatalf("post-processing GET for evaluation-job ID returned %d, want 404: %s", wrongPostProcessingGet.Code, wrongPostProcessingGet.Body.String())
	}
	listedEvaluationJobs := postProcessingRequest(handler, http.MethodGet, "/api/v1/evaluations/jobs", "")
	if listedEvaluationJobs.Code != http.StatusOK {
		t.Fatalf("evaluation-job list returned %d: %s", listedEvaluationJobs.Code, listedEvaluationJobs.Body.String())
	}
	var evaluationJobs api.EvaluationJobResourceList
	if err := json.Unmarshal(listedEvaluationJobs.Body.Bytes(), &evaluationJobs); err != nil {
		t.Fatal(err)
	}
	if len(evaluationJobs.Items) != 1 || evaluationJobs.Items[0].Resource.ID != source.Resource.ID {
		t.Fatalf("evaluation-job list exposed the wrong resources: %+v", evaluationJobs.Items)
	}
	if _, err := store.GetEvaluationJob(created.Resource.ID); err != nil {
		t.Fatalf("cross-API DELETE removed the post-processing job: %v", err)
	}
	if _, err := store.GetEvaluationJob(source.Resource.ID); err != nil {
		t.Fatalf("cross-API DELETE removed the evaluation job: %v", err)
	}
	result := resource.Results.Benchmarks[0]
	if result.ID != "accuracy" || result.ProviderID != "source-provider" || result.BenchmarkIndex != 0 || result.ConfidenceInterval.Lower != 0.82 || result.ConfidenceInterval.Upper != 0.94 {
		t.Fatalf("GET response has incorrect mapped result: %+v", result)
	}
}

func TestPostProcessingGetMethodsAndIdentity(t *testing.T) {
	handler, _, _ := newPostProcessingServer(t)
	itemMethod := postProcessingRequest(handler, http.MethodPut, postProcessingPath+"/post-processing-id", "")
	if itemMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PUT to a post-processing item returned %d, want 405", itemMethod.Code)
	}
	missingItemIdentity := httptest.NewRequest(http.MethodGet, postProcessingPath+"/post-processing-id", nil)
	missingItemIdentity.Header.Set("X-Tenant", "tenant-a")
	missingItemResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingItemResponse, missingItemIdentity)
	if missingItemResponse.Code < 400 {
		t.Fatalf("GET post-processing item without X-User returned %d", missingItemResponse.Code)
	}
}
