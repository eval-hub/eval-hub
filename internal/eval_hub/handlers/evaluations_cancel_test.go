package handlers_test

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/constants"
	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/handlers"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/internal/eval_hub/workloads"
	"github.com/eval-hub/eval-hub/internal/testhelpers"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// stickyFakeStorage returns itself from With* so cancel/delete side effects are visible
// on the same pointer the test holds (fakeStorage clones on With*).
type stickyFakeStorage struct {
	*fakeStorage
}

func (s *stickyFakeStorage) WithLogger(_ *slog.Logger) abstractions.Storage         { return s }
func (s *stickyFakeStorage) WithContext(_ context.Context) abstractions.Storage     { return s }
func (s *stickyFakeStorage) WithTenant(_ api.Tenant) abstractions.Storage           { return s }
func (s *stickyFakeStorage) WithOwner(_ api.User) abstractions.Storage              { return s }
func (s *stickyFakeStorage) WithWorkloadType(_ workloads.Type) abstractions.Storage { return s }

type notFoundStorage struct {
	*fakeStorage
}

func (s *notFoundStorage) WithLogger(_ *slog.Logger) abstractions.Storage         { return s }
func (s *notFoundStorage) WithContext(_ context.Context) abstractions.Storage     { return s }
func (s *notFoundStorage) WithTenant(_ api.Tenant) abstractions.Storage           { return s }
func (s *notFoundStorage) WithOwner(_ api.User) abstractions.Storage              { return s }
func (s *notFoundStorage) WithWorkloadType(_ workloads.Type) abstractions.Storage { return s }
func (s *notFoundStorage) GetEvaluationJob(id string) (*api.EvaluationJobResource, error) {
	return nil, serviceerrors.NewServiceError(messages.ResourceNotFound, "Type", "evaluation job", "ResourceId", id)
}

func sticky(job *api.EvaluationJobResource) *stickyFakeStorage {
	return &stickyFakeStorage{fakeStorage: &fakeStorage{job: job}}
}

func newCancelRequest(jobID string, hardDelete bool) *listEvaluationsRequest {
	hardDeleteVal := "false"
	if hardDelete {
		hardDeleteVal = "true"
	}
	return &listEvaluationsRequest{
		MockRequest: createMockRequest("DELETE", "/api/v1/evaluations/jobs/"+jobID),
		queryValues: map[string][]string{"hard_delete": {hardDeleteVal}},
		pathValues:  map[string]string{constants.PathParameterJobID: jobID},
	}
}

func TestHandleCancelEvaluationSoftCancelCleansUpRunningJob(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	jobID := "job-soft-running"
	storage := sticky(&api.EvaluationJobResource{
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
	})
	runtime := &fakeRuntime{}
	h := handlers.New(storage, testhelpers.NewValidator(t), runtime, nil, nil, nil, nil)
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-soft", logger, "test-user", "test-tenant")
	recorder := httptest.NewRecorder()

	h.HandleCancelEvaluation(ctx, newCancelRequest(jobID, false), MockResponseWrapper{recorder: recorder})

	if recorder.Code != 204 {
		t.Fatalf("expected 204, got %d", recorder.Code)
	}
	if storage.lastStatus != api.OverallStateCancelled {
		t.Fatalf("expected cancelled status signal before cleanup, got %v", storage.lastStatus)
	}
	if !runtime.called {
		t.Fatal("expected runtime cleanup after soft cancel of a running job")
	}
	if storage.deleteID != "" {
		t.Fatalf("soft cancel must not hard-delete storage row, got deleteID=%q", storage.deleteID)
	}
}

func TestHandleCancelEvaluationSoftCancelSkipsCleanupWhenAlreadyCancelled(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	jobID := "job-already-cancelled"
	storage := sticky(&api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{ID: jobID},
		},
		Status: &api.EvaluationJobStatus{
			EvaluationJobState: api.EvaluationJobState{
				State: api.OverallStateCancelled,
				Message: &api.MessageInfo{
					Message:     "cancelled",
					MessageCode: "job_cancelled",
				},
			},
		},
	})
	runtime := &fakeRuntime{}
	h := handlers.New(storage, testhelpers.NewValidator(t), runtime, nil, nil, nil, nil)
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-already", logger, "test-user", "test-tenant")
	recorder := httptest.NewRecorder()

	h.HandleCancelEvaluation(ctx, newCancelRequest(jobID, false), MockResponseWrapper{recorder: recorder})

	if recorder.Code != 204 {
		t.Fatalf("expected 204, got %d", recorder.Code)
	}
	if runtime.called {
		t.Fatal("expected no runtime cleanup when job is already cancelled")
	}
}

