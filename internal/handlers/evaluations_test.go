package handlers_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/abstractions"
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
	lastStatus   *api.StatusEvent
}

func (f *fakeStorage) WithLogger(_ *slog.Logger) abstractions.Storage { return f }
func (f *fakeStorage) WithContext(_ context.Context) abstractions.Storage {
	return f
}
func (f *fakeStorage) GetDatasourceName() string  { return "fake" }
func (f *fakeStorage) Ping(_ time.Duration) error { return nil }
func (f *fakeStorage) CreateEvaluationJob(_ *api.EvaluationJobConfig) (*api.EvaluationJobResource, error) {
	return &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{ID: "job-1"},
		},
	}, nil
}
func (f *fakeStorage) GetEvaluationJob(_ string) (*api.EvaluationJobResource, error) { return nil, nil }
func (f *fakeStorage) GetEvaluationJobs(_ int, _ int, _ string) (*abstractions.QueryResults[api.EvaluationJobResource], error) {
	return nil, nil
}
func (f *fakeStorage) DeleteEvaluationJob(_ string, _ bool) error { return nil }
func (f *fakeStorage) UpdateEvaluationJobStatus(id string, status *api.StatusEvent) error {
	f.lastStatusID = id
	f.lastStatus = status
	return nil
}
func (f *fakeStorage) UpdateEvaluationJob(_ string, _ *api.RunStatusInternal) error { return nil }
func (f *fakeStorage) CreateCollection(_ *api.CollectionResource) error             { return nil }
func (f *fakeStorage) GetCollection(_ string, _ bool) (*api.CollectionResource, error) {
	return nil, nil
}
func (f *fakeStorage) GetCollections(_ int, _ int) (*abstractions.QueryResults[api.CollectionResource], error) {
	return nil, nil
}
func (f *fakeStorage) UpdateCollection(_ *api.CollectionResource) error { return nil }
func (f *fakeStorage) DeleteCollection(_ string) error                  { return nil }
func (f *fakeStorage) Close() error                                     { return nil }

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

func TestHandleCreateEvaluationMarksFailedWhenRuntimeErrors(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	storage := &fakeStorage{}
	runtime := &fakeRuntime{err: errors.New("runtime failed")}
	validate := validator.New()
	h := handlers.New(storage, validate, runtime, nil, nil, nil)
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, time.Second)

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
	if storage.lastStatus == nil || storage.lastStatusID == "" {
		t.Fatalf("expected evaluation status update to be recorded")
	}
	if storage.lastStatus.StatusEvent == nil || storage.lastStatus.StatusEvent.State != api.StateFailed {
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
	h := handlers.New(storage, validate, runtime, nil, nil, nil)
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-2", logger, time.Second)

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
	if storage.lastStatus != nil {
		t.Fatalf("did not expect evaluation status update on success")
	}
	if recorder.Code != 202 {
		t.Fatalf("expected status 202, got %d", recorder.Code)
	}
}
