package handlers_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_hub/constants"
	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/handlers"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/internal/testhelpers"
	"github.com/eval-hub/eval-hub/pkg/api"
)

type logsRuntime struct {
	logs                   string
	err                    error
	streamLogsCalled       bool
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
func (r *logsRuntime) NotifyJobPhaseTransition(_ context.Context, _ *api.EvaluationJobResource, _ int, _ api.State) {
}
func (r *logsRuntime) NotifyThresholdViolation(_ context.Context, _ *api.EvaluationJobResource, _ int, _ string, _, _ float32) {
}
func (r *logsRuntime) StreamEvaluationLogs(
	_ *api.EvaluationJobResource,
	_ []api.EvaluationBenchmarkConfig,
	benchmarkIndex *int,
	opts api.EvaluationLogOptions,
	w io.Writer,
) error {
	r.streamLogsCalled = true
	r.capturedBenchmarkIndex = benchmarkIndex
	r.capturedOpts = opts
	if r.err != nil {
		return r.err
	}
	if r.logs != "" {
		_, err := io.WriteString(w, r.logs)
		if err != nil {
			return err
		}
	}
	return nil
}
func (r *logsRuntime) ValidateHardwareProfiles(_ []api.EvaluationBenchmarkConfig) error {
	return nil
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
	h := handlers.New(storage, testhelpers.NewValidator(t), runtime, nil, nil, nil)
	rec := httptest.NewRecorder()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, "test-user", "test-tenant")
	req := &logsRequest{
		MockRequest: createMockRequest(http.MethodGet, "/api/v1/evaluations/jobs/"+jobID+"/logs"),
		pathValues:  map[string]string{constants.PathParameterJobID: jobID},
		queryValues: map[string][]string{
			"tail_lines":    {"500"},
			"timestamps":    {"true"},
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
	if !runtime.streamLogsCalled {
		t.Fatal("expected StreamEvaluationLogs to be called")
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
	h := handlers.New(storage, testhelpers.NewValidator(t), runtime, nil, nil, nil)
	rec := httptest.NewRecorder()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-2", logger, "test-user", "test-tenant")
	req := &logsRequest{
		MockRequest: createMockRequest(http.MethodGet, "/api/v1/evaluations/jobs/"+jobID+"/benchmarks/0/logs"),
		pathValues: map[string]string{
			constants.PathParameterJobID:          jobID,
			constants.PathParameterBenchmarkIndex: "0",
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
	h := handlers.New(storage, testhelpers.NewValidator(t), runtime, nil, nil, nil)
	rec := httptest.NewRecorder()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-3", logger, "test-user", "test-tenant")
	req := &logsRequest{
		MockRequest: createMockRequest(http.MethodGet, "/api/v1/evaluations/jobs/"+jobID+"/logs?tail_lines=0"),
		pathValues:  map[string]string{constants.PathParameterJobID: jobID},
		queryValues: map[string][]string{"tail_lines": {"0"}},
	}

	h.HandleGetEvaluationJobLogs(ctx, req, MockResponseWrapper{recorder: rec})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if runtime.streamLogsCalled {
		t.Fatal("expected StreamEvaluationLogs not to be called")
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
	h := handlers.New(storage, testhelpers.NewValidator(t), runtime, nil, nil, nil)
	rec := httptest.NewRecorder()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-4", logger, "test-user", "test-tenant")
	req := &logsRequest{
		MockRequest: createMockRequest(http.MethodGet, "/api/v1/evaluations/jobs/"+jobID+"/logs?since_seconds="),
		pathValues:  map[string]string{constants.PathParameterJobID: jobID},
		queryValues: map[string][]string{"since_seconds": {""}},
	}

	h.HandleGetEvaluationJobLogs(ctx, req, MockResponseWrapper{recorder: rec})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if runtime.streamLogsCalled {
		t.Fatal("expected StreamEvaluationLogs not to be called")
	}
}

func TestHandleGetEvaluationJobLogsMissingJobID(t *testing.T) {
	h := handlers.New(&fakeStorage{}, testhelpers.NewValidator(t), &logsRuntime{}, nil, nil, nil)
	rec := httptest.NewRecorder()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-5", logger, "test-user", "test-tenant")
	req := &logsRequest{
		MockRequest: createMockRequest(http.MethodGet, "/api/v1/evaluations/jobs//logs"),
		pathValues:  map[string]string{},
	}

	h.HandleGetEvaluationJobLogs(ctx, req, MockResponseWrapper{recorder: rec})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandleGetEvaluationBenchmarkLogsMissingBenchmarkIndex(t *testing.T) {
	h := handlers.New(&fakeStorage{}, testhelpers.NewValidator(t), &logsRuntime{}, nil, nil, nil)
	rec := httptest.NewRecorder()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-6", logger, "test-user", "test-tenant")
	req := &logsRequest{
		MockRequest: createMockRequest(http.MethodGet, "/api/v1/evaluations/jobs/job-1/benchmarks//logs"),
		pathValues:  map[string]string{constants.PathParameterJobID: "job-1"},
	}

	h.HandleGetEvaluationBenchmarkLogs(ctx, req, MockResponseWrapper{recorder: rec})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandleGetEvaluationBenchmarkLogsInvalidBenchmarkIndex(t *testing.T) {
	h := handlers.New(&fakeStorage{}, testhelpers.NewValidator(t), &logsRuntime{}, nil, nil, nil)
	rec := httptest.NewRecorder()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-7", logger, "test-user", "test-tenant")
	req := &logsRequest{
		MockRequest: createMockRequest(http.MethodGet, "/api/v1/evaluations/jobs/job-1/benchmarks/abc/logs"),
		pathValues: map[string]string{
			constants.PathParameterJobID:          "job-1",
			constants.PathParameterBenchmarkIndex: "abc",
		},
	}

	h.HandleGetEvaluationBenchmarkLogs(ctx, req, MockResponseWrapper{recorder: rec})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandleGetEvaluationJobLogsNoRuntime(t *testing.T) {
	jobID := "job-logs-no-runtime"
	storage := &fakeStorage{
		job: &api.EvaluationJobResource{
			Resource: api.EvaluationResource{Resource: api.Resource{ID: jobID}},
		},
	}
	h := handlers.New(storage, testhelpers.NewValidator(t), nil, nil, nil, nil)
	rec := httptest.NewRecorder()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-8", logger, "test-user", "test-tenant")
	req := &logsRequest{
		MockRequest: createMockRequest(http.MethodGet, "/api/v1/evaluations/jobs/"+jobID+"/logs"),
		pathValues:  map[string]string{constants.PathParameterJobID: jobID},
	}

	h.HandleGetEvaluationJobLogs(ctx, req, MockResponseWrapper{recorder: rec})

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestHandleGetEvaluationJobLogsRuntimeError(t *testing.T) {
	jobID := "job-logs-runtime-err"
	runtime := &logsRuntime{err: errors.New("runtime failed")}
	storage := &fakeStorage{
		job: &api.EvaluationJobResource{
			Resource: api.EvaluationResource{Resource: api.Resource{ID: jobID}},
			EvaluationJobConfig: api.EvaluationJobConfig{
				Benchmarks: []api.EvaluationBenchmarkConfig{
					{Ref: api.Ref{ID: "bench-1"}, ProviderID: "provider-1"},
				},
			},
		},
	}
	h := handlers.New(storage, testhelpers.NewValidator(t), runtime, nil, nil, nil)
	rec := httptest.NewRecorder()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-9", logger, "test-user", "test-tenant")
	req := &logsRequest{
		MockRequest: createMockRequest(http.MethodGet, "/api/v1/evaluations/jobs/"+jobID+"/logs"),
		pathValues:  map[string]string{constants.PathParameterJobID: jobID},
	}

	h.HandleGetEvaluationJobLogs(ctx, req, MockResponseWrapper{recorder: rec})

	// With streaming, the 200 status is committed before the runtime is called.
	// Runtime errors mid-stream cannot change the status code.
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (streaming commits status before runtime call)", rec.Code)
	}
}

func TestHandleGetEvaluationJobLogsRejectsTailLinesOverMax(t *testing.T) {
	jobID := "job-logs-tail-max"
	storage := &fakeStorage{
		job: &api.EvaluationJobResource{
			Resource: api.EvaluationResource{Resource: api.Resource{ID: jobID}},
		},
	}
	runtime := &logsRuntime{}
	h := handlers.New(storage, testhelpers.NewValidator(t), runtime, nil, nil, nil)
	rec := httptest.NewRecorder()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-10", logger, "test-user", "test-tenant")
	req := &logsRequest{
		MockRequest: createMockRequest(http.MethodGet, "/api/v1/evaluations/jobs/"+jobID+"/logs"),
		pathValues:  map[string]string{constants.PathParameterJobID: jobID},
		queryValues: map[string][]string{"tail_lines": {strconv.Itoa(api.MaxLogTailLines + 1)}},
	}

	h.HandleGetEvaluationJobLogs(ctx, req, MockResponseWrapper{recorder: rec})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandleGetEvaluationJobLogsAcceptsMinusOneForAllLines(t *testing.T) {
	jobID := "job-logs-all"
	runtime := &logsRuntime{logs: "all the logs"}
	storage := &fakeStorage{
		job: &api.EvaluationJobResource{
			Resource: api.EvaluationResource{Resource: api.Resource{ID: jobID}},
			EvaluationJobConfig: api.EvaluationJobConfig{
				Benchmarks: []api.EvaluationBenchmarkConfig{
					{Ref: api.Ref{ID: "bench-1"}, ProviderID: "provider-1"},
				},
			},
		},
	}
	h := handlers.New(storage, testhelpers.NewValidator(t), runtime, nil, nil, nil)
	rec := httptest.NewRecorder()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-all", logger, "test-user", "test-tenant")
	req := &logsRequest{
		MockRequest: createMockRequest(http.MethodGet, "/api/v1/evaluations/jobs/"+jobID+"/logs"),
		pathValues:  map[string]string{constants.PathParameterJobID: jobID},
		queryValues: map[string][]string{"tail_lines": {"-1"}},
	}

	h.HandleGetEvaluationJobLogs(ctx, req, MockResponseWrapper{recorder: rec})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if body := strings.TrimSpace(rec.Body.String()); body != "all the logs" {
		t.Fatalf("body = %q, want %q", body, "all the logs")
	}
	if runtime.capturedOpts.TailLines != api.AllLogLines {
		t.Fatalf("tail_lines = %d, want %d", runtime.capturedOpts.TailLines, api.AllLogLines)
	}
}

func TestHandleGetEvaluationJobLogsRejectsMinusTwo(t *testing.T) {
	jobID := "job-logs-minus2"
	storage := &fakeStorage{
		job: &api.EvaluationJobResource{
			Resource: api.EvaluationResource{Resource: api.Resource{ID: jobID}},
		},
	}
	runtime := &logsRuntime{}
	h := handlers.New(storage, testhelpers.NewValidator(t), runtime, nil, nil, nil)
	rec := httptest.NewRecorder()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-m2", logger, "test-user", "test-tenant")
	req := &logsRequest{
		MockRequest: createMockRequest(http.MethodGet, "/api/v1/evaluations/jobs/"+jobID+"/logs"),
		pathValues:  map[string]string{constants.PathParameterJobID: jobID},
		queryValues: map[string][]string{"tail_lines": {"-2"}},
	}

	h.HandleGetEvaluationJobLogs(ctx, req, MockResponseWrapper{recorder: rec})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandleGetEvaluationJobLogsRejectsNonPositiveSinceSeconds(t *testing.T) {
	jobID := "job-logs-since-zero"
	storage := &fakeStorage{
		job: &api.EvaluationJobResource{
			Resource: api.EvaluationResource{Resource: api.Resource{ID: jobID}},
		},
	}
	runtime := &logsRuntime{}
	h := handlers.New(storage, testhelpers.NewValidator(t), runtime, nil, nil, nil)
	rec := httptest.NewRecorder()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-11", logger, "test-user", "test-tenant")
	req := &logsRequest{
		MockRequest: createMockRequest(http.MethodGet, "/api/v1/evaluations/jobs/"+jobID+"/logs"),
		pathValues:  map[string]string{constants.PathParameterJobID: jobID},
		queryValues: map[string][]string{"since_seconds": {"0"}},
	}

	h.HandleGetEvaluationJobLogs(ctx, req, MockResponseWrapper{recorder: rec})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandleGetEvaluationJobLogsResolvesCollectionBenchmarks(t *testing.T) {
	jobID := "job-logs-collection"
	collectionID := "coll-1"
	runtime := &logsRuntime{logs: "collection logs"}
	storage := &logsCollectionStorage{
		fakeStorage: fakeStorage{
			job: &api.EvaluationJobResource{
				Resource: api.EvaluationResource{Resource: api.Resource{ID: jobID}},
				EvaluationJobConfig: api.EvaluationJobConfig{
					Collection: &api.CollectionRef{ID: collectionID},
				},
			},
			collectionConfigs: map[string]api.CollectionResource{
				collectionID: {
					Resource: api.Resource{ID: collectionID},
					CollectionConfig: api.CollectionConfig{
						Benchmarks: []api.CollectionBenchmarkConfig{
							{Ref: api.Ref{ID: "bench-1"}, ProviderID: "provider-1"},
						},
					},
				},
			},
		},
	}
	h := handlers.New(storage, testhelpers.NewValidator(t), runtime, nil, nil, nil)
	rec := httptest.NewRecorder()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-12", logger, "test-user", "test-tenant")
	req := &logsRequest{
		MockRequest: createMockRequest(http.MethodGet, "/api/v1/evaluations/jobs/"+jobID+"/logs"),
		pathValues:  map[string]string{constants.PathParameterJobID: jobID},
	}

	h.HandleGetEvaluationJobLogs(ctx, req, MockResponseWrapper{recorder: rec})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !runtime.streamLogsCalled {
		t.Fatal("expected StreamEvaluationLogs to be called")
	}
}

func TestHandleGetEvaluationJobLogsJobNotFound(t *testing.T) {
	jobID := "missing-job"
	storage := &logsJobStorage{
		fakeStorage: fakeStorage{},
		getJobErr:   serviceerrors.NewServiceError(messages.ResourceNotFound, "Type", "evaluation job", "ResourceId", jobID),
	}
	h := handlers.New(storage, testhelpers.NewValidator(t), &logsRuntime{}, nil, nil, nil)
	rec := httptest.NewRecorder()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-13", logger, "test-user", "test-tenant")
	req := &logsRequest{
		MockRequest: createMockRequest(http.MethodGet, "/api/v1/evaluations/jobs/"+jobID+"/logs"),
		pathValues:  map[string]string{constants.PathParameterJobID: jobID},
	}

	h.HandleGetEvaluationJobLogs(ctx, req, MockResponseWrapper{recorder: rec})

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestHandleGetEvaluationJobLogsCollectionNotFound(t *testing.T) {
	jobID := "job-logs-missing-collection"
	storage := &logsCollectionStorage{
		fakeStorage: fakeStorage{
			job: &api.EvaluationJobResource{
				Resource: api.EvaluationResource{Resource: api.Resource{ID: jobID}},
				EvaluationJobConfig: api.EvaluationJobConfig{
					Collection: &api.CollectionRef{ID: "missing-coll"},
				},
			},
		},
	}
	h := handlers.New(storage, testhelpers.NewValidator(t), &logsRuntime{}, nil, nil, nil)
	rec := httptest.NewRecorder()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-14", logger, "test-user", "test-tenant")
	req := &logsRequest{
		MockRequest: createMockRequest(http.MethodGet, "/api/v1/evaluations/jobs/"+jobID+"/logs"),
		pathValues:  map[string]string{constants.PathParameterJobID: jobID},
	}

	h.HandleGetEvaluationJobLogs(ctx, req, MockResponseWrapper{recorder: rec})

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestHandleGetEvaluationJobLogsRejectsInvalidSinceSeconds(t *testing.T) {
	jobID := "job-logs-bad-since"
	storage := &fakeStorage{
		job: &api.EvaluationJobResource{
			Resource: api.EvaluationResource{Resource: api.Resource{ID: jobID}},
		},
	}
	runtime := &logsRuntime{}
	h := handlers.New(storage, testhelpers.NewValidator(t), runtime, nil, nil, nil)
	rec := httptest.NewRecorder()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-15", logger, "test-user", "test-tenant")
	req := &logsRequest{
		MockRequest: createMockRequest(http.MethodGet, "/api/v1/evaluations/jobs/"+jobID+"/logs"),
		pathValues:  map[string]string{constants.PathParameterJobID: jobID},
		queryValues: map[string][]string{"since_seconds": {"abc"}},
	}

	h.HandleGetEvaluationJobLogs(ctx, req, MockResponseWrapper{recorder: rec})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandleGetEvaluationJobLogsRejectsInvalidTimestamps(t *testing.T) {
	jobID := "job-logs-bad-timestamps"
	storage := &fakeStorage{
		job: &api.EvaluationJobResource{
			Resource: api.EvaluationResource{Resource: api.Resource{ID: jobID}},
		},
	}
	runtime := &logsRuntime{}
	h := handlers.New(storage, testhelpers.NewValidator(t), runtime, nil, nil, nil)
	rec := httptest.NewRecorder()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-16", logger, "test-user", "test-tenant")
	req := &logsRequest{
		MockRequest: createMockRequest(http.MethodGet, "/api/v1/evaluations/jobs/"+jobID+"/logs"),
		pathValues:  map[string]string{constants.PathParameterJobID: jobID},
		queryValues: map[string][]string{"timestamps": {"not-a-bool"}},
	}

	h.HandleGetEvaluationJobLogs(ctx, req, MockResponseWrapper{recorder: rec})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandleGetEvaluationJobLogsTruncationSetsHeader(t *testing.T) {
	jobID := "job-logs-truncated"
	runtime := &logsRuntime{err: handlers.ErrLogResponseTruncated}
	storage := &fakeStorage{
		job: &api.EvaluationJobResource{
			Resource: api.EvaluationResource{Resource: api.Resource{ID: jobID}},
			EvaluationJobConfig: api.EvaluationJobConfig{
				Benchmarks: []api.EvaluationBenchmarkConfig{
					{Ref: api.Ref{ID: "bench-1"}, ProviderID: "provider-1"},
				},
			},
		},
	}
	h := handlers.New(storage, testhelpers.NewValidator(t), runtime, nil, nil, nil)
	rec := httptest.NewRecorder()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-trunc", logger, "test-user", "test-tenant")
	req := &logsRequest{
		MockRequest: createMockRequest(http.MethodGet, "/api/v1/evaluations/jobs/"+jobID+"/logs"),
		pathValues:  map[string]string{constants.PathParameterJobID: jobID},
	}

	h.HandleGetEvaluationJobLogs(ctx, req, MockResponseWrapper{recorder: rec})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("X-Log-Truncated"); got != "true" {
		t.Fatalf("X-Log-Truncated = %q, want %q", got, "true")
	}
}

func TestHandleGetEvaluationJobLogsTruncationWithMaxBytes(t *testing.T) {
	jobID := "job-logs-maxbytes"
	runtime := &logsRuntime{logs: strings.Repeat("x", 100)}
	storage := &fakeStorage{
		job: &api.EvaluationJobResource{
			Resource: api.EvaluationResource{Resource: api.Resource{ID: jobID}},
			EvaluationJobConfig: api.EvaluationJobConfig{
				Benchmarks: []api.EvaluationBenchmarkConfig{
					{Ref: api.Ref{ID: "bench-1"}, ProviderID: "provider-1"},
				},
			},
		},
	}
	cfg := &config.Config{
		Service: &config.ServiceConfig{
			MaxLogResponseBytes: 10,
		},
	}
	h := handlers.New(storage, testhelpers.NewValidator(t), runtime, nil, cfg, nil)
	rec := httptest.NewRecorder()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-maxbytes", logger, "test-user", "test-tenant")
	req := &logsRequest{
		MockRequest: createMockRequest(http.MethodGet, "/api/v1/evaluations/jobs/"+jobID+"/logs"),
		pathValues:  map[string]string{constants.PathParameterJobID: jobID},
	}

	h.HandleGetEvaluationJobLogs(ctx, req, MockResponseWrapper{recorder: rec})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Trailer"); got != "X-Log-Truncated" {
		t.Fatalf("Trailer = %q, want %q", got, "X-Log-Truncated")
	}
	if got := rec.Header().Get("X-Log-Truncated"); got != "true" {
		t.Fatalf("X-Log-Truncated = %q, want %q", got, "true")
	}
	if body := rec.Body.String(); len(body) > 10 {
		t.Fatalf("body length = %d, want <= 10", len(body))
	}
}

func TestHandleGetEvaluationJobLogsWithMaxLogBytesConfig(t *testing.T) {
	jobID := "job-logs-config"
	runtime := &logsRuntime{logs: "hello"}
	storage := &fakeStorage{
		job: &api.EvaluationJobResource{
			Resource: api.EvaluationResource{Resource: api.Resource{ID: jobID}},
			EvaluationJobConfig: api.EvaluationJobConfig{
				Benchmarks: []api.EvaluationBenchmarkConfig{
					{Ref: api.Ref{ID: "bench-1"}, ProviderID: "provider-1"},
				},
			},
		},
	}
	cfg := &config.Config{
		Service: &config.ServiceConfig{MaxLogResponseBytes: 1024},
	}
	h := handlers.New(storage, testhelpers.NewValidator(t), runtime, nil, cfg, nil)
	rec := httptest.NewRecorder()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-cfg", logger, "test-user", "test-tenant")
	req := &logsRequest{
		MockRequest: createMockRequest(http.MethodGet, "/api/v1/evaluations/jobs/"+jobID+"/logs"),
		pathValues:  map[string]string{constants.PathParameterJobID: jobID},
	}

	h.HandleGetEvaluationJobLogs(ctx, req, MockResponseWrapper{recorder: rec})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if body := strings.TrimSpace(rec.Body.String()); body != "hello" {
		t.Fatalf("body = %q, want %q", body, "hello")
	}
}

func TestHandleGetEvaluationJobLogsWithConfiguredTimeout(t *testing.T) {
	jobID := "job-logs-timeout"
	runtime := &logsRuntime{logs: "ok"}
	storage := &fakeStorage{
		job: &api.EvaluationJobResource{
			Resource: api.EvaluationResource{Resource: api.Resource{ID: jobID}},
			EvaluationJobConfig: api.EvaluationJobConfig{
				Benchmarks: []api.EvaluationBenchmarkConfig{
					{Ref: api.Ref{ID: "bench-1"}, ProviderID: "provider-1"},
				},
			},
		},
	}
	cfg := &config.Config{
		Service: &config.ServiceConfig{
			LogStreamTimeout:    10 * time.Minute,
			MaxLogResponseBytes: -1,
		},
	}
	h := handlers.New(storage, testhelpers.NewValidator(t), runtime, nil, cfg, nil)
	rec := httptest.NewRecorder()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-timeout", logger, "test-user", "test-tenant")
	req := &logsRequest{
		MockRequest: createMockRequest(http.MethodGet, "/api/v1/evaluations/jobs/"+jobID+"/logs"),
		pathValues:  map[string]string{constants.PathParameterJobID: jobID},
	}

	h.HandleGetEvaluationJobLogs(ctx, req, MockResponseWrapper{recorder: rec})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if body := strings.TrimSpace(rec.Body.String()); body != "ok" {
		t.Fatalf("body = %q, want %q", body, "ok")
	}
}

type logsJobStorage struct {
	fakeStorage
	getJobErr error
}

func (s *logsJobStorage) copy() *logsJobStorage {
	c := *s
	return &c
}

func (s *logsJobStorage) WithLogger(logger *slog.Logger) abstractions.Storage {
	c := s.copy()
	c.fakeStorage = *s.fakeStorage.WithLogger(logger).(*fakeStorage)
	return c
}

func (s *logsJobStorage) WithContext(ctx context.Context) abstractions.Storage {
	c := s.copy()
	c.fakeStorage = *s.fakeStorage.WithContext(ctx).(*fakeStorage)
	return c
}

func (s *logsJobStorage) WithTenant(tenant api.Tenant) abstractions.Storage {
	c := s.copy()
	c.fakeStorage = *s.fakeStorage.WithTenant(tenant).(*fakeStorage)
	return c
}

func (s *logsJobStorage) WithOwner(user api.User) abstractions.Storage {
	c := s.copy()
	c.fakeStorage = *s.fakeStorage.WithOwner(user).(*fakeStorage)
	return c
}

func (s *logsJobStorage) GetEvaluationJob(_ string) (*api.EvaluationJobResource, error) {
	if s.getJobErr != nil {
		return nil, s.getJobErr
	}
	return s.fakeStorage.GetEvaluationJob("")
}

type logsCollectionStorage struct {
	fakeStorage
}

func (s *logsCollectionStorage) copy() *logsCollectionStorage {
	c := *s
	return &c
}

func (s *logsCollectionStorage) WithLogger(logger *slog.Logger) abstractions.Storage {
	c := s.copy()
	c.fakeStorage = *s.fakeStorage.WithLogger(logger).(*fakeStorage)
	return c
}

func (s *logsCollectionStorage) WithContext(ctx context.Context) abstractions.Storage {
	c := s.copy()
	c.fakeStorage = *s.fakeStorage.WithContext(ctx).(*fakeStorage)
	return c
}

func (s *logsCollectionStorage) WithTenant(tenant api.Tenant) abstractions.Storage {
	c := s.copy()
	c.fakeStorage = *s.fakeStorage.WithTenant(tenant).(*fakeStorage)
	return c
}

func (s *logsCollectionStorage) WithOwner(user api.User) abstractions.Storage {
	c := s.copy()
	c.fakeStorage = *s.fakeStorage.WithOwner(user).(*fakeStorage)
	return c
}

func (s *logsCollectionStorage) GetCollection(id string) (*api.CollectionResource, error) {
	if collection, ok := s.collectionConfigs[id]; ok {
		return &collection, nil
	}
	return s.fakeStorage.GetCollection(id)
}
