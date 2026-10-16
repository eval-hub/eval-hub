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

	"github.com/eval-hub/eval-hub/internal/eval_hub/evaluations"
	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/handlers"
	"github.com/eval-hub/eval-hub/internal/eval_hub/workloads"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// An additional implementation exercises registry dispatch without changes to
// the shared handler or the built-in workload implementations.
type retrievalWorkload struct {
	evaluations.Workload
	workloadType workloads.Type
}

func (w retrievalWorkload) Type() workloads.Type { return w.workloadType }
func (w retrievalWorkload) ProviderID() string   { return string(w.workloadType) + "-provider" }
func (retrievalWorkload) BenchmarkID() string    { return "retrieve" }
func (w retrievalWorkload) MatchesJob(job *api.EvaluationJobConfig) bool {
	return job != nil && job.Name == string(w.workloadType)
}
func (retrievalWorkload) GetOptions() workloads.GetOptions {
	return workloads.GetOptions{
		PathParameter: "retrieval_id", ResourceName: "retrieval result",
		SpanName: "get-retrieval", TraceIDAttribute: "retrieval.id",
	}
}
func (retrievalWorkload) ValidateCreate(*api.EvaluationJobConfig) error {
	return errors.New("create validation does not apply to GET")
}
func (w retrievalWorkload) ValidateGet(id string, job *api.EvaluationJobResource) error {
	return workloads.ValidateResource(w, id, job)
}
func (retrievalWorkload) ValidateDelete(string, *api.EvaluationJobResource) error {
	return errors.New("delete validation does not apply to GET")
}
func (w retrievalWorkload) ResourceFromJob(job *api.EvaluationJobResource) (any, error) {
	return map[string]string{"id": job.Resource.ID, "workload": string(w.workloadType)}, nil
}

func TestHandleGetJobSupportsRegisteredImplementation(t *testing.T) {
	workload := &retrievalWorkload{workloadType: workloads.Type(t.Name())}
	workloads.Register(workload)
	storage := &fakeStorage{job: &api.EvaluationJobResource{
		Resource:            api.EvaluationResource{Resource: api.Resource{ID: "custom-job"}},
		EvaluationJobConfig: api.EvaluationJobConfig{Name: string(workload.Type())},
	}}
	handler := handlers.New(storage, nil, nil, nil, nil, nil, nil)
	ctx := executioncontext.NewExecutionContext(context.Background(), "custom-get", slog.New(slog.NewTextHandler(io.Discard, nil)), "user", "tenant")
	request := &listEvaluationsRequest{
		MockRequest: createMockRequest(http.MethodGet, "/custom/custom-job"),
		pathValues:  map[string]string{"retrieval_id": "custom-job", "job_id": "custom-job"},
	}
	recorder := httptest.NewRecorder()
	handler.HandleGetJob(ctx, request, MockResponseWrapper{recorder: recorder}, workload.Type())
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET status %d: %s", recorder.Code, recorder.Body.String())
	}
	var response map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["id"] != "custom-job" || response["workload"] != string(workload.Type()) {
		t.Fatalf("workload response was not preserved: %v", response)
	}

	// The evaluation route must not expose the registered workload's job.
	recorder = httptest.NewRecorder()
	handler.HandleGetEvaluation(ctx, request, MockResponseWrapper{recorder: recorder})
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("evaluation GET status %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestHandleGetJobRejectsUnregisteredWorkload(t *testing.T) {
	handler := handlers.New(nil, nil, nil, nil, nil, nil, nil)
	ctx := executioncontext.NewExecutionContext(context.Background(), "unknown-get", slog.New(slog.NewTextHandler(io.Discard, nil)), "user", "tenant")
	recorder := httptest.NewRecorder()
	handler.HandleGetJob(ctx, createMockRequest(http.MethodGet, "/unknown/job"), MockResponseWrapper{recorder: recorder}, "unregistered")
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("unregistered workload GET status %d: %s", recorder.Code, recorder.Body.String())
	}
}
