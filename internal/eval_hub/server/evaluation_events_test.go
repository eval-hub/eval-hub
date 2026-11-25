package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/common"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestEvaluationConditionalFailureEvents(t *testing.T) {
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

	const (
		jobsPath      = "/api/v1/evaluations/jobs"
		owner         = "test-user"
		operator      = "system:serviceaccount:operator:trustyai-service-operator"
		first         = "arc_easy"
		second        = "AraDiCE_boolq_lev"
		provider      = "lm_evaluation_harness"
		operatorEvent = `{"benchmark_status_event":{
			"provider_id":"lm_evaluation_harness", "id":"arc_easy", "status":"failed",
			"error_message":{"message_code":"RUNTIME_FAILURE", "message":"Job failed."},
			"completed_at":"2026-11-24T12:00:00Z",
			"precondition":"nonterminal"
		}}`
	)

	request := func(method, path, caller, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("X-Tenant", "tenant-a")
		req.Header.Set("X-User", caller)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	decodeJob := func(t *testing.T, rec *httptest.ResponseRecorder) api.EvaluationJobResource {
		t.Helper()
		var job api.EvaluationJobResource
		if err := json.Unmarshal(rec.Body.Bytes(), &job); err != nil {
			t.Fatalf("decode evaluation: %v", err)
		}
		return job
	}

	createJob := func(t *testing.T, ids ...string) string {
		t.Helper()
		config := api.EvaluationJobConfig{
			Name:  "conditional failure regression",
			Model: &api.ModelRef{Name: "test", URL: "http://test.com"},
		}
		for _, id := range ids {
			config.Benchmarks = append(config.Benchmarks, api.EvaluationBenchmarkConfig{Ref: api.Ref{ID: id}, ProviderID: provider})
		}
		body, err := json.Marshal(config)
		if err != nil {
			t.Fatal(err)
		}
		rec := request(http.MethodPost, jobsPath, owner, string(body))
		if rec.Code != http.StatusAccepted {
			t.Fatalf("create evaluation: got %d: %s", rec.Code, rec.Body)
		}
		return jobsPath + "/" + decodeJob(t, rec).Resource.ID
	}

	readJob := func(t *testing.T, path string) api.EvaluationJobResource {
		t.Helper()
		rec := request(http.MethodGet, path, owner, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("read evaluation: got %d: %s", rec.Code, rec.Body)
		}
		return decodeJob(t, rec)
	}

	postAdapter := func(t *testing.T, path string, event api.BenchmarkStatusEvent, wantStatus int) {
		t.Helper()
		event.ProviderID = provider
		body, err := json.Marshal(api.StatusEvent{BenchmarkStatusEvent: &event})
		if err != nil {
			t.Fatal(err)
		}
		rec := request(http.MethodPost, path+"/events", owner, string(body))
		if rec.Code != wantStatus {
			t.Fatalf("adapter reports %s: got %d, want %d: %s", event.Status, rec.Code, wantStatus, rec.Body)
		}
	}

	postOperator := func(t *testing.T, path, expected string) {
		t.Helper()
		rec := request(http.MethodPost, path+"/events", operator, operatorEvent)
		if rec.Code != http.StatusOK {
			t.Fatalf("conditional failure: got %d, want 200 (%s): %s", rec.Code, expected, rec.Body)
		}
		var response struct {
			Outcome string `json:"outcome"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatalf("decode outcome: %v", err)
		}
		if response.Outcome != expected {
			t.Fatalf("outcome = %q, want %q", response.Outcome, expected)
		}
	}

	for _, tc := range []struct {
		firstStatus api.State
		finalState  api.OverallState
	}{
		{api.StateCompleted, api.OverallStatePartiallyFailed},
		{api.StateFailed, api.OverallStateFailed},
	} {
		t.Run("adapter_first_"+string(tc.firstStatus), func(t *testing.T) {
			path := createJob(t, first, second)
			postAdapter(t, path, api.BenchmarkStatusEvent{
				ID: first, Status: tc.firstStatus, Metrics: map[string]any{"acc_norm": 0.75},
				ErrorMessage: &api.MessageInfo{Message: "Adapter detail.", MessageCode: "ADAPTER_MESSAGE"},
				Artifacts:    map[string]any{"report": "file:///tmp/report.json"}, LogsPath: "/data/logs/adapter.log",
			}, http.StatusNoContent)
			before := readJob(t, path)
			if before.Status.State != api.OverallStateRunning || before.Results == nil ||
				len(before.Results.Benchmarks) != 1 || before.Results.Benchmarks[0].LogsPath == "" {
				t.Fatalf("expected saved adapter result with an active sibling: %+v", before)
			}
			postOperator(t, path, "already_terminal")
			after := readJob(t, path)
			if !reflect.DeepEqual(before.Status, after.Status) || !reflect.DeepEqual(before.Results, after.Results) {
				t.Fatalf("late failure changed the stored benchmark: status=%+v results=%+v", after.Status, after.Results)
			}

			postAdapter(t, path, api.BenchmarkStatusEvent{
				ID: second, BenchmarkIndex: 1, Status: api.StateFailed,
			}, http.StatusNoContent)
			final := readJob(t, path)
			if final.Status.State != tc.finalState {
				t.Errorf("state after sibling failure = %q, want %q", final.Status.State, tc.finalState)
			}
			if !slices.ContainsFunc(final.Status.Benchmarks, func(b api.BenchmarkStatus) bool {
				return b.BenchmarkIndex == 0 && b.ID == first && b.Status == tc.firstStatus
			}) || !slices.ContainsFunc(final.Status.Benchmarks, func(b api.BenchmarkStatus) bool {
				return b.BenchmarkIndex == 1 && b.ID == second && b.Status == api.StateFailed
			}) {
				t.Errorf("unexpected benchmark statuses after sibling failure: %+v", final.Status.Benchmarks)
			}
			if final.Results == nil || !slices.ContainsFunc(final.Results.Benchmarks, func(b api.BenchmarkResult) bool {
				return b.BenchmarkIndex == 0 && b.ID == first && b.Metrics["acc_norm"] == 0.75
			}) {
				t.Errorf("saved metric missing after sibling failure: %+v", final.Results)
			}
		})
	}

	for _, tc := range []struct {
		name     string
		ids      []string
		before   api.OverallState
		callback api.State
		want     api.OverallState
	}{
		{"complete_after_failed", []string{first}, api.OverallStateFailed, api.StateCompleted, api.OverallStateCompleted},
		{"complete_after_partially_failed", []string{first, second}, api.OverallStatePartiallyFailed, api.StateCompleted, api.OverallStateCompleted},
		{"failure_after_partially_failed", []string{first, second}, api.OverallStatePartiallyFailed, api.StateFailed, api.OverallStatePartiallyFailed},
	} {
		t.Run("operator_first_"+tc.name, func(t *testing.T) {
			path := createJob(t, tc.ids...)
			if len(tc.ids) > 1 {
				postAdapter(t, path, api.BenchmarkStatusEvent{
					ID: second, BenchmarkIndex: 1, Status: api.StateCompleted, Metrics: map[string]any{"acc": 0.5},
				}, http.StatusNoContent)
			}
			postOperator(t, path, "applied")
			before := readJob(t, path)
			if before.Status.State != tc.before {
				t.Fatalf("state before callback = %q, want %q", before.Status.State, tc.before)
			}
			postOperator(t, path, "already_terminal")
			postAdapter(t, path, api.BenchmarkStatusEvent{ID: first, Status: api.StateRunning}, http.StatusConflict)
			event := api.BenchmarkStatusEvent{
				ID: first, Status: tc.callback, CompletedAt: "2026-11-24T12:01:00Z",
				Metrics: map[string]any{"acc_norm": 0.75}, LogsPath: "/data/logs/adapter.log",
			}
			if tc.callback == api.StateFailed {
				event.ErrorMessage = &api.MessageInfo{Message: "Adapter detail.", MessageCode: "ADAPTER_FAILURE"}
			}
			postAdapter(t, path, event, http.StatusNoContent)
			job := readJob(t, path)
			if job.Status.State != tc.want {
				t.Fatalf("state after callback = %q, want %q", job.Status.State, tc.want)
			}
			var found bool
			for _, benchmark := range job.Status.Benchmarks {
				if benchmark.ID == first {
					found = true
					if benchmark.Status != tc.callback || benchmark.CompletedAt != event.CompletedAt ||
						!reflect.DeepEqual(benchmark.ErrorMessage, api.DefaultMessageOrigin(event.ErrorMessage, api.MessageOriginRuntime)) {
						t.Errorf("benchmark did not retain the adapter's final report: %+v", benchmark)
					}
				}
			}
			if !found {
				t.Error("updated benchmark status is missing")
			}
			for _, result := range job.Results.Benchmarks {
				if result.ID == first {
					if result.Metrics["acc_norm"] != 0.75 || result.LogsPath != event.LogsPath {
						t.Errorf("benchmark did not retain the adapter result: %+v", result)
					}
					return
				}
			}
			t.Error("updated benchmark result is missing")
		})
	}

	t.Run("ordinary_failure_is_not_recoverable", func(t *testing.T) {
		path := createJob(t, first, second)
		postAdapter(t, path, api.BenchmarkStatusEvent{
			ID: first, Status: api.StateFailed,
			ErrorMessage: &api.MessageInfo{
				Message: "Adapter detail.", MessageCode: "KUBERNETES_JOB_FAILURE", MessageOrigin: api.MessageOriginServer,
			},
		}, http.StatusNoContent)
		before := readJob(t, path)
		if before.Status.State != api.OverallStateRunning || before.Status.Benchmarks[0].ErrorMessage.MessageOrigin != api.MessageOriginRuntime {
			t.Fatalf("expected an ordinary adapter failure: %+v", before.Status)
		}
		postAdapter(t, path, api.BenchmarkStatusEvent{ID: first, Status: api.StateCompleted}, http.StatusConflict)
		if after := readJob(t, path); !reflect.DeepEqual(before.Status, after.Status) {
			t.Errorf("adapter failure changed after rejected completion: %+v", after.Status)
		}
	})

	t.Run("conditional_event_validation", func(t *testing.T) {
		path := createJob(t, first)
		body := strings.Replace(operatorEvent, `"nonterminal"`, `"unknown"`, 1)
		rec := request(http.MethodPost, path+"/events", operator, body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("unsupported precondition: got %d, want 400: %s", rec.Code, rec.Body)
		}
		postOperator(t, path, "applied")
		body = strings.Replace(operatorEvent, `"id":"arc_easy"`, `"id":"missing"`, 1)
		rec = request(http.MethodPost, path+"/events", operator, body)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("unknown benchmark in a terminal evaluation: got %d, want 404: %s", rec.Code, rec.Body)
		}
	})

	t.Run("cancelled_evaluation", func(t *testing.T) {
		path := createJob(t, first, second)
		postOperator(t, path, "applied")
		rec := request(http.MethodDelete, path, owner, "")
		if rec.Code != http.StatusNoContent {
			t.Fatalf("cancel evaluation: got %d: %s", rec.Code, rec.Body)
		}
		postOperator(t, path, "already_terminal")
		postAdapter(t, path, api.BenchmarkStatusEvent{ID: first, Status: api.StateCompleted}, http.StatusConflict)
		if got := readJob(t, path).Status.State; got != api.OverallStateCancelled {
			t.Errorf("state after callbacks = %q, want cancelled", got)
		}
	})
}
