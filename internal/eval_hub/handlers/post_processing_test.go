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

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/handlers"
	"github.com/eval-hub/eval-hub/internal/eval_hub/postprocessing"
	"github.com/eval-hub/eval-hub/internal/testhelpers"
	"github.com/eval-hub/eval-hub/pkg/api"
)

type postProcessingHandlerStorage struct {
	*fakeStorage
	source     *api.EvaluationJobResource
	getErr     error
	createErr  error
	createdJob *api.EvaluationJobResource
	mutateJob  func(*api.EvaluationJobResource)
}

func (s *postProcessingHandlerStorage) WithLogger(_ *slog.Logger) abstractions.Storage { return s }
func (s *postProcessingHandlerStorage) WithContext(_ context.Context) abstractions.Storage {
	return s
}
func (s *postProcessingHandlerStorage) WithTenant(_ api.Tenant) abstractions.Storage { return s }
func (s *postProcessingHandlerStorage) WithOwner(_ api.User) abstractions.Storage    { return s }

func (s *postProcessingHandlerStorage) GetEvaluationJob(_ string) (*api.EvaluationJobResource, error) {
	return s.source, s.getErr
}

func (s *postProcessingHandlerStorage) CreateEvaluationJob(job *api.EvaluationJobResource) error {
	s.createdJob = job
	if s.mutateJob != nil {
		s.mutateJob(job)
	}
	return s.createErr
}

func TestHandleCreatePostProcessing(t *testing.T) {
	completedSource := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{Resource: api.Resource{ID: "source-job"}},
		Status:   &api.EvaluationJobStatus{EvaluationJobState: api.EvaluationJobState{State: api.OverallStateCompleted}},
	}
	tests := []struct {
		name            string
		body            []byte
		bodyErr         error
		storage         *postProcessingHandlerStorage
		serviceConfig   *config.Config
		wantStatus      int
		wantCreated     bool
		wantThreadCount int
	}{
		{
			name:       "body read error",
			bodyErr:    errors.New("read failed"),
			storage:    newPostProcessingHandlerStorage(),
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "invalid json",
			body:       []byte("{"),
			storage:    newPostProcessingHandlerStorage(),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid operation order",
			body:       marshalPostProcessingRequest(t, nil, []string{"unknown"}),
			storage:    newPostProcessingHandlerStorage(),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:        "external data succeeds and preserves operation order",
			body:        marshalPostProcessingRequest(t, nil, []string{"confidence_interval"}),
			storage:     newPostProcessingHandlerStorage(),
			wantStatus:  http.StatusAccepted,
			wantCreated: true,
		},
		{
			name: "completed eval job succeeds",
			body: marshalPostProcessingRequest(t, &api.EvaluationJobDataRef{ID: "source-job"}, nil),
			storage: &postProcessingHandlerStorage{
				fakeStorage: newPostProcessingBaseStorage(),
				source:      completedSource,
			},
			wantStatus:  http.StatusAccepted,
			wantCreated: true,
		},
		{
			name: "completed eval job preserves explicit thread count",
			body: marshalPostProcessingRequest(t, &api.EvaluationJobDataRef{ID: "source-job", NumParallelThreads: intPointer(8)}, nil),
			storage: &postProcessingHandlerStorage{
				fakeStorage: newPostProcessingBaseStorage(),
				source:      completedSource,
			},
			wantStatus:      http.StatusAccepted,
			wantCreated:     true,
			wantThreadCount: 8,
		},
		{
			name:       "source job not found",
			body:       marshalPostProcessingRequest(t, &api.EvaluationJobDataRef{ID: "missing"}, nil),
			storage:    newPostProcessingHandlerStorage(),
			wantStatus: http.StatusNotFound,
		},
		{
			name: "source job is incomplete",
			body: marshalPostProcessingRequest(t, &api.EvaluationJobDataRef{ID: "source-job"}, nil),
			storage: &postProcessingHandlerStorage{
				fakeStorage: newPostProcessingBaseStorage(),
				source: &api.EvaluationJobResource{
					Resource: api.EvaluationResource{Resource: api.Resource{ID: "source-job"}},
					Status:   &api.EvaluationJobStatus{EvaluationJobState: api.EvaluationJobState{State: api.OverallStateRunning}},
				},
			},
			wantStatus: http.StatusConflict,
		},
		{
			name: "source job has no status",
			body: marshalPostProcessingRequest(t, &api.EvaluationJobDataRef{ID: "source-job"}, nil),
			storage: &postProcessingHandlerStorage{
				fakeStorage: newPostProcessingBaseStorage(),
				source:      &api.EvaluationJobResource{Resource: api.EvaluationResource{Resource: api.Resource{ID: "source-job"}}},
			},
			wantStatus: http.StatusConflict,
		},
		{
			name: "source lookup error",
			body: marshalPostProcessingRequest(t, &api.EvaluationJobDataRef{ID: "source-job"}, nil),
			storage: &postProcessingHandlerStorage{
				fakeStorage: newPostProcessingBaseStorage(),
				getErr:      errors.New("storage unavailable"),
			},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:          "configured post-processing runtime does not require a catalog provider",
			body:          marshalPostProcessingRequest(t, nil, nil),
			storage:       newPostProcessingHandlerStorageWithoutProvider(),
			serviceConfig: localPostProcessingConfig(),
			wantStatus:    http.StatusAccepted,
			wantCreated:   true,
		},
		{
			name:        "dedicated endpoint bypasses catalog validation without service runtime config",
			body:        marshalPostProcessingRequest(t, nil, nil),
			storage:     newPostProcessingHandlerStorageWithoutProvider(),
			wantStatus:  http.StatusAccepted,
			wantCreated: true,
		},
		{
			name: "created job cannot be represented as post-processing resource",
			body: marshalPostProcessingRequest(t, nil, nil),
			storage: &postProcessingHandlerStorage{
				fakeStorage: newPostProcessingBaseStorage(),
				mutateJob: func(job *api.EvaluationJobResource) {
					job.Benchmarks[0].Parameters = map[string]any{}
				},
			},
			wantStatus:  http.StatusInternalServerError,
			wantCreated: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := handlers.New(test.storage, testhelpers.NewValidator(t), nil, nil, nil, test.serviceConfig, nil)
			recorder := httptest.NewRecorder()
			request := &bodyRequest{
				MockRequest: createMockRequest(http.MethodPost, "/api/v1/evaluations/post-processing"),
				body:        test.body,
				bodyErr:     test.bodyErr,
			}
			ctx := executioncontext.NewExecutionContext(context.Background(), "req-post-processing", slog.New(slog.NewTextHandler(io.Discard, nil)), "test-user", "test-tenant")

			handler.HandleCreatePostProcessing(ctx, request, MockResponseWrapper{recorder: recorder})

			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; response=%s", recorder.Code, test.wantStatus, recorder.Body.String())
			}
			if (test.storage.createdJob != nil) != test.wantCreated {
				t.Fatalf("created job = %v, want created %t", test.storage.createdJob, test.wantCreated)
			}
			if test.wantThreadCount > 0 {
				mapped, ok := test.storage.createdJob.Benchmarks[0].Parameters["operations"].(api.StandalonePostProcessingOperations)
				if !ok || mapped.ConfidenceInterval == nil || mapped.ConfidenceInterval.ResultsDataRef.EvalJob.NumParallelThreads == nil || *mapped.ConfidenceInterval.ResultsDataRef.EvalJob.NumParallelThreads != test.wantThreadCount {
					t.Fatalf("eval-job thread default was not mapped: %#v", test.storage.createdJob.Benchmarks[0].Parameters["operations"])
				}
			}
			if test.wantCreated && test.wantStatus == http.StatusAccepted {
				var response api.PostProcessingResource
				if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
					t.Fatalf("decode response: %v", err)
				}
				if !response.Operations.HasOperation() {
					t.Fatal("response does not include the submitted operation")
				}
			}
		})
	}
}

