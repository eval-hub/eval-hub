package handlers_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/handlers"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/internal/eval_hub/workloads"
	"github.com/eval-hub/eval-hub/internal/testhelpers"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestHandleDeletePostProcessing(t *testing.T) {
	activeJob := standalonePostProcessingJob("post-processing-id", &api.EvaluationJobStatus{
		EvaluationJobState: api.EvaluationJobState{State: api.OverallStateRunning},
	})
	cancelledJob := standalonePostProcessingJob("cancelled-id", &api.EvaluationJobStatus{
		EvaluationJobState: api.EvaluationJobState{State: api.OverallStateCancelled},
	})
	noStatusJob := standalonePostProcessingJob("no-status-id", nil)
	tests := []struct {
		name              string
		id                string
		job               *api.EvaluationJobResource
		getErr            error
		deleteErr         error
		runtime           *fakeRuntime
		wantStatus        int
		wantDeleteCalls   int
		wantRuntimeDelete bool
	}{
		{name: "missing id", wantStatus: http.StatusBadRequest},
		{name: "not found", id: "missing", getErr: serviceerrors.NewServiceError(messages.ResourceNotFound, "Type", "evaluation job", "ResourceId", "missing"), wantStatus: http.StatusNotFound},
		{name: "storage failure", id: "post-processing-id", getErr: errors.New("storage unavailable"), wantStatus: http.StatusInternalServerError},
		{name: "nil job", id: "post-processing-id", wantStatus: http.StatusNotFound},
		{name: "evaluation job", id: "evaluation-id", job: &api.EvaluationJobResource{EvaluationJobConfig: api.EvaluationJobConfig{Name: "evaluation"}}, wantStatus: http.StatusNotFound},
		{name: "delete without runtime", id: "post-processing-id", job: activeJob, wantStatus: http.StatusNoContent, wantDeleteCalls: 1},
		{name: "delete cancelled job skips runtime cleanup", id: "cancelled-id", job: cancelledJob, runtime: &fakeRuntime{}, wantStatus: http.StatusNoContent, wantDeleteCalls: 1},
		{name: "runtime cleanup failure is best effort", id: "post-processing-id", job: activeJob, runtime: &fakeRuntime{err: errors.New("runtime unavailable")}, wantStatus: http.StatusNoContent, wantDeleteCalls: 1, wantRuntimeDelete: true},
		{name: "job without status still cleans up runtime", id: "no-status-id", job: noStatusJob, runtime: &fakeRuntime{}, wantStatus: http.StatusNoContent, wantDeleteCalls: 1, wantRuntimeDelete: true},
		{name: "storage delete failure", id: "post-processing-id", job: activeJob, deleteErr: errors.New("database unavailable"), runtime: &fakeRuntime{}, wantStatus: http.StatusInternalServerError, wantDeleteCalls: 1, wantRuntimeDelete: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			storage := newPostProcessingHandlerStorage()
			storage.source, storage.getErr, storage.deleteErr = test.job, test.getErr, test.deleteErr
			var runtime abstractions.Runtime
			if test.runtime != nil {
				runtime = test.runtime
			}
			handler := handlers.New(storage, testhelpers.NewValidator(t), runtime, nil, nil, nil, nil)
			recorder := httptest.NewRecorder()
			request := &postProcessingPathRequest{MockRequest: createMockRequest(http.MethodDelete, "/api/v1/evaluations/post-processing/"+test.id), id: test.id}
			ctx := executioncontext.NewExecutionContext(context.Background(), "req-delete-post-processing", slog.New(slog.NewTextHandler(io.Discard, nil)), "test-user", "test-tenant")

			handler.HandleDeletePostProcessing(ctx, request, MockResponseWrapper{recorder: recorder})

			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; response=%s", recorder.Code, test.wantStatus, recorder.Body.String())
			}
			if storage.deleteCall != test.wantDeleteCalls {
				t.Errorf("storage delete calls = %d, want %d", storage.deleteCall, test.wantDeleteCalls)
			}
			if test.wantDeleteCalls > 0 && storage.deleteID != test.id {
				t.Errorf("storage delete id = %q, want %q", storage.deleteID, test.id)
			}
			if test.id != "" && storage.gotWorkload != workloads.PostProcessing {
				t.Errorf("storage workload type = %q, want %q", storage.gotWorkload, workloads.PostProcessing)
			}
			if test.runtime != nil && test.runtime.called != test.wantRuntimeDelete {
				t.Errorf("runtime cleanup called = %t, want %t", test.runtime.called, test.wantRuntimeDelete)
			}
		})
	}
}
