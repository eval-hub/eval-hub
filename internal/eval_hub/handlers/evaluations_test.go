package handlers_test

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/handlers"
	"github.com/eval-hub/eval-hub/internal/eval_hub/validation"
	"github.com/eval-hub/eval-hub/pkg/api"
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
	lastStatusID      string
	lastStatus        api.OverallState
	job               *api.EvaluationJobResource
	deleteID          string
	providerConfigs   map[string]api.ProviderResource
	collectionConfigs map[string]api.CollectionResource
}

func (f *fakeStorage) WithLogger(_ *slog.Logger) abstractions.Storage { return f }
func (f *fakeStorage) WithContext(_ context.Context) abstractions.Storage {
	return f
}
func (f *fakeStorage) WithTenant(_ api.Tenant) abstractions.Storage {
	return f
}
func (f *fakeStorage) WithOwner(_ api.User) abstractions.Storage {
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
	return &abstractions.QueryResults[api.EvaluationJobResource]{Items: []api.EvaluationJobResource{}, TotalCount: 0}, nil
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
func (r *fakeRuntime) RunEvaluationJob(
	_ *api.EvaluationJobResource,
	_ []api.BenchmarkConfig,
	_ abstractions.RuntimeStorage,
) error {
	r.called = true
	return r.err
}
func (r *fakeRuntime) DeleteEvaluationJobResources(_ *api.EvaluationJobResource) error {
	r.called = true
	return r.err
}
func (r *fakeRuntime) QueueExists(_, _ string) (bool, error) {
	return true, nil
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
func (s *listEvaluationsStorage) WithOwner(_ api.User) abstractions.Storage    { return s }

func (s *listEvaluationsStorage) GetEvaluationJobs(_ *abstractions.QueryFilter) (*abstractions.QueryResults[api.EvaluationJobResource], error) {
	if s.err != nil {
		return nil, s.err
	}
	return &abstractions.QueryResults[api.EvaluationJobResource]{
		Items:      s.jobs,
		TotalCount: len(s.jobs),
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
func (s *updateEvaluationStorage) WithOwner(_ api.User) abstractions.Storage    { return s }

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

func TestResolveProvider_FromMap(t *testing.T) {
	providers := map[string]api.ProviderResource{
		"p1": {Resource: api.Resource{ID: "p1"}},
	}
	storage := &fakeStorage{providerConfigs: providers}
	got, err := storage.GetProvider("p1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got == nil || got.Resource.ID != "p1" {
		t.Fatalf("expected provider p1, got %v", got)
	}
}

func TestResolveProvider_NotFound(t *testing.T) {
	storage := &fakeStorage{}
	got, err := storage.GetProvider("missing")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got != nil {
		t.Fatalf("expected nil provider, got %v", got)
	}
	if !strings.Contains(err.Error(), "provider resource 'missing' was not found") {
		t.Fatalf("expected: provider resource 'missing' was not found, got %q", err.Error())
	}
}

func TestResolveBenchmarks_FromJobBenchmarks(t *testing.T) {
	eval := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-1"}},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Benchmarks: []api.BenchmarkConfig{
				{Ref: api.Ref{ID: "b1"}, ProviderID: "p1"},
			},
		},
	}
	got, err := handlers.ResolveBenchmarks(eval, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(got) != 1 || got[0].ID != "b1" {
		t.Fatalf("expected one benchmark b1, got %v", got)
	}
}

/* TODO: Fix this test

func TestHandleCreateEvaluationMarksFailedWhenRuntimeErrors(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
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
	// note that the fake storage only implements the functions that are used in this test
	storage := &fakeStorage{providerConfigs: providerConfigs}
	runtime := &fakeRuntime{err: errors.New("runtime failed")}
	validate := validation.NewValidator()

	h := handlers.New(storage, validate, runtime, nil, nil)
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, time.Second, "test-user", "")

	req := &bodyRequest{
		MockRequest: createMockRequest("POST", "/api/v1/evaluations/jobs"),
		body:        []byte(`{"name": "test-evaluation-job", "model":{"url":"http://test.com","name":"test"},"benchmarks":[{"id":"bench-1","provider_id":"garak"}]}`),
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
	storage := &fakeStorage{providerConfigs: providerConfigs}
	runtime := &fakeRuntime{}
	validate := validation.NewValidator()
	h := handlers.New(storage, validate, runtime, nil, nil)
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-2", logger, time.Second, "test-user", "test-tenant")

	req := &bodyRequest{
		MockRequest: createMockRequest("POST", "/api/v1/evaluations/jobs"),
		body:        []byte(`{"name": "test-evaluation-job", "model":{"url":"http://test.com","name":"test"},"benchmarks":[{"id":"bench-1","provider_id":"garak"}]}`),
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
	validate := validation.NewValidator()
	h := handlers.New(storage, validate, runtime, nil, nil)
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
	validate := validation.NewValidator()
	h := handlers.New(storage, validate, runtime, nil, nil)
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
	validate := validation.NewValidator()
	h := handlers.New(storage, validate, runtime, nil, nil)

	req := &bodyRequest{
		MockRequest: createMockRequest("POST", "/api/v1/evaluations/jobs"),
		body:        []byte(`{"name": "test-evaluation-job", "model":{"url":"http://test.com","name":"test"},"benchmarks":[{"provider_id":"garak"}]}`),
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

func TestHandleCreateEvaluationRejectsMissingBenchmarks(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	storage := &fakeStorage{}
	runtime := &fakeRuntime{}
	validate := validation.NewValidator()
	h := handlers.New(storage, validate, runtime, nil, nil)

	index := 1

	invalidRequestBodies := []string{
		`{"name": "test-evaluation-job", "model":{"url":"http://test.com","name":"test"},"benchmarks":[]}`,
		`{"name": "test-evaluation-job", "model":{"url":"http://test.com","name":"test"}}`,
	}
	for _, body := range invalidRequestBodies {
		req := &bodyRequest{
			MockRequest: createMockRequest("POST", "/api/v1/evaluations/jobs"),
			body:        []byte(body),
		}

		ctx := executioncontext.NewExecutionContext(context.Background(), fmt.Sprintf("invalid-request-body-%d", index), logger, time.Second, "test-user", "test-tenant")
		index++
		recorder := httptest.NewRecorder()
		resp := MockResponseWrapper{recorder: recorder}

		h.HandleCreateEvaluation(ctx, req, resp)

		if runtime.called {
			t.Fatalf("did not expect runtime to be invoked")
		}
		if recorder.Code != 400 {
			t.Fatalf("expected status 400, got %d: %s", recorder.Code, recorder.Body.String())
		}
	}
}

func TestHandleCreateEvaluationRejectsMissingProviderID(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	storage := &fakeStorage{}
	runtime := &fakeRuntime{}
	validate := validation.NewValidator()
	h := handlers.New(storage, validate, runtime, nil, nil)

	req := &bodyRequest{
		MockRequest: createMockRequest("POST", "/api/v1/evaluations/jobs"),
		body:        []byte(`{"name": "test-evaluation-job", "model":{"url":"http://test.com","name":"test"},"benchmarks":[{"id":"bench-1"}]}`),
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
	storage := &fakeStorage{providerConfigs: providerConfigs}
	runtime := &fakeRuntime{}
	validate := validation.NewValidator()
	h := handlers.New(storage, validate, runtime, nil, nil)
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-invalid-provider", logger, time.Second, "test-user", "test-tenant")

	req := &bodyRequest{
		MockRequest: createMockRequest("POST", "/api/v1/evaluations/jobs"),
		body:        []byte(`{"name": "test-evaluation-job", "model":{"url":"http://test.com","name":"test"},"benchmarks":[{"id":"bench-1","provider_id":"unknown"}]}`),
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
	storage := &fakeStorage{providerConfigs: providerConfigs}
	runtime := &fakeRuntime{}
	validate := validation.NewValidator()
	h := handlers.New(storage, validate, runtime, nil, nil)
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-invalid-benchmark", logger, time.Second, "test-user", "test-tenant")

	req := &bodyRequest{
		MockRequest: createMockRequest("POST", "/api/v1/evaluations/jobs"),
		body:        []byte(`{"name": "test-evaluation-job", "model":{"url":"http://test.com","name":"test"},"benchmarks":[{"id":"unknown","provider_id":"garak"}]}`),
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
	validate := validation.NewValidator()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.New(storage, validate, &fakeRuntime{}, nil, nil)

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
	validate := validation.NewValidator()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.New(storage, validate, &fakeRuntime{}, nil, nil)

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
	validate := validation.NewValidator()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.New(storage, validate, &fakeRuntime{}, nil, nil)

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
	storage := &updateEvaluationStorage{fakeStorage: &fakeStorage{
		job: &api.EvaluationJobResource{
			EvaluationJobConfig: api.EvaluationJobConfig{
				Benchmarks: []api.BenchmarkConfig{
					{Ref: api.Ref{ID: "b1"}, ProviderID: "p1"},
				},
			},
		},
	}}
	validate := validation.NewValidator()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.New(storage, validate, &fakeRuntime{}, nil, nil)

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
*/

func TestHandleCreateEvaluationRejectsExperimentWhenMLflowDisabled(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
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
	storage := &fakeStorage{providerConfigs: providerConfigs}
	runtime := &fakeRuntime{}
	validate := validation.NewValidator()
	h := handlers.New(storage, validate, runtime, nil, nil)
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-mlflow-exp", logger, time.Second, "test-user", "test-tenant")

	req := &bodyRequest{
		MockRequest: createMockRequest("POST", "/api/v1/evaluations/jobs"),
		body:        []byte(`{"name": "test-evaluation-job", "model":{"url":"http://test.com","name":"test"},"benchmarks":[{"id":"bench-1","provider_id":"garak"}],"experiment":{"name":"my-experiment"}}`),
	}
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}

	h.HandleCreateEvaluation(ctx, req, resp)

	if runtime.called {
		t.Fatalf("did not expect runtime when MLflow is disabled and experiment is set")
	}
	if recorder.Code == 202 {
		t.Fatalf("expected error response, got 202")
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "mlflow_required_for_experiment") {
		t.Fatalf("expected mlflow_required_for_experiment in body, got %q", body)
	}
}
