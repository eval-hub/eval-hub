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
	lastStatus   api.OverallState
}

func (f *fakeStorage) WithLogger(_ *slog.Logger) abstractions.Storage { return f }
func (f *fakeStorage) WithContext(_ context.Context) abstractions.Storage {
	return f
}

func (f *fakeStorage) CreateEvaluationJob(_ *api.EvaluationJobConfig, _ string, _ string) (*api.EvaluationJobResource, error) {
	return &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{ID: "job-1"},
		},
	}, nil
}

func (f *fakeStorage) UpdateEvaluationJobStatus(id string, state api.OverallState, message *api.MessageInfo) error {
	f.lastStatusID = id
	f.lastStatus = state
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

func TestHandleCreateEvaluationMarksFailedWhenRuntimeErrors(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	// note that the fake storage only implements the functions that are used in this test
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
	if storage.lastStatus != "" {
		t.Fatalf("did not expect evaluation status update on success")
	}
	if recorder.Code != 202 {
		t.Fatalf("expected status 202, got %d", recorder.Code)
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
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-3", logger, time.Second)
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
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-4", logger, time.Second)
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