func TestHandleCreateEvaluationRejectsInternalPostProcessingProvider(t *testing.T) {
	storage := newPostProcessingHandlerStorage()
	handler := handlers.New(storage, testhelpers.NewValidator(t), nil, nil, nil, localPostProcessingConfig(), nil)
	recorder := httptest.NewRecorder()
	request := &bodyRequest{
		MockRequest: createMockRequest(http.MethodPost, "/api/v1/evaluations/jobs"),
		body:        []byte(`{"name":"ordinary-evaluation","model":{"name":"model","url":"http://model.example"},"benchmarks":[{"id":"evaluation-post-processor","provider_id":"evalhub-internal"}]}`),
	}
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-ordinary-evaluation", slog.New(slog.NewTextHandler(io.Discard, nil)), "test-user", "test-tenant")

	handler.HandleCreateEvaluation(ctx, request, MockResponseWrapper{recorder: recorder})

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; response=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if storage.createdJob != nil {
		t.Fatal("ordinary evaluation with an internal provider must not be persisted")
	}
}

func localPostProcessingConfig() *config.Config {
	return &config.Config{
		Service: &config.ServiceConfig{LocalMode: true},
	}
}

func newPostProcessingBaseStorage() *fakeStorage {
	return &fakeStorage{providerConfigs: map[string]api.ProviderResource{
		postprocessing.ProviderID: {
			Resource: api.Resource{ID: postprocessing.ProviderID},
			ProviderConfig: api.ProviderConfig{Benchmarks: []api.BenchmarkResource{{
				ID: postprocessing.BenchmarkID,
			}}},
		},
	}}
}

func newPostProcessingHandlerStorage() *postProcessingHandlerStorage {
	return &postProcessingHandlerStorage{fakeStorage: newPostProcessingBaseStorage()}
}

func newPostProcessingHandlerStorageWithoutProvider() *postProcessingHandlerStorage {
	return &postProcessingHandlerStorage{fakeStorage: &fakeStorage{providerConfigs: map[string]api.ProviderResource{}}}
}

func marshalPostProcessingRequest(t *testing.T, evalJob *api.EvaluationJobDataRef, order []string) []byte {
	t.Helper()
	results := &api.PostProcessingResultsDataRef{PVC: &api.PVCTestDataRef{ClaimName: "results-data"}}
	primaryScore := &api.PrimaryScore{Metric: "accuracy"}
	if evalJob != nil {
		results = &api.PostProcessingResultsDataRef{EvalJob: evalJob}
		primaryScore = nil
	}
	request := api.StandalonePostProcessingRequest{
		PostProcessingCommon: api.PostProcessingCommon{OperationOrder: order},
		Operations: api.StandalonePostProcessingOperations{ConfidenceInterval: &api.StandaloneConfidenceIntervalConfig{
			ConfidenceIntervalConfigCommon: api.ConfidenceIntervalConfigCommon{
				CalibrationDataRef: []api.CalibrationDataRef{{
					PVC:        &api.PVCTestDataRef{ClaimName: "calibration-data"},
					DataConfig: api.CalibrationDataConfig{Format: "jsonl", Columns: api.CalibrationDataColumns{Label: "label", Prediction: "prediction"}},
				}},
				SignificanceLevel: 0.05,
			},
			ResultsDataRef: results,
			PrimaryScore:   primaryScore,
		}},
	}
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func intPointer(value int) *int {
	return &value
}
