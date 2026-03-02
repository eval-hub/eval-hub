package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/constants"
	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"github.com/eval-hub/eval-hub/internal/handlers"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/go-playground/validator/v10"
)

type bodyRequest struct {
	*MockRequest
	body    []byte
	bodyErr error
}

func (r *bodyRequest) BodyAsBytes() ([]byte, error) {
	if r.bodyErr != nil {
		return nil, r.bodyErr
	}
	return r.body, nil
}

type fakeStorage struct {
	abstractions.Storage
	lastStatusID string
	lastStatus   api.OverallState
	job          *api.EvaluationJobResource
	deleteID     string
}

func (f *fakeStorage) WithLogger(_ *slog.Logger) abstractions.Storage { return f }
func (f *fakeStorage) WithContext(_ context.Context) abstractions.Storage {
	return f
}
func (f *fakeStorage) WithTenant(_ api.Tenant) abstractions.Storage {
	return f
}

func (f *fakeStorage) CreateEvaluationJob(_ *api.EvaluationJobResource) error {
	return nil
}

func (f *fakeStorage) UpdateEvaluationJobStatus(id string, state api.OverallState, message *api.MessageInfo) error {
	f.lastStatusID = id
	f.lastStatus = state
	return nil
}

func (f *fakeStorage) GetEvaluationJob(_ string) (*api.EvaluationJobResource, error) {
	return f.job, nil
}

func (f *fakeStorage) GetEvaluationJobs(_ *abstractions.QueryFilter) (*abstractions.QueryResults[api.EvaluationJobResource], error) {
	return &abstractions.QueryResults[api.EvaluationJobResource]{Items: []api.EvaluationJobResource{}, TotalStored: 0}, nil
}

func (f *fakeStorage) UpdateEvaluationJob(_ string, _ *api.StatusEvent) error {
	return nil
}

func (f *fakeStorage) DeleteEvaluationJob(id string) error {
	f.deleteID = id
	return nil
}

type fakeRuntime struct {
	err    error
	called bool
}

func (r *fakeRuntime) WithLogger(_ *slog.Logger) abstractions.Runtime { return r }
func (r *fakeRuntime) WithContext(_ context.Context) abstractions.Runtime {
	return r
}
func (r *fakeRuntime) Name() string { return "fake" }
func (r *fakeRuntime) RunEvaluationJob(_ *api.EvaluationJobResource, _ *abstractions.Storage) error {
	r.called = true
	return r.err
}
func (r *fakeRuntime) DeleteEvaluationJobResources(_ *api.EvaluationJobResource) error {
	r.called = true
	return r.err
}

type listEvaluationsRequest struct {
	*MockRequest
	queryValues map[string][]string
	pathValues  map[string]string
}

func (r *listEvaluationsRequest) Query(key string) []string {
	if values, ok := r.queryValues[key]; ok {
		return values
	}
	return []string{}
}

func (r *listEvaluationsRequest) PathValue(name string) string {
	return r.pathValues[name]
}

type listEvaluationsStorage struct {
	*fakeStorage
	jobs []api.EvaluationJobResource
	err  error
}

func (s *listEvaluationsStorage) WithLogger(_ *slog.Logger) abstractions.Storage { return s }
func (s *listEvaluationsStorage) WithContext(_ context.Context) abstractions.Storage {
	return s
}
func (s *listEvaluationsStorage) WithTenant(_ api.Tenant) abstractions.Storage { return s }

func (s *listEvaluationsStorage) GetEvaluationJobs(_ *abstractions.QueryFilter) (*abstractions.QueryResults[api.EvaluationJobResource], error) {
	if s.err != nil {
		return nil, s.err
	}
	return &abstractions.QueryResults[api.EvaluationJobResource]{
		Items:       s.jobs,
		TotalStored: len(s.jobs),
	}, nil
}

type updateEvaluationStorage struct {
	*fakeStorage
	updateErr error
}

func (s *updateEvaluationStorage) WithLogger(_ *slog.Logger) abstractions.Storage { return s }
func (s *updateEvaluationStorage) WithContext(_ context.Context) abstractions.Storage {
	return s
}
func (s *updateEvaluationStorage) WithTenant(_ api.Tenant) abstractions.Storage { return s }

func (s *updateEvaluationStorage) UpdateEvaluationJob(_ string, _ *api.StatusEvent) error {
	return s.updateErr
}

type deleteRequest struct {
	*MockRequest
	queryValues map[string][]string
	pathValues  map[string]string
}

func (r *deleteRequest) Query(key string) []string {
	if values, ok := r.queryValues[key]; ok {
		return values
	}
	return []string{}
}

