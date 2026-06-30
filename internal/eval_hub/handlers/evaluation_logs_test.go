package handlers_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/constants"
	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/handlers"
	"github.com/eval-hub/eval-hub/internal/eval_hub/validation"
	"github.com/eval-hub/eval-hub/pkg/api"
)

type logsRuntime struct {
	logs                   string
	err                    error
	getLogsCalled          bool
	capturedBenchmarkIndex *int
	capturedOpts           api.EvaluationLogOptions
}

func (r *logsRuntime) WithLogger(_ *slog.Logger) abstractions.Runtime { return r }
func (r *logsRuntime) WithContext(_ context.Context) abstractions.Runtime {
	return r
}
func (r *logsRuntime) Name() string { return "logs" }
func (r *logsRuntime) RunEvaluationJob(
	_ *api.EvaluationJobResource,
	_ []api.EvaluationBenchmarkConfig,
	_ abstractions.RuntimeStorage,
) error {
	return nil
}
func (r *logsRuntime) DeleteEvaluationJobResources(_ *api.EvaluationJobResource) error { return nil }
func (r *logsRuntime) GetEvaluationLogs(
	_ *api.EvaluationJobResource,
	_ []api.EvaluationBenchmarkConfig,
	benchmarkIndex *int,
	opts api.EvaluationLogOptions,
) (string, error) {
	r.getLogsCalled = true
	r.capturedBenchmarkIndex = benchmarkIndex
	r.capturedOpts = opts
	if r.err != nil {
		return "", r.err
	}
	return r.logs, nil
}

type logsRequest struct {
	*MockRequest
	pathValues  map[string]string
	queryValues map[string][]string
}

func (r *logsRequest) PathValue(name string) string {
	return r.pathValues[name]
}

func (r *logsRequest) Query(key string) []string {
	if values, ok := r.queryValues[key]; ok {
		return values
	}
	return nil
}

func TestHandleGetEvaluationJobLogs(t *testing.T) {
	jobID := "job-logs"
	runtime := &logsRuntime{logs: "hello logs"}
	storage := &fakeStorage{
		job: &api.EvaluationJobResource{
			Resource: api.EvaluationResource{
				Resource: api.Resource{ID: jobID},
			},
			EvaluationJobConfig: api.EvaluationJobConfig{
				Benchmarks: []api.EvaluationBenchmarkConfig{
					{Ref: api.Ref{ID: "bench-1"}, ProviderID: "provider-1"},
				},
			},
		},
	}
	h := handlers.New(storage, validation.NewValidator(), runtime, nil, nil)
	rec := httptest.NewRecorder()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, "test-user", "test-tenant")
	req := &logsRequest{
		MockRequest: createMockRequest(http.MethodGet, "/api/v1/evaluations/jobs/"+jobID+"/logs"),
		pathValues:  map[string]string{constants.PATH_PARAMETER_JOB_ID: jobID},
		queryValues: map[string][]string{
			"tail_lines":    {"500"},
			"timestamps":    {"true"},
			"previous":      {"true"},
			"since_seconds": {"120"},
		},
	}

	h.HandleGetEvaluationJobLogs(ctx, req, MockResponseWrapper{recorder: rec})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("content-type = %q, want text/plain", ct)
	}
	if body := strings.TrimSpace(rec.Body.String()); body != "hello logs" {
		t.Fatalf("body = %q, want %q", body, "hello logs")
	}
	if !runtime.getLogsCalled {
		t.Fatal("expected GetEvaluationLogs to be called")
	}
	if runtime.capturedBenchmarkIndex != nil {
		t.Fatalf("benchmark index = %v, want nil", runtime.capturedBenchmarkIndex)
	}
	if runtime.capturedOpts.TailLines != 500 {
		t.Fatalf("tail_lines = %d, want 500", runtime.capturedOpts.TailLines)
	}
	if !runtime.capturedOpts.Timestamps {
		t.Fatal("expected timestamps=true")
	}
	if !runtime.capturedOpts.Previous {
		t.Fatal("expected previous=true")
	}
	if runtime.capturedOpts.SinceSeconds == nil || *runtime.capturedOpts.SinceSeconds != 120 {
		t.Fatalf("since_seconds = %v, want 120", runtime.capturedOpts.SinceSeconds)
	}
}

