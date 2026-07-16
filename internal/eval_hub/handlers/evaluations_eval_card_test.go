package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/constants"
	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	se "github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
)

type evalCardRequest struct {
	method      string
	uri         string
	pathValues  map[string]string
	queryValues map[string][]string
}

func (r *evalCardRequest) Method() string { return r.method }
func (r *evalCardRequest) URI() string    { return r.uri }
func (r *evalCardRequest) Path() string   { return "" }
func (r *evalCardRequest) Header(_ string) string {
	return ""
}
func (r *evalCardRequest) SetHeader(_, _ string)        {}
func (r *evalCardRequest) BodyAsBytes() ([]byte, error) { return nil, nil }
func (r *evalCardRequest) Query(key string) []string {
	if values, ok := r.queryValues[key]; ok {
		return values
	}
	return nil
}
func (r *evalCardRequest) PathValue(name string) string {
	return r.pathValues[name]
}

type evalCardGetStorage struct {
	noopStorage
	job            *api.EvaluationJobResource
	evalCard       json.RawMessage
	getJobErr      error
	getEvalCardErr error
}

func (s *evalCardGetStorage) clone() *evalCardGetStorage {
	return &evalCardGetStorage{
		job:            s.job,
		evalCard:       s.evalCard,
		getJobErr:      s.getJobErr,
		getEvalCardErr: s.getEvalCardErr,
	}
}

func (s *evalCardGetStorage) WithLogger(_ *slog.Logger) abstractions.Storage {
	return s.clone()
}

func (s *evalCardGetStorage) WithContext(_ context.Context) abstractions.Storage {
	return s.clone()
}

func (s *evalCardGetStorage) WithTenant(_ api.Tenant) abstractions.Storage {
	return s.clone()
}

func (s *evalCardGetStorage) WithOwner(_ api.User) abstractions.Storage {
	return s.clone()
}

func (s *evalCardGetStorage) GetEvaluationJob(_ string) (*api.EvaluationJobResource, error) {
	if s.getJobErr != nil {
		return nil, s.getJobErr
	}
	return s.job, nil
}

func (s *evalCardGetStorage) GetEvaluationJobEvalCard(_ string) (json.RawMessage, error) {
	if s.getEvalCardErr != nil {
		return nil, s.getEvalCardErr
	}
	return s.evalCard, nil
}

func TestRequestIncludesEvalCard(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		include string
		want    bool
	}{
		{name: "empty", include: "", want: false},
		{name: "exact match", include: "eval_card", want: true},
		{name: "comma separated", include: "status,eval_card", want: true},
		{name: "whitespace trimmed", include: " eval_card ,other", want: true},
		{name: "other value", include: "status", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := &evalCardRequest{
				queryValues: map[string][]string{"include": {tt.include}},
			}
			if got := requestIncludesEvalCard(req); got != tt.want {
				t.Fatalf("requestIncludesEvalCard() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAttachEvalCardToResponse(t *testing.T) {
	t.Parallel()

	evalCard := json.RawMessage(`{"card_version":"1.0"}`)

	t.Run("nil job", func(t *testing.T) {
		t.Parallel()
		attachEvalCardToResponse(nil, evalCard)
	})

	t.Run("empty eval card", func(t *testing.T) {
		t.Parallel()
		job := &api.EvaluationJobResource{}
		attachEvalCardToResponse(job, nil)
		if job.Results != nil {
			t.Fatal("expected results to remain nil")
		}
	})

	t.Run("creates results when missing", func(t *testing.T) {
		t.Parallel()
		job := &api.EvaluationJobResource{}
		attachEvalCardToResponse(job, evalCard)
		if job.Results == nil || string(job.Results.EvalCard) != string(evalCard) {
			t.Fatalf("results = %#v", job.Results)
		}
	})

	t.Run("preserves existing results", func(t *testing.T) {
		t.Parallel()
		job := &api.EvaluationJobResource{
			Results: &api.EvaluationJobResults{
				MLFlowExperimentURL: "https://example.com/exp",
			},
		}
		attachEvalCardToResponse(job, evalCard)
		if job.Results.MLFlowExperimentURL != "https://example.com/exp" {
			t.Fatalf("results = %#v", job.Results)
		}
		if string(job.Results.EvalCard) != string(evalCard) {
			t.Fatalf("eval_card = %s", job.Results.EvalCard)
		}
	})
}

func TestHandleGetEvaluationIncludesEvalCardWhenRequested(t *testing.T) {
	t.Parallel()

	evalCard := json.RawMessage(`{"card_version":"1.0","schema_version":"1.0"}`)
	storage := &evalCardGetStorage{
		job: &api.EvaluationJobResource{
			Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-get"}},
		},
		evalCard: evalCard,
	}
	h := New(storage, nil, nil, nil, nil, nil)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, "test-user", "test-tenant")

	req := &evalCardRequest{
		method:     "GET",
		uri:        "/api/v1/evaluations/jobs/job-get?include=eval_card",
		pathValues: map[string]string{constants.PATH_PARAMETER_JOB_ID: "job-get"},
		queryValues: map[string][]string{
			"include": {"eval_card"},
		},
	}
	recorder := httptest.NewRecorder()
	resp := testResponseWriter{recorder: recorder}

	h.HandleGetEvaluation(ctx, req, resp)

	if recorder.Code != 200 {
		t.Fatalf("expected status 200, got %d body %s", recorder.Code, recorder.Body.String())
	}
	var got api.EvaluationJobResource
	if err := json.NewDecoder(recorder.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Results == nil || string(got.Results.EvalCard) != string(evalCard) {
		t.Fatalf("results = %#v", got.Results)
	}
}

func TestHandleGetEvaluationOmitsEvalCardByDefault(t *testing.T) {
	t.Parallel()

	storage := &evalCardGetStorage{
		job: &api.EvaluationJobResource{
			Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-get"}},
		},
		evalCard: json.RawMessage(`{"card_version":"1.0"}`),
	}
	h := New(storage, nil, nil, nil, nil, nil)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, "test-user", "test-tenant")

	req := &evalCardRequest{
		method:     "GET",
		uri:        "/api/v1/evaluations/jobs/job-get",
		pathValues: map[string]string{constants.PATH_PARAMETER_JOB_ID: "job-get"},
	}
	recorder := httptest.NewRecorder()
	resp := testResponseWriter{recorder: recorder}

	h.HandleGetEvaluation(ctx, req, resp)

	if recorder.Code != 200 {
		t.Fatalf("expected status 200, got %d body %s", recorder.Code, recorder.Body.String())
	}
	if recorder.Body.String() != "" && containsEvalCardField(recorder.Body.Bytes()) {
		t.Fatalf("expected eval_card to be omitted, got body %s", recorder.Body.String())
	}
}

func TestHandleGetEvaluationEvalCardLoadError(t *testing.T) {
	t.Parallel()

	storage := &evalCardGetStorage{
		job: &api.EvaluationJobResource{
			Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-get"}},
		},
		getEvalCardErr: se.NewServiceError(messages.DatabaseOperationFailed, "Type", "evaluation job", "ResourceId", "job-get", "Error", "db down"),
	}
	h := New(storage, nil, nil, nil, nil, nil)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, "test-user", "test-tenant")

	req := &evalCardRequest{
		method:     "GET",
		uri:        "/api/v1/evaluations/jobs/job-get?include=eval_card",
		pathValues: map[string]string{constants.PATH_PARAMETER_JOB_ID: "job-get"},
		queryValues: map[string][]string{
			"include": {"eval_card"},
		},
	}
	recorder := httptest.NewRecorder()
	resp := testResponseWriter{recorder: recorder}

	h.HandleGetEvaluation(ctx, req, resp)

	if recorder.Code != 500 {
		t.Fatalf("expected status 500, got %d body %s", recorder.Code, recorder.Body.String())
	}
}

func TestHandleGetEvaluationMissingPathParam(t *testing.T) {
	t.Parallel()

	h := New(&evalCardGetStorage{}, nil, nil, nil, nil, nil)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, "test-user", "test-tenant")

	req := &evalCardRequest{
		method: "GET",
		uri:    "/api/v1/evaluations/jobs/",
	}
	recorder := httptest.NewRecorder()
	resp := testResponseWriter{recorder: recorder}

	h.HandleGetEvaluation(ctx, req, resp)

	if recorder.Code != 400 {
		t.Fatalf("expected status 400, got %d", recorder.Code)
	}
}

