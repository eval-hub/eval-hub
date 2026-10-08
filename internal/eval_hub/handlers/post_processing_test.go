package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_hub/constants"
	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/handlers"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/postprocessing"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/internal/eval_hub/workloads"
	"github.com/eval-hub/eval-hub/internal/testhelpers"
	"github.com/eval-hub/eval-hub/pkg/api"
)

type postProcessingHandlerStorage struct {
	*fakeStorage
	source      *api.EvaluationJobResource
	getErr      error
	createErr   error
	deleteErr   error
	createdJob  *api.EvaluationJobResource
	getID       string
	deleteID    string
	deleteCall  int
	gotWorkload workloads.Type
	mutateJob   func(*api.EvaluationJobResource)
}

func (s *postProcessingHandlerStorage) WithLogger(_ *slog.Logger) abstractions.Storage { return s }
func (s *postProcessingHandlerStorage) WithContext(_ context.Context) abstractions.Storage {
	return s
}
func (s *postProcessingHandlerStorage) WithTenant(_ api.Tenant) abstractions.Storage { return s }
func (s *postProcessingHandlerStorage) WithOwner(_ api.User) abstractions.Storage    { return s }
func (s *postProcessingHandlerStorage) WithWorkloadType(workloadType workloads.Type) abstractions.Storage {
	s.gotWorkload = workloadType
	return s
}

func (s *postProcessingHandlerStorage) GetEvaluationJob(id string) (*api.EvaluationJobResource, error) {
	s.getID = id
	return s.source, s.getErr
}

func (s *postProcessingHandlerStorage) CreateEvaluationJob(job *api.EvaluationJobResource) error {
	s.createdJob = job
	if s.mutateJob != nil {
		s.mutateJob(job)
	}
	return s.createErr
}

func (s *postProcessingHandlerStorage) DeleteEvaluationJob(id string) error {
	s.deleteCall++
	s.deleteID = id
	return s.deleteErr
}

type postProcessingPathRequest struct {
	*MockRequest
	id string
}

func (r *postProcessingPathRequest) PathValue(name string) string {
	if name == constants.PathParameterPostProcessingID {
		return r.id
	}
	return ""
}

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

func standalonePostProcessingJob(id string, status *api.EvaluationJobStatus) *api.EvaluationJobResource {
	request := &api.StandalonePostProcessingRequest{
		Operations: api.StandalonePostProcessingOperations{ConfidenceInterval: &api.StandaloneConfidenceIntervalConfig{
			ConfidenceIntervalConfigCommon: api.ConfidenceIntervalConfigCommon{
				CalibrationDataRef: []api.CalibrationDataRef{{PVC: &api.PVCTestDataRef{ClaimName: "calibration"}}},
				SignificanceLevel:  0.05,
			},
			ResultsDataRef: &api.PostProcessingResultsDataRef{PVC: &api.PVCTestDataRef{ClaimName: "results"}},
			PrimaryScore:   &api.PrimaryScore{Metric: "accuracy"},
		}},
	}
	return &api.EvaluationJobResource{
		Resource:            api.EvaluationResource{Resource: api.Resource{ID: id}},
		EvaluationJobConfig: *postprocessing.ToEvaluationJob(request),
		Status:              status,
	}
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
		wantExports     *api.EvaluationExports
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
			name: "storage create failure",
			body: marshalPostProcessingRequest(t, nil, nil),
			storage: &postProcessingHandlerStorage{
				fakeStorage: newPostProcessingBaseStorage(),
				createErr:   errors.New("storage unavailable"),
			},
			wantStatus:  http.StatusInternalServerError,
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
			name: "post-processing job cannot be used as an eval-job source",
			body: marshalPostProcessingRequest(t, &api.EvaluationJobDataRef{ID: "source-job"}, nil),
			storage: &postProcessingHandlerStorage{
				fakeStorage: newPostProcessingBaseStorage(),
				source: &api.EvaluationJobResource{
					Resource: api.EvaluationResource{Resource: api.Resource{ID: "source-job"}},
					EvaluationJobConfig: api.EvaluationJobConfig{Benchmarks: []api.EvaluationBenchmarkConfig{{
						Ref: api.Ref{ID: postprocessing.BenchmarkID}, ProviderID: postprocessing.ProviderID,
					}}},
					Status: &api.EvaluationJobStatus{EvaluationJobState: api.EvaluationJobState{State: api.OverallStateCompleted}},
				},
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "completed eval job propagates OCI export settings",
			body: marshalPostProcessingRequest(t, &api.EvaluationJobDataRef{ID: "source-job"}, nil),
			storage: &postProcessingHandlerStorage{
				fakeStorage: newPostProcessingBaseStorage(),
				source: &api.EvaluationJobResource{
					Resource: api.EvaluationResource{Resource: api.Resource{ID: "source-job"}},
					Status:   &api.EvaluationJobStatus{EvaluationJobState: api.EvaluationJobState{State: api.OverallStateCompleted}},
					EvaluationJobConfig: api.EvaluationJobConfig{
						Exports: &api.EvaluationExports{OCI: &api.EvaluationExportsOCI{
							Coordinates: api.OCICoordinates{
								OCIHost:       "quay.io",
								OCIRepository: "rh-ee-nbs/nbs-dev",
								OCITag:        "source-results",
								Annotations:   map[string]string{"purpose": "evaluation-results"},
							},
							K8s: &api.OCIConnectionConfig{Connection: "oci-credentials"},
						}},
					},
				},
			},
			wantStatus:  http.StatusAccepted,
			wantCreated: true,
			wantExports: &api.EvaluationExports{OCI: &api.EvaluationExportsOCI{
				Coordinates: api.OCICoordinates{
					OCIHost:       "quay.io",
					OCIRepository: "rh-ee-nbs/nbs-dev",
					OCITag:        "source-results",
					Annotations:   map[string]string{"purpose": "evaluation-results"},
				},
				K8s: &api.OCIConnectionConfig{Connection: "oci-credentials"},
			}},
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
			if test.wantCreated && !reflect.DeepEqual(test.storage.createdJob.Exports, test.wantExports) {
				t.Errorf("created job exports = %#v, want %#v", test.storage.createdJob.Exports, test.wantExports)
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