func TestHandleCancelEvaluationSoftCancelSkipsCleanupWhenStatusMissing(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	jobID := "job-no-status"
	storage := sticky(&api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{ID: jobID},
		},
	})
	runtime := &fakeRuntime{}
	h := handlers.New(storage, testhelpers.NewValidator(t), runtime, nil, nil, nil, nil)
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-nostatus", logger, "test-user", "test-tenant")
	recorder := httptest.NewRecorder()

	h.HandleCancelEvaluation(ctx, newCancelRequest(jobID, false), MockResponseWrapper{recorder: recorder})

	if recorder.Code != 204 {
		t.Fatalf("expected 204, got %d", recorder.Code)
	}
	if runtime.called {
		t.Fatal("expected no runtime cleanup when job status is missing")
	}
}

func TestHandleCancelEvaluationHardDeleteCleansUpResources(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	jobID := "job-hard-delete"
	storage := sticky(&api.EvaluationJobResource{
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
	})
	runtime := &fakeRuntime{}
	h := handlers.New(storage, testhelpers.NewValidator(t), runtime, nil, nil, nil, nil)
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-hard", logger, "test-user", "test-tenant")
	recorder := httptest.NewRecorder()

	h.HandleCancelEvaluation(ctx, newCancelRequest(jobID, true), MockResponseWrapper{recorder: recorder})

	if recorder.Code != 204 {
		t.Fatalf("expected 204, got %d", recorder.Code)
	}
	if storage.deleteID != jobID {
		t.Fatalf("expected storage delete for %s before cleanup, got %q", jobID, storage.deleteID)
	}
	if !runtime.called {
		t.Fatal("expected runtime cleanup after hard delete")
	}
}

func TestHandleCancelEvaluationHardDeleteStillSucceedsWhenCleanupFails(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	jobID := "job-cleanup-fail"
	storage := sticky(&api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{ID: jobID},
		},
		Status: &api.EvaluationJobStatus{
			EvaluationJobState: api.EvaluationJobState{
				State: api.OverallStateRunning,
			},
		},
	})
	runtime := &fakeRuntime{err: context.DeadlineExceeded}
	h := handlers.New(storage, testhelpers.NewValidator(t), runtime, nil, nil, nil, nil)
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-cleanup-fail", logger, "test-user", "test-tenant")
	recorder := httptest.NewRecorder()

	h.HandleCancelEvaluation(ctx, newCancelRequest(jobID, true), MockResponseWrapper{recorder: recorder})

	if recorder.Code != 204 {
		t.Fatalf("expected 204 even when runtime cleanup fails, got %d", recorder.Code)
	}
	if storage.deleteID != jobID {
		t.Fatalf("expected storage delete before cleanup, got %q", storage.deleteID)
	}
	if !runtime.called {
		t.Fatal("expected runtime cleanup to be attempted")
	}
}

func TestHandleCancelEvaluationMissingJobID(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.New(&fakeStorage{}, testhelpers.NewValidator(t), &fakeRuntime{}, nil, nil, nil, nil)
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-missing", logger, "test-user", "test-tenant")
	recorder := httptest.NewRecorder()
	req := &listEvaluationsRequest{
		MockRequest: createMockRequest("DELETE", "/api/v1/evaluations/jobs/"),
		queryValues: map[string][]string{"hard_delete": {"false"}},
		pathValues:  map[string]string{},
	}

	h.HandleCancelEvaluation(ctx, req, MockResponseWrapper{recorder: recorder})

	if recorder.Code != 400 {
		t.Fatalf("expected 400 for missing job id, got %d", recorder.Code)
	}
}

func TestHandleCancelEvaluationNotFound(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.New(&notFoundStorage{fakeStorage: &fakeStorage{}}, testhelpers.NewValidator(t), &fakeRuntime{}, nil, nil, nil, nil)
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-nf", logger, "test-user", "test-tenant")
	recorder := httptest.NewRecorder()

	h.HandleCancelEvaluation(ctx, newCancelRequest("missing-job", true), MockResponseWrapper{recorder: recorder})

	if recorder.Code != 404 {
		t.Fatalf("expected 404 when job missing, got %d body %s", recorder.Code, recorder.Body.String())
	}
}