func TestHandleGetEvaluationBenchmarkLogs(t *testing.T) {
	jobID := "job-logs-bench"
	runtime := &logsRuntime{logs: "bench log"}
	storage := &fakeStorage{
		job: &api.EvaluationJobResource{
			Resource: api.EvaluationResource{
				Resource: api.Resource{ID: jobID},
			},
			EvaluationJobConfig: api.EvaluationJobConfig{
				Benchmarks: []api.EvaluationBenchmarkConfig{
					{Ref: api.Ref{ID: "bench-1"}, ProviderID: "provider-1"},
				},
			},
		},
	}
	h := handlers.New(storage, validation.NewValidator(), runtime, nil, nil)
	rec := httptest.NewRecorder()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-2", logger, "test-user", "test-tenant")
	req := &logsRequest{
		MockRequest: createMockRequest(http.MethodGet, "/api/v1/evaluations/jobs/"+jobID+"/benchmarks/0/logs"),
		pathValues: map[string]string{
			constants.PATH_PARAMETER_JOB_ID:          jobID,
			constants.PATH_PARAMETER_BENCHMARK_INDEX: "0",
		},
		queryValues: map[string][]string{
			"tail_lines": {"250"},
		},
	}

	h.HandleGetEvaluationBenchmarkLogs(ctx, req, MockResponseWrapper{recorder: rec})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if body := strings.TrimSpace(rec.Body.String()); body != "bench log" {
		t.Fatalf("body = %q, want %q", body, "bench log")
	}
	if runtime.capturedBenchmarkIndex == nil || *runtime.capturedBenchmarkIndex != 0 {
		t.Fatalf("benchmark index = %v, want 0", runtime.capturedBenchmarkIndex)
	}
	if runtime.capturedOpts.TailLines != 250 {
		t.Fatalf("tail_lines = %d, want 250", runtime.capturedOpts.TailLines)
	}
	if runtime.capturedOpts.SinceSeconds != nil {
		t.Fatalf("since_seconds = %v, want nil", runtime.capturedOpts.SinceSeconds)
	}
}

func TestHandleGetEvaluationJobLogsRejectsInvalidTailLines(t *testing.T) {
	jobID := "job-logs-invalid"
	storage := &fakeStorage{
		job: &api.EvaluationJobResource{
			Resource: api.EvaluationResource{
				Resource: api.Resource{ID: jobID},
			},
			EvaluationJobConfig: api.EvaluationJobConfig{
				Benchmarks: []api.EvaluationBenchmarkConfig{
					{Ref: api.Ref{ID: "bench-1"}, ProviderID: "provider-1"},
				},
			},
		},
	}
	runtime := &logsRuntime{logs: "ignored"}
	h := handlers.New(storage, validation.NewValidator(), runtime, nil, nil)
	rec := httptest.NewRecorder()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-3", logger, "test-user", "test-tenant")
	req := &logsRequest{
		MockRequest: createMockRequest(http.MethodGet, "/api/v1/evaluations/jobs/"+jobID+"/logs?tail_lines=0"),
		pathValues:  map[string]string{constants.PATH_PARAMETER_JOB_ID: jobID},
		queryValues: map[string][]string{"tail_lines": {"0"}},
	}

	h.HandleGetEvaluationJobLogs(ctx, req, MockResponseWrapper{recorder: rec})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if runtime.getLogsCalled {
		t.Fatal("expected GetEvaluationLogs not to be called")
	}
}

func TestHandleGetEvaluationJobLogsRejectsEmptySinceSeconds(t *testing.T) {
	jobID := "job-logs-empty-since"
	storage := &fakeStorage{
		job: &api.EvaluationJobResource{
			Resource: api.EvaluationResource{
				Resource: api.Resource{ID: jobID},
			},
			EvaluationJobConfig: api.EvaluationJobConfig{
				Benchmarks: []api.EvaluationBenchmarkConfig{
					{Ref: api.Ref{ID: "bench-1"}, ProviderID: "provider-1"},
				},
			},
		},
	}
	runtime := &logsRuntime{logs: "ignored"}
	h := handlers.New(storage, validation.NewValidator(), runtime, nil, nil)
	rec := httptest.NewRecorder()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-4", logger, "test-user", "test-tenant")
	req := &logsRequest{
		MockRequest: createMockRequest(http.MethodGet, "/api/v1/evaluations/jobs/"+jobID+"/logs?since_seconds="),
		pathValues:  map[string]string{constants.PATH_PARAMETER_JOB_ID: jobID},
		queryValues: map[string][]string{"since_seconds": {""}},
	}

	h.HandleGetEvaluationJobLogs(ctx, req, MockResponseWrapper{recorder: rec})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if runtime.getLogsCalled {
		t.Fatal("expected GetEvaluationLogs not to be called")
	}
}
