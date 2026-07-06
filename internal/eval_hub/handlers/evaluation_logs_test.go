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

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/constants"
	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/handlers"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
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

func TestHandleGetEvaluationJobLogsMissingJobID(t *testing.T) {
	h := handlers.New(&fakeStorage{}, validation.NewValidator(), &logsRuntime{}, nil, nil)
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
	h := handlers.New(&fakeStorage{}, validation.NewValidator(), &logsRuntime{}, nil, nil)
	rec := httptest.NewRecorder()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-6", logger, "test-user", "test-tenant")
	req := &logsRequest{
		MockRequest: createMockRequest(http.MethodGet, "/api/v1/evaluations/jobs/job-1/benchmarks//logs"),
		pathValues:  map[string]string{constants.PATH_PARAMETER_JOB_ID: "job-1"},
	}

	h.HandleGetEvaluationBenchmarkLogs(ctx, req, MockResponseWrapper{recorder: rec})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandleGetEvaluationBenchmarkLogsInvalidBenchmarkIndex(t *testing.T) {
	h := handlers.New(&fakeStorage{}, validation.NewValidator(), &logsRuntime{}, nil, nil)
	rec := httptest.NewRecorder()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-7", logger, "test-user", "test-tenant")
	req := &logsRequest{
		MockRequest: createMockRequest(http.MethodGet, "/api/v1/evaluations/jobs/job-1/benchmarks/abc/logs"),
		pathValues: map[string]string{
			constants.PATH_PARAMETER_JOB_ID:          "job-1",
			constants.PATH_PARAMETER_BENCHMARK_INDEX: "abc",
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
	h := handlers.New(storage, validation.NewValidator(), nil, nil, nil)
	rec := httptest.NewRecorder()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-8", logger, "test-user", "test-tenant")
	req := &logsRequest{
		MockRequest: createMockRequest(http.MethodGet, "/api/v1/evaluations/jobs/"+jobID+"/logs"),
		pathValues:  map[string]string{constants.PATH_PARAMETER_JOB_ID: jobID},
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
	h := handlers.New(storage, validation.NewValidator(), runtime, nil, nil)
	rec := httptest.NewRecorder()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-9", logger, "test-user", "test-tenant")
	req := &logsRequest{
		MockRequest: createMockRequest(http.MethodGet, "/api/v1/evaluations/jobs/"+jobID+"/logs"),
		pathValues:  map[string]string{constants.PATH_PARAMETER_JOB_ID: jobID},
	}

	h.HandleGetEvaluationJobLogs(ctx, req, MockResponseWrapper{recorder: rec})

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
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
	h := handlers.New(storage, validation.NewValidator(), runtime, nil, nil)
	rec := httptest.NewRecorder()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-10", logger, "test-user", "test-tenant")
	req := &logsRequest{
		MockRequest: createMockRequest(http.MethodGet, "/api/v1/evaluations/jobs/"+jobID+"/logs"),
		pathValues:  map[string]string{constants.PATH_PARAMETER_JOB_ID: jobID},
		queryValues: map[string][]string{"tail_lines": {strconv.Itoa(api.MaxLogTailLines + 1)}},
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
	h := handlers.New(storage, validation.NewValidator(), runtime, nil, nil)
	rec := httptest.NewRecorder()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-11", logger, "test-user", "test-tenant")
	req := &logsRequest{
		MockRequest: createMockRequest(http.MethodGet, "/api/v1/evaluations/jobs/"+jobID+"/logs"),
		pathValues:  map[string]string{constants.PATH_PARAMETER_JOB_ID: jobID},
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
	h := handlers.New(storage, validation.NewValidator(), runtime, nil, nil)
	rec := httptest.NewRecorder()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-12", logger, "test-user", "test-tenant")
	req := &logsRequest{
		MockRequest: createMockRequest(http.MethodGet, "/api/v1/evaluations/jobs/"+jobID+"/logs"),
		pathValues:  map[string]string{constants.PATH_PARAMETER_JOB_ID: jobID},
	}

	h.HandleGetEvaluationJobLogs(ctx, req, MockResponseWrapper{recorder: rec})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !runtime.getLogsCalled {
		t.Fatal("expected GetEvaluationLogs to be called")
	}
}

func TestHandleGetEvaluationJobLogsJobNotFound(t *testing.T) {
	jobID := "missing-job"
	storage := &logsJobStorage{
		fakeStorage: fakeStorage{},
		getJobErr:   serviceerrors.NewServiceError(messages.ResourceNotFound, "Type", "evaluation job", "ResourceId", jobID),
	}
	h := handlers.New(storage, validation.NewValidator(), &logsRuntime{}, nil, nil)
	rec := httptest.NewRecorder()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-13", logger, "test-user", "test-tenant")
	req := &logsRequest{
		MockRequest: createMockRequest(http.MethodGet, "/api/v1/evaluations/jobs/"+jobID+"/logs"),
		pathValues:  map[string]string{constants.PATH_PARAMETER_JOB_ID: jobID},
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
	h := handlers.New(storage, validation.NewValidator(), &logsRuntime{}, nil, nil)
	rec := httptest.NewRecorder()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-14", logger, "test-user", "test-tenant")
	req := &logsRequest{
		MockRequest: createMockRequest(http.MethodGet, "/api/v1/evaluations/jobs/"+jobID+"/logs"),
		pathValues:  map[string]string{constants.PATH_PARAMETER_JOB_ID: jobID},
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
	h := handlers.New(storage, validation.NewValidator(), runtime, nil, nil)
	rec := httptest.NewRecorder()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-15", logger, "test-user", "test-tenant")
	req := &logsRequest{
		MockRequest: createMockRequest(http.MethodGet, "/api/v1/evaluations/jobs/"+jobID+"/logs"),
		pathValues:  map[string]string{constants.PATH_PARAMETER_JOB_ID: jobID},
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
	h := handlers.New(storage, validation.NewValidator(), runtime, nil, nil)
	rec := httptest.NewRecorder()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-16", logger, "test-user", "test-tenant")
	req := &logsRequest{
		MockRequest: createMockRequest(http.MethodGet, "/api/v1/evaluations/jobs/"+jobID+"/logs"),
		pathValues:  map[string]string{constants.PATH_PARAMETER_JOB_ID: jobID},
		queryValues: map[string][]string{"timestamps": {"not-a-bool"}},
	}

	h.HandleGetEvaluationJobLogs(ctx, req, MockResponseWrapper{recorder: rec})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
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
