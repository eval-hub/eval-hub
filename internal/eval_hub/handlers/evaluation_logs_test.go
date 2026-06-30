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
	logs string
	err  error
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
	_ *int,
	_ api.EvaluationLogOptions,
) (string, error) {
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
	h := handlers.New(storage, validation.NewValidator(), &logsRuntime{logs: "hello logs"}, nil, nil)
	rec := httptest.NewRecorder()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, "test-user", "test-tenant")
	req := &logsRequest{
		MockRequest: createMockRequest(http.MethodGet, "/api/v1/evaluations/jobs/"+jobID+"/logs"),
		pathValues:  map[string]string{constants.PATH_PARAMETER_JOB_ID: jobID},
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
}

func TestHandleGetEvaluationBenchmarkLogs(t *testing.T) {
	jobID := "job-logs-bench"
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
	h := handlers.New(storage, validation.NewValidator(), &logsRuntime{logs: "bench log"}, nil, nil)
	rec := httptest.NewRecorder()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-2", logger, "test-user", "test-tenant")
	req := &logsRequest{
		MockRequest: createMockRequest(http.MethodGet, "/api/v1/evaluations/jobs/"+jobID+"/benchmarks/0/logs"),
		pathValues: map[string]string{
			constants.PATH_PARAMETER_JOB_ID:          jobID,
			constants.PATH_PARAMETER_BENCHMARK_INDEX: "0",
		},
	}

	h.HandleGetEvaluationBenchmarkLogs(ctx, req, MockResponseWrapper{recorder: rec})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if body := strings.TrimSpace(rec.Body.String()); body != "bench log" {
		t.Fatalf("body = %q, want %q", body, "bench log")
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
	h := handlers.New(storage, validation.NewValidator(), &logsRuntime{logs: "ignored"}, nil, nil)
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
}
