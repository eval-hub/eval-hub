package handlers_test

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/handlers"
	"github.com/eval-hub/eval-hub/internal/testhelpers"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/cards"
)

type recordingResultsExporter struct {
	called  bool
	cardURL string
}

func (r *recordingResultsExporter) Export(_ context.Context, _ *api.EvaluationJobResource, _ *cards.EvaluationCard) (string, error) {
	r.called = true
	return r.cardURL, nil
}

type terminalExportStorage struct {
	*fakeStorage
	cardURL  string
	updateID string
}

func (s *terminalExportStorage) WithLogger(_ *slog.Logger) abstractions.Storage { return s }
func (s *terminalExportStorage) WithContext(_ context.Context) abstractions.Storage {
	return s
}
func (s *terminalExportStorage) WithTenant(_ api.Tenant) abstractions.Storage { return s }
func (s *terminalExportStorage) WithOwner(_ api.User) abstractions.Storage    { return s }

func (s *terminalExportStorage) UpdateEvaluationJob(_ string, status *api.StatusEvent) error {
	if s.job != nil && s.job.Status != nil && status != nil && status.BenchmarkStatusEvent != nil {
		s.job.Status.State = api.OverallState(status.BenchmarkStatusEvent.Status)
	}
	return nil
}

func (s *terminalExportStorage) UpdateEvaluationJobCardURL(id string, cardURL string) error {
	s.updateID = id
	s.cardURL = cardURL
	return nil
}

func TestHandleUpdateEvaluationSkipsCardExportWhenNotTerminal(t *testing.T) {
	t.Parallel()
	exporter := &recordingResultsExporter{cardURL: "https://example.com/card.json"}
	storage := &terminalExportStorage{
		fakeStorage: &fakeStorage{
			job: &api.EvaluationJobResource{
				Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-1"}},
				Status: &api.EvaluationJobStatus{
					EvaluationJobState: api.EvaluationJobState{State: api.OverallStateRunning},
				},
			},
		},
	}
	h := handlers.New(storage, testhelpers.NewValidator(t), nil, nil, nil, exporter)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-running", logger, "test-user", "test-tenant")

	body := `{"benchmark_status_event":{"provider_id":"p1","id":"b1","status":"running"}}`
	req := &updateEvaluationRequest{
		bodyRequest: &bodyRequest{
			MockRequest: createMockRequest("POST", "/api/v1/evaluations/jobs/job-1/events"),
			body:        []byte(body),
		},
		pathValues: map[string]string{"job_id": "job-1"},
	}
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}

	h.HandleUpdateEvaluation(ctx, req, resp)

	if recorder.Code != 204 {
		t.Fatalf("expected status 204, got %d body %s", recorder.Code, recorder.Body.String())
	}
	if exporter.called {
		t.Fatal("expected card export to be skipped for non-terminal job")
	}
	if storage.updateID != "" {
		t.Fatalf("expected card URL not to be persisted, got update for %q", storage.updateID)
	}
}

func TestHandleUpdateEvaluationExportsCardOnTerminalTransition(t *testing.T) {
	t.Parallel()
	exporter := &recordingResultsExporter{cardURL: "https://example.com/card.json"}
	storage := &terminalExportStorage{
		fakeStorage: &fakeStorage{
			job: &api.EvaluationJobResource{
				Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-1"}},
				Status: &api.EvaluationJobStatus{
					EvaluationJobState: api.EvaluationJobState{State: api.OverallStateRunning},
				},
			},
		},
	}
	h := handlers.New(storage, testhelpers.NewValidator(t), nil, nil, nil, exporter)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-completed", logger, "test-user", "test-tenant")

	body := `{"benchmark_status_event":{"provider_id":"p1","id":"b1","status":"completed"}}`
	req := &updateEvaluationRequest{
		bodyRequest: &bodyRequest{
			MockRequest: createMockRequest("POST", "/api/v1/evaluations/jobs/job-1/events"),
			body:        []byte(body),
		},
		pathValues: map[string]string{"job_id": "job-1"},
	}
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}

	h.HandleUpdateEvaluation(ctx, req, resp)

	if recorder.Code != 204 {
		t.Fatalf("expected status 204, got %d body %s", recorder.Code, recorder.Body.String())
	}
	if !exporter.called {
		t.Fatal("expected card export when job transitions to terminal state")
	}
	if storage.updateID != "job-1" || storage.cardURL != "https://example.com/card.json" {
		t.Fatalf("expected card URL persisted, got id=%q url=%q", storage.updateID, storage.cardURL)
	}
}

func TestHandleUpdateEvaluationExportsCardOnFailedTransition(t *testing.T) {
	t.Parallel()
	exporter := &recordingResultsExporter{cardURL: "https://example.com/card.json"}
	storage := &terminalExportStorage{
		fakeStorage: &fakeStorage{
			job: &api.EvaluationJobResource{
				Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-1"}},
				Status: &api.EvaluationJobStatus{
					EvaluationJobState: api.EvaluationJobState{State: api.OverallStateRunning},
				},
			},
		},
	}
	h := handlers.New(storage, testhelpers.NewValidator(t), nil, nil, nil, exporter)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-failed", logger, "test-user", "test-tenant")

	body := `{"benchmark_status_event":{"provider_id":"p1","id":"b1","status":"failed"}}`
	req := &updateEvaluationRequest{
		bodyRequest: &bodyRequest{
			MockRequest: createMockRequest("POST", "/api/v1/evaluations/jobs/job-1/events"),
			body:        []byte(body),
		},
		pathValues: map[string]string{"job_id": "job-1"},
	}
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}

	h.HandleUpdateEvaluation(ctx, req, resp)

	if recorder.Code != 204 {
		t.Fatalf("expected status 204, got %d body %s", recorder.Code, recorder.Body.String())
	}
	if !exporter.called {
		t.Fatal("expected card export when job transitions to failed")
	}
}

func TestHandleUpdateEvaluationSkipsCardExportWhenTerminalStateUnchanged(t *testing.T) {
	t.Parallel()
	exporter := &recordingResultsExporter{cardURL: "https://example.com/card.json"}
	storage := &terminalExportStorage{
		fakeStorage: &fakeStorage{
			job: &api.EvaluationJobResource{
				Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-1"}},
				Status: &api.EvaluationJobStatus{
					EvaluationJobState: api.EvaluationJobState{State: api.OverallStateCompleted},
				},
			},
		},
	}
	h := handlers.New(storage, testhelpers.NewValidator(t), nil, nil, nil, exporter)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-completed-again", logger, "test-user", "test-tenant")

	body := `{"benchmark_status_event":{"provider_id":"p1","id":"b1","status":"completed"}}`
	req := &updateEvaluationRequest{
		bodyRequest: &bodyRequest{
			MockRequest: createMockRequest("POST", "/api/v1/evaluations/jobs/job-1/events"),
			body:        []byte(body),
		},
		pathValues: map[string]string{"job_id": "job-1"},
	}
	recorder := httptest.NewRecorder()
	resp := MockResponseWrapper{recorder: recorder}

	h.HandleUpdateEvaluation(ctx, req, resp)

	if recorder.Code != 204 {
		t.Fatalf("expected status 204, got %d body %s", recorder.Code, recorder.Body.String())
	}
	if exporter.called {
		t.Fatal("expected card export to be skipped when terminal state is unchanged")
	}
}