func (r *deleteRequest) PathValue(name string) string {
	return r.pathValues[name]
}

func TestHandleCreateEvaluationMarksFailedWhenRuntimeErrors(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	// note that the fake storage only implements the functions that are used in this test
	storage := &fakeStorage{}
	runtime := &fakeRuntime{err: errors.New("runtime failed")}
	validate := validator.New()
	providerConfigs := map[string]api.ProviderResource{
		"garak": {
			Resource: api.Resource{ID: "garak"},
			ProviderConfig: api.ProviderConfig{
				Benchmarks: []api.BenchmarkResource{
					{ID: "bench-1"},
				},
			},
		},
	}

	h := handlers.New(storage, validate, runtime, nil, providerConfigs, nil)
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, time.Second, "test-user", "test-tenant")

	req := &bodyRequest{
		MockRequest: createMockRequest("POST", "/api/v1/evaluations/jobs"),
		body:        []byte(`{"model":{"url":"http://test.com","name":"test"},"benchmarks":[{"id":"bench-1","provider_id":"garak"}]}`),
	}
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}

	h.HandleCreateEvaluation(ctx, req, resp)

	if !runtime.called {
		t.Fatalf("expected runtime to be invoked")
	}
	if storage.lastStatus == "" || storage.lastStatusID == "" {
		t.Fatalf("expected evaluation status update to be recorded")
	}
	if storage.lastStatus != api.OverallStateFailed {
		t.Fatalf("expected failed status update, got %+v", storage.lastStatus)
	}
	if recorder.Code == 202 {
		t.Fatalf("expected non-202 error response, got %d", recorder.Code)
	}
	if recorder.Code == 0 {
		t.Fatalf("expected response code to be set")
	}
}

func TestHandleCreateEvaluationSucceedsWhenRuntimeOk(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	storage := &fakeStorage{}
	runtime := &fakeRuntime{}
	validate := validator.New()
	providerConfigs := map[string]api.ProviderResource{
		"garak": {
			Resource: api.Resource{ID: "garak"},
			ProviderConfig: api.ProviderConfig{
				Benchmarks: []api.BenchmarkResource{
					{ID: "bench-1"},
				},
			},
		},
	}
	h := handlers.New(storage, validate, runtime, nil, providerConfigs, nil)
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-2", logger, time.Second, "test-user", "test-tenant")

	req := &bodyRequest{
		MockRequest: createMockRequest("POST", "/api/v1/evaluations/jobs"),
		body:        []byte(`{"model":{"url":"http://test.com","name":"test"},"benchmarks":[{"id":"bench-1","provider_id":"garak"}]}`),
	}
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}

	h.HandleCreateEvaluation(ctx, req, resp)

	if !runtime.called {
		t.Fatalf("expected runtime to be invoked")
	}
	if storage.lastStatus != "" {
		t.Fatalf("did not expect evaluation status update on success")
	}
	if recorder.Code != 202 {
		t.Fatalf("expected status 202, got %d", recorder.Code)
	}
}

func TestHandleCancelEvaluationWithSoftDeleteDoesNotCleanupResources(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	jobID := "job-1"
	storage := &fakeStorage{
		job: &api.EvaluationJobResource{
			Resource: api.EvaluationResource{
				Resource: api.Resource{ID: jobID},
			},
		},
	}
	runtime := &fakeRuntime{}
	validate := validator.New()
	h := handlers.New(storage, validate, runtime, nil, nil, nil)
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-3", logger, time.Second, "test-user", "test-tenant")

	req := &deleteRequest{
		MockRequest: createMockRequest("DELETE", "/api/v1/evaluations/jobs/"+jobID),
		queryValues: map[string][]string{"hard_delete": {"false"}},
		pathValues:  map[string]string{constants.PATH_PARAMETER_JOB_ID: jobID},
	}
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}

	h.HandleCancelEvaluation(ctx, req, resp)

	if runtime.called {
		t.Fatalf("expected runtime cleanup not to be invoked for soft delete")
	}
	if recorder.Code != 204 {
		t.Fatalf("expected 204 response, got %d", recorder.Code)
	}
}

