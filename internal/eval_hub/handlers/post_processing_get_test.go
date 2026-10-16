package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/handlers"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/postprocessing"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/internal/eval_hub/workloads"
	"github.com/eval-hub/eval-hub/internal/testhelpers"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestHandleGetPostProcessing(t *testing.T) {
	validJob := standalonePostProcessingJob("post-processing-id", &api.EvaluationJobStatus{
		EvaluationJobState: api.EvaluationJobState{State: api.OverallStateCompleted},
	})
	tests := []struct {
		name       string
		id         string
		job        *api.EvaluationJobResource
		getErr     error
		wantStatus int
	}{
		{name: "missing id", wantStatus: http.StatusBadRequest},
		{name: "storage not found", id: "missing", getErr: serviceerrors.NewServiceError(messages.ResourceNotFound, "Type", "evaluation job", "ResourceId", "missing"), wantStatus: http.StatusNotFound},
		{name: "storage failure", id: "post-processing-id", getErr: errors.New("storage unavailable"), wantStatus: http.StatusInternalServerError},
		{name: "nil job", id: "post-processing-id", wantStatus: http.StatusNotFound},
		{name: "evaluation job", id: "evaluation-id", job: &api.EvaluationJobResource{EvaluationJobConfig: api.EvaluationJobConfig{Name: "evaluation"}}, wantStatus: http.StatusNotFound},
		{name: "invalid stored post-processing config", id: "post-processing-id", job: &api.EvaluationJobResource{EvaluationJobConfig: api.EvaluationJobConfig{Benchmarks: []api.EvaluationBenchmarkConfig{{ProviderID: postprocessing.ProviderID, Ref: api.Ref{ID: postprocessing.BenchmarkID}}}}}, wantStatus: http.StatusInternalServerError},
		{name: "valid post-processing job", id: "post-processing-id", job: validJob, wantStatus: http.StatusOK},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			storage := newPostProcessingHandlerStorage()
			storage.source, storage.getErr = test.job, test.getErr
			handler := handlers.New(storage, testhelpers.NewValidator(t), nil, nil, nil, nil, nil)
			recorder := httptest.NewRecorder()
			request := &postProcessingPathRequest{MockRequest: createMockRequest(http.MethodGet, "/api/v1/evaluations/post-processing/"+test.id), id: test.id}
			ctx := executioncontext.NewExecutionContext(context.Background(), "req-get-post-processing", slog.New(slog.NewTextHandler(io.Discard, nil)), "test-user", "test-tenant")

			handler.HandleGetPostProcessing(ctx, request, MockResponseWrapper{recorder: recorder})

			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; response=%s", recorder.Code, test.wantStatus, recorder.Body.String())
			}
			if test.id != "" && storage.getID != test.id {
				t.Errorf("storage lookup id = %q, want %q", storage.getID, test.id)
			}
			if test.id != "" && storage.gotWorkload != workloads.PostProcessing {
				t.Errorf("storage workload type = %q, want %q", storage.gotWorkload, workloads.PostProcessing)
			}
			if test.wantStatus == http.StatusOK {
				var response api.PostProcessingResource
				if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
					t.Fatalf("decode response: %v", err)
				}
				if response.Resource.ID != "post-processing-id" || response.Status.State != api.StateCompleted || !response.Operations.HasOperation() {
					t.Fatalf("unexpected post-processing response: %+v", response)
				}
			}
		})
	}
}
