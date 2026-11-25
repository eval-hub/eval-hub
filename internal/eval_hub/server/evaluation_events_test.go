package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/common"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestEvaluationEventsPreserveCompletedBenchmarkAfterLateFailure(t *testing.T) {
	t.Setenv("DB_DRIVER", "sqlite")
	t.Setenv("DB_URL", "file:"+common.GUID()+"?mode=memory&cache=shared")

	srv, err := createServerWithLocalMode(t, 8080, false)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := srv.SetupRoutes()
	if err != nil {
		t.Fatal(err)
	}

	request := func(method, path, caller, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("X-Tenant", "tenant-a")
		req.Header.Set("X-User", caller)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	const jobsPath = "/api/v1/evaluations/jobs"
	const owner = "test-user"
	rec := request(http.MethodPost, jobsPath, owner, `{
		"name": "late failure regression",
		"model": {"name": "test", "url": "http://test.com"},
		"benchmarks": [
			{"id": "arc_easy", "provider_id": "lm_evaluation_harness"},
			{"id": "AraDiCE_boolq_lev", "provider_id": "lm_evaluation_harness"}
		]
	}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("create evaluation: got %d: %s", rec.Code, rec.Body)
	}
	decodeJob := func(rec *httptest.ResponseRecorder) api.EvaluationJobResource {
		t.Helper()
		var job api.EvaluationJobResource
		if err := json.NewDecoder(rec.Body).Decode(&job); err != nil {
			t.Fatalf("decode evaluation: %v", err)
		}
		return job
	}
	job := decodeJob(rec)
	if job.Resource.ID == "" {
		t.Fatal("create evaluation returned no ID")
	}
	jobPath := jobsPath + "/" + job.Resource.ID
	eventsPath := jobPath + "/events"

	assertCompleted := func(stage string) {
		t.Helper()
		rec := request(http.MethodGet, jobPath, owner, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: read evaluation: got %d: %s", stage, rec.Code, rec.Body)
		}
		job := decodeJob(rec)
		if job.Status == nil || job.Status.State != api.OverallStateRunning {
			t.Fatalf("%s: evaluation should still be running while the second benchmark is pending: %+v", stage, job.Status)
		}
		first := slices.IndexFunc(job.Status.Benchmarks, func(benchmark api.BenchmarkStatus) bool {
			return benchmark.ID == "arc_easy" && benchmark.BenchmarkIndex == 0
		})
		if first < 0 {
			t.Fatalf("%s: first benchmark missing from statuses: %+v", stage, job.Status.Benchmarks)
		}
		if got := job.Status.Benchmarks[first].Status; got != api.StateCompleted {
			t.Errorf("%s: first benchmark status = %q, want %q", stage, got, api.StateCompleted)
		}
		if job.Results == nil || len(job.Results.Benchmarks) != 1 || job.Results.Benchmarks[0].ID != "arc_easy" {
			t.Fatalf("%s: unexpected benchmark results: %+v", stage, job.Results)
		}
		if got := job.Results.Benchmarks[0].Metrics["acc_norm"]; got != 0.75 {
			t.Errorf("%s: saved accuracy = %v, want 0.75", stage, got)
		}
	}

	rec = request(http.MethodPost, eventsPath, owner, `{"benchmark_status_event":{
		"provider_id":"lm_evaluation_harness", "id":"arc_easy", "benchmark_index":0,
		"status":"completed", "metrics":{"acc_norm":0.75}
	}}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("report completion: got %d: %s", rec.Code, rec.Body)
	}
	assertCompleted("before late failure")
	if t.Failed() {
		return
	}

	rec = request(http.MethodPost, eventsPath, "system:serviceaccount:operator:trustyai-service-operator", `{"benchmark_status_event":{
		"provider_id":"lm_evaluation_harness", "id":"arc_easy", "benchmark_index":0,
		"status":"failed", "error_message":{
			"message_code":"RUNTIME_FAILURE", "message":"Kubernetes marked the evaluation Job as failed."
		}
	}}`)
	if rec.Code != http.StatusConflict {
		t.Errorf("report late failure: got %d, want 409: %s", rec.Code, rec.Body)
	} else {
		// The operator uses the conflict message to confirm the benchmark's final state.
		if !strings.Contains(rec.Body.String(), "'completed'") {
			t.Errorf("conflict response does not identify the completed state: %s", rec.Body)
		}
		assertMessageCode(t, rec, "benchmark_can_not_be_updated")
	}
	assertCompleted("after late failure")
}