func TestHandleDeleteEvaluationCleansUpResources(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	jobID := "job-2"
	storage := &fakeStorage{
		job: &api.EvaluationJobResource{
			Resource: api.EvaluationResource{
				Resource: api.Resource{ID: jobID},
			},
			Status: &api.EvaluationJobStatus{
				EvaluationJobState: api.EvaluationJobState{
					State: api.OverallStateRunning,
					Message: &api.MessageInfo{
						Message:     "running",
						MessageCode: "job_running",
					},
				},
			},
		},
	}
	runtime := &fakeRuntime{}
	validate := validator.New()
	h := handlers.New(storage, validate, runtime, nil, nil, nil)
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-4", logger, time.Second, "test-user", "test-tenant")

	req := &deleteRequest{
		MockRequest: createMockRequest("DELETE", "/api/v1/evaluations/jobs/"+jobID),
		queryValues: map[string][]string{"hard_delete": {"true"}},
		pathValues:  map[string]string{constants.PATH_PARAMETER_JOB_ID: jobID},
	}
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}

	h.HandleCancelEvaluation(ctx, req, resp)

	if !runtime.called {
		t.Fatalf("expected runtime cleanup to be invoked for hard delete")
	}
	if storage.deleteID != jobID {
		t.Fatalf("expected delete to be invoked for %s, got %s", jobID, storage.deleteID)
	}
	if recorder.Code != 204 {
		t.Fatalf("expected 204 response, got %d", recorder.Code)
	}
}

func TestHandleCreateEvaluationRejectsMissingBenchmarkID(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	storage := &fakeStorage{}
	runtime := &fakeRuntime{}
	validate := validator.New()
	h := handlers.New(storage, validate, runtime, nil, nil, nil)

	req := &bodyRequest{
		MockRequest: createMockRequest("POST", "/api/v1/evaluations/jobs"),
		body:        []byte(`{"model":{"url":"http://test.com","name":"test"},"benchmarks":[{"provider_id":"garak"}]}`),
	}
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-3", logger, time.Second, "test-user", "test-tenant")
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}

	h.HandleCreateEvaluation(ctx, req, resp)

	if runtime.called {
		t.Fatalf("did not expect runtime to be invoked")
	}
	if recorder.Code != 400 {
		t.Fatalf("expected status 400, got %d", recorder.Code)
	}
}

func TestHandleCreateEvaluationRejectsMissingProviderID(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	storage := &fakeStorage{}
	runtime := &fakeRuntime{}
	validate := validator.New()
	h := handlers.New(storage, validate, runtime, nil, nil, nil)

	req := &bodyRequest{
		MockRequest: createMockRequest("POST", "/api/v1/evaluations/jobs"),
		body:        []byte(`{"model":{"url":"http://test.com","name":"test"},"benchmarks":[{"id":"bench-1"}]}`),
	}
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-4", logger, time.Second, "test-user", "test-tenant")
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}

	h.HandleCreateEvaluation(ctx, req, resp)

	if runtime.called {
		t.Fatalf("did not expect runtime to be invoked")
	}
	if recorder.Code != 400 {
		t.Fatalf("expected status 400, got %d", recorder.Code)
	}
}

func TestHandleCreateEvaluationRejectsInvalidProviderID(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	storage := &fakeStorage{}
	runtime := &fakeRuntime{}
	validate := validator.New()
	providerConfigs := map[string]api.ProviderResource{
		"garak": {
			Resource: api.Resource{ID: "garak"},
			ProviderConfig: api.ProviderConfig{
				Benchmarks: []api.BenchmarkResource{
					{ID: "bench-1"},
				},
			},
		},
	}
	h := handlers.New(storage, validate, runtime, nil, providerConfigs, nil)
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-invalid-provider", logger, time.Second, "test-user", "test-tenant")

	req := &bodyRequest{
		MockRequest: createMockRequest("POST", "/api/v1/evaluations/jobs"),
		body:        []byte(`{"model":{"url":"http://test.com","name":"test"},"benchmarks":[{"id":"bench-1","provider_id":"unknown"}]}`),
	}
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}

	h.HandleCreateEvaluation(ctx, req, resp)

	if recorder.Code != 400 {
		t.Fatalf("expected status 400, got %d", recorder.Code)
	}
}

func TestHandleCreateEvaluationRejectsInvalidBenchmarkID(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	storage := &fakeStorage{}
	runtime := &fakeRuntime{}
	validate := validator.New()
	providerConfigs := map[string]api.ProviderResource{
		"garak": {
			Resource: api.Resource{ID: "garak"},
			ProviderConfig: api.ProviderConfig{
				Benchmarks: []api.BenchmarkResource{
					{ID: "bench-1"},
				},
			},
		},
	}
	h := handlers.New(storage, validate, runtime, nil, providerConfigs, nil)
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-invalid-benchmark", logger, time.Second, "test-user", "test-tenant")

	req := &bodyRequest{
		MockRequest: createMockRequest("POST", "/api/v1/evaluations/jobs"),
		body:        []byte(`{"model":{"url":"http://test.com","name":"test"},"benchmarks":[{"id":"unknown","provider_id":"garak"}]}`),
	}
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}

	h.HandleCreateEvaluation(ctx, req, resp)

	if recorder.Code != 400 {
		t.Fatalf("expected status 400, got %d", recorder.Code)
	}
}