func TestHandleGetEvaluationJobNotFound(t *testing.T) {
	t.Parallel()

	storage := &evalCardGetStorage{
		getJobErr: se.NewServiceError(messages.ResourceNotFound, "Type", "evaluation job", "ResourceId", "missing"),
	}
	h := New(storage, nil, nil, nil, nil, nil)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, "test-user", "test-tenant")

	req := &evalCardRequest{
		method:     "GET",
		uri:        "/api/v1/evaluations/jobs/missing",
		pathValues: map[string]string{constants.PATH_PARAMETER_JOB_ID: "missing"},
	}
	recorder := httptest.NewRecorder()
	resp := testResponseWriter{recorder: recorder}

	h.HandleGetEvaluation(ctx, req, resp)

	if recorder.Code != 404 {
		t.Fatalf("expected status 404, got %d body %s", recorder.Code, recorder.Body.String())
	}
}

type testResponseWriter struct {
	recorder *httptest.ResponseRecorder
}

func (w testResponseWriter) SetStatusCode(code int) {
	w.recorder.WriteHeader(code)
}

func (w testResponseWriter) SetHeader(key, value string) {
	w.recorder.Header().Set(key, value)
}

func (w testResponseWriter) DeleteHeader(key string) {
	w.recorder.Header().Del(key)
}

func (w testResponseWriter) Write(buf []byte) (int, error) {
	return w.recorder.Write(buf)
}

func (w testResponseWriter) Error(err error, requestID string) {
	var serviceErr *se.ServiceError
	if errors.As(err, &serviceErr) {
		w.ErrorWithMessageCode(requestID, serviceErr.MessageCode(), serviceErr.MessageParams()...)
		return
	}
	w.ErrorWithMessageCode(requestID, messages.UnknownError, "Error", err.Error())
}

func (w testResponseWriter) ErrorWithMessageCode(requestID string, messageCode *messages.MessageCode, messageParams ...any) {
	w.WriteJSON(api.Error{
		Message:     messages.GetErrorMessage(messageCode, messageParams...),
		MessageCode: messageCode.GetCode(),
		Trace:       requestID,
	}, messageCode.GetStatusCode())
}

func (w testResponseWriter) WriteJSON(v any, code int, _ ...any) {
	w.recorder.Code = code
	if code != 204 {
		w.recorder.Header().Set("Content-Type", "application/json")
		w.recorder.WriteHeader(code)
		_ = json.NewEncoder(w.recorder).Encode(v)
	}
}

func containsEvalCardField(body []byte) bool {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return false
	}
	results, ok := payload["results"].(map[string]any)
	if !ok {
		return false
	}
	_, ok = results["eval_card"]
	return ok
}