func TestHandleListEvaluations(t *testing.T) {
	storage := &listEvaluationsStorage{
		fakeStorage: &fakeStorage{},
		jobs: []api.EvaluationJobResource{
			{
				Resource: api.EvaluationResource{
					Resource: api.Resource{ID: "job-1"},
				},
			},
		},
	}
	validate := validator.New()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.New(storage, validate, &fakeRuntime{}, nil, nil, nil)

	req := &listEvaluationsRequest{
		MockRequest: createMockRequest("GET", "/api/v1/evaluations/jobs"),
		queryValues: map[string][]string{},
		pathValues:  map[string]string{},
	}
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, time.Second, "test-user", "test-tenant")

	h.HandleListEvaluations(ctx, req, resp)

	if recorder.Code != 200 {
		t.Fatalf("expected status 200, got %d body %s", recorder.Code, recorder.Body.String())
	}
	var got api.EvaluationJobResourceList
	if err := json.NewDecoder(recorder.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.TotalCount != 1 {
		t.Errorf("expected TotalCount 1, got %d", got.TotalCount)
	}
	if len(got.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(got.Items))
	}
	if got.Items[0].Resource.ID != "job-1" {
		t.Errorf("expected id job-1, got %s", got.Items[0].Resource.ID)
	}
}

func TestHandleGetEvaluation(t *testing.T) {
	storage := &fakeStorage{
		job: &api.EvaluationJobResource{
			Resource: api.EvaluationResource{
				Resource: api.Resource{ID: "job-get"},
			},
		},
	}
	validate := validator.New()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.New(storage, validate, &fakeRuntime{}, nil, nil, nil)

	req := &deleteRequest{
		MockRequest: createMockRequest("GET", "/api/v1/evaluations/jobs/job-get"),
		pathValues:  map[string]string{constants.PATH_PARAMETER_JOB_ID: "job-get"},
	}
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, time.Second, "test-user", "test-tenant")

	h.HandleGetEvaluation(ctx, req, resp)

	if recorder.Code != 200 {
		t.Fatalf("expected status 200, got %d body %s", recorder.Code, recorder.Body.String())
	}
	var got api.EvaluationJobResource
	if err := json.NewDecoder(recorder.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Resource.ID != "job-get" {
		t.Errorf("expected id job-get, got %s", got.Resource.ID)
	}
}

func TestHandleGetEvaluation_MissingPathParam(t *testing.T) {
	storage := &fakeStorage{}
	validate := validator.New()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.New(storage, validate, &fakeRuntime{}, nil, nil, nil)

	req := &deleteRequest{
		MockRequest: createMockRequest("GET", "/api/v1/evaluations/jobs/"),
		pathValues:  map[string]string{},
	}
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, time.Second, "test-user", "test-tenant")

	h.HandleGetEvaluation(ctx, req, resp)

	if recorder.Code != 400 {
		t.Fatalf("expected status 400 for missing path param, got %d", recorder.Code)
	}
}

type updateEvaluationRequest struct {
	*bodyRequest
	pathValues map[string]string
}

func (r *updateEvaluationRequest) PathValue(name string) string {
	return r.pathValues[name]
}

func TestHandleUpdateEvaluation(t *testing.T) {
	storage := &updateEvaluationStorage{fakeStorage: &fakeStorage{}}
	validate := validator.New()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.New(storage, validate, &fakeRuntime{}, nil, nil, nil)

	body := `{"benchmark_status_event":{"provider_id":"p1","id":"b1","status":"completed"}}`
	req := &bodyRequest{
		MockRequest: createMockRequest("PUT", "/api/v1/evaluations/jobs/job-update/events"),
		body:        []byte(body),
	}
	reqWithPath := &updateEvaluationRequest{
		bodyRequest: req,
		pathValues:  map[string]string{constants.PATH_PARAMETER_JOB_ID: "job-update"},
	}
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, time.Second, "test-user", "test-tenant")

	h.HandleUpdateEvaluation(ctx, reqWithPath, resp)

	if recorder.Code != 204 {
		t.Fatalf("expected status 204, got %d body %s", recorder.Code, recorder.Body.String())
	}
}
