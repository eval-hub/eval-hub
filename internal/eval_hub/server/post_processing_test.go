package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/common"
	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_hub/postprocessing"
	"github.com/eval-hub/eval-hub/internal/eval_hub/runtimes/shared"
	"github.com/eval-hub/eval-hub/internal/eval_hub/server"
	"github.com/eval-hub/eval-hub/internal/eval_hub/storage"
	"github.com/eval-hub/eval-hub/internal/testhelpers"
	"github.com/eval-hub/eval-hub/pkg/api"
)

const postProcessingPath = "/api/v1/evaluations/post-processing"

type postProcessingRuntime struct {
	stubRuntime
	job     *api.EvaluationJobResource
	storage abstractions.RuntimeStorage
	err     error
}

func (r *postProcessingRuntime) WithLogger(_ *slog.Logger) abstractions.Runtime     { return r }
func (r *postProcessingRuntime) WithContext(_ context.Context) abstractions.Runtime { return r }
func (r *postProcessingRuntime) RunEvaluationJob(job *api.EvaluationJobResource, benchmarks []api.EvaluationBenchmarkConfig, store abstractions.RuntimeStorage) error {
	r.job, r.storage = job, store
	if r.err != nil {
		return r.err
	}
	if postprocessing.IsPostProcessingJob(&job.EvaluationJobConfig) {
		benchmark := benchmarks[0]
		provider, err := shared.ProviderForBenchmark(job, benchmark, store)
		if err != nil {
			return err
		}
		_, err = shared.BuildJobSpec(job, provider.Resource.ID, &benchmark, 0, nil, provider)
		return err
	}
	return r.stubRuntime.RunEvaluationJob(job, benchmarks, store)
}

func newPostProcessingServer(t *testing.T) (http.Handler, abstractions.Storage, *postProcessingRuntime) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	providers := map[string]api.ProviderResource{
		"ordinary-provider": {
			Resource: api.Resource{ID: "ordinary-provider"},
			ProviderConfig: api.ProviderConfig{
				Name:       "Ordinary Provider",
				Benchmarks: []api.BenchmarkResource{{ID: "ordinary-benchmark"}},
			},
		},
	}
	db := map[string]any{"driver": "sqlite", "url": "file:" + common.GUID() + "?mode=memory&cache=shared"}
	store, err := storage.NewStorage(&db, nil, providers, false, false, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	runtime := &postProcessingRuntime{stubRuntime: stubRuntime{logger: logger, providers: providers}}
	srv, err := server.NewServer(logger, &config.Config{Service: &config.ServiceConfig{}}, store, testhelpers.NewValidator(t), runtime, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := srv.SetupRoutes()
	if err != nil {
		t.Fatal(err)
	}
	return handler, store.WithTenant("tenant-a").WithOwner("user-a"), runtime
}

func postProcessingBody(source string) string {
	primaryScore := `,"primary_score":{"metric":"accuracy"}`
	if strings.Contains(source, `"eval_job"`) {
		primaryScore = ""
	}
	return fmt.Sprintf(`{"name":"confidence intervals",
		"hardware_config":{"cpu":{"request":"500m"},"queue":{"name":"batch"}},
		"operations":{"confidence_interval":{
			"results_data_ref":%s%s,
			"calibration_data_ref":[
				{"s3":{"bucket":"calibration","key":"labels","secret_ref":"s3-secret"},"data_config":{"format":"jsonl","columns":{"label":"human","prediction":"judge"}}},
				{"pvc":{"claim_name":"calibration-data"},"data_config":{"format":"jsonl","columns":{"label":"truth","prediction":"score"}}}
			],
			"significance_level":0.05
		}}}`, source, primaryScore)
}

func postProcessingRequest(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("X-Tenant", "tenant-a")
	req.Header.Set("X-User", "user-a")
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}

func createPostProcessingSource(t *testing.T, store abstractions.Storage, state api.OverallState) *api.EvaluationJobResource {
	t.Helper()
	job := &api.EvaluationJobResource{
		Resource:            api.EvaluationResource{Resource: api.Resource{ID: common.GUID(), Tenant: "tenant-a", Owner: "user-a", CreatedAt: time.Now()}},
		EvaluationJobConfig: api.EvaluationJobConfig{Name: "source", Model: &api.ModelRef{Name: "source model", URL: "https://model.example"}},
		Status:              &api.EvaluationJobStatus{EvaluationJobState: api.EvaluationJobState{State: state}},
		Results: &api.EvaluationJobResults{
			Benchmarks:          []api.BenchmarkResult{{ID: "accuracy", ProviderID: "source-provider", Metrics: map[string]any{"accuracy": 0.9}}},
			MLFlowExperimentURL: "https://mlflow.example/experiments/1",
		},
	}
	if err := store.CreateEvaluationJob(job); err != nil {
		t.Fatal(err)
	}
	stored, err := store.GetEvaluationJob(job.Resource.ID)
	if err != nil {
		t.Fatal(err)
	}
	return stored
}

func TestPostProcessingCreate(t *testing.T) {
	sources := map[string]string{
		"s3":     `{"s3":{"bucket":"results","key":"accuracy","secret_ref":"s3-secret"}}`,
		"pvc":    `{"pvc":{"claim_name":"results-data","sub_path":"accuracy"},"type":"pre_recorded_data"}`,
		"git":    `{"git":{"url":"https://github.com/example/results.git","ref":"main"}}`,
		"hf":     `{"hf":{"repo_id":"example/results","revision":"main"}}`,
		"mlflow": `{"mlflow":{"run_id":"run-1","artifact_path":"results/accuracy.json"}}`,
		"oci":    `{"oci":{"coordinates":{"oci_host":"quay.io","oci_repository":"example/results"},"artifact_path":"results.json","digest":"sha256:` + strings.Repeat("a", 64) + `","k8s":{"connection":"registry-secret"}}}`,
	}
	for name, source := range sources {
		t.Run(name, func(t *testing.T) {
			handler, store, runtime := newPostProcessingServer(t)
			response := postProcessingRequest(handler, http.MethodPost, postProcessingPath, postProcessingBody(source))
			if response.Code != http.StatusAccepted {
				t.Fatalf("status %d: %s", response.Code, response.Body.String())
			}
			var result api.PostProcessingResource
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Name != "confidence intervals" || result.Status.State != api.StatePending || result.Resource.ID == "" || result.Resource.Tenant != "tenant-a" || result.Resource.Owner != "user-a" {
				t.Fatalf("unexpected response: %+v", result)
			}
			if runtime.job == nil || runtime.job.Resource.ID != result.Resource.ID {
				t.Fatal("backing job was not launched")
			}
			stored, err := store.GetEvaluationJob(result.Resource.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !postprocessing.IsPostProcessingJob(&stored.EvaluationJobConfig) || stored.Name != result.Name || stored.Model == nil || stored.Model.Name == "" || stored.Model.URL != "" || stored.Model.Auth != nil {
				t.Fatalf("unexpected backing job: %+v", stored.EvaluationJobConfig)
			}
			benchmark := stored.Benchmarks[0]
			hardwareConfig := stored.HardwareConfig
			if len(benchmark.Parameters) != 1 || hardwareConfig == nil || hardwareConfig.CPU == nil || hardwareConfig.CPU.Request != "500m" || hardwareConfig.Queue == nil || hardwareConfig.Queue.Kind != "kueue" || benchmark.TestDataRef != nil {
				t.Fatalf("unexpected post-processing job configuration: hardware_config=%+v benchmark=%+v", hardwareConfig, benchmark)
			}
			operations, err := postprocessing.OperationsFromJob(&stored.EvaluationJobConfig)
			if err != nil || !reflect.DeepEqual(operations, result.Operations) {
				t.Fatalf("configuration did not round trip: %v", err)
			}
			var raw map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &raw); err != nil {
				t.Fatal(err)
			}
			for _, field := range []string{"model", "benchmarks", "results"} {
				if _, exists := raw[field]; exists {
					t.Fatalf("submission response exposes %s", field)
				}
			}
			if err := runtime.storage.UpdateEvaluationJob(result.Resource.ID, &api.StatusEvent{
				BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
					ProviderID: postprocessing.ProviderID, ID: postprocessing.BenchmarkID, Status: api.StateCompleted,
				},
			}); err != nil {
				t.Fatal(err)
			}
			completed, err := store.GetEvaluationJob(result.Resource.ID)
			if err != nil || completed.Status.State != api.OverallStateCompleted {
				t.Fatalf("external-data computation did not complete independently: %v, %+v", err, completed)
			}
		})
	}
}

func TestPostProcessingCompletionDoesNotUpdateSource(t *testing.T) {
	handler, store, runtime := newPostProcessingServer(t)
	source := createPostProcessingSource(t, store, api.OverallStateCompleted)
	body := postProcessingBody(fmt.Sprintf(`{"eval_job":{"id":%q}}`, source.Resource.ID))
	submit := func() string {
		t.Helper()
		response := postProcessingRequest(handler, http.MethodPost, postProcessingPath, body)
		if response.Code != http.StatusAccepted {
			t.Fatalf("status %d: %s", response.Code, response.Body.String())
		}
		var result api.PostProcessingResource
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result.Resource.ID
	}
	assertSourceUnchanged := func() {
		t.Helper()
		stored, err := store.GetEvaluationJob(source.Resource.ID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Status.State != api.OverallStateCompleted || !reflect.DeepEqual(stored.Results, source.Results) {
			t.Fatalf("source evaluation changed during post-processing: %+v", stored)
		}
	}
	first := submit()
	assertSourceUnchanged()
	event := &api.StatusEvent{BenchmarkStatusEvent: &api.BenchmarkStatusEvent{ProviderID: postprocessing.ProviderID, ID: postprocessing.BenchmarkID, Status: api.StateRunning}}
	if err := runtime.storage.UpdateEvaluationJob(first, event); err != nil {
		t.Fatal(err)
	}
	assertSourceUnchanged()
	event.BenchmarkStatusEvent.Status = api.StateCompleted
	if err := runtime.storage.UpdateEvaluationJob(first, event); err != nil {
		t.Fatal(err)
	}
	assertSourceUnchanged()

	second := submit()
	assertSourceUnchanged()
	eventBody, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	response := postProcessingRequest(handler, http.MethodPost, "/api/v1/evaluations/jobs/"+second+"/events", string(eventBody))
	if response.Code != http.StatusNoContent {
		t.Fatalf("completion status %d: %s", response.Code, response.Body.String())
	}
	assertSourceUnchanged()
	// A replay from an older completed computation must not change its source.
	_ = runtime.storage.UpdateEvaluationJob(first, event)
	assertSourceUnchanged()

	failed := submit()
	event.BenchmarkStatusEvent.Status = api.StateFailed
	if err := runtime.storage.UpdateEvaluationJob(failed, event); err != nil {
		t.Fatal(err)
	}
	assertSourceUnchanged()
	cancelled := submit()
	response = postProcessingRequest(handler, http.MethodDelete, "/api/v1/evaluations/jobs/"+cancelled, "")
	if response.Code != http.StatusNoContent {
		t.Fatalf("cancel status %d: %s", response.Code, response.Body.String())
	}
	assertSourceUnchanged()
}

func TestPostProcessingSourceValidation(t *testing.T) {
	for _, state := range []api.OverallState{api.OverallStatePending, api.OverallStateRunning, api.OverallStateFailed, api.OverallStateCancelled, api.OverallStatePartiallyFailed, api.OverallStateCompleted} {
		t.Run(string(state), func(t *testing.T) {
			handler, store, runtime := newPostProcessingServer(t)
			source := createPostProcessingSource(t, store, state)
			body := postProcessingBody(fmt.Sprintf(`{"eval_job":{"id":%q,"num_parallel_threads":2}}`, source.Resource.ID))
			response := postProcessingRequest(handler, http.MethodPost, postProcessingPath, body)
			want := http.StatusConflict
			if state == api.OverallStateCompleted {
				want = http.StatusAccepted
			}
			if response.Code != want {
				t.Fatalf("status %d: %s", response.Code, response.Body.String())
			}
			if want == http.StatusConflict && runtime.job != nil {
				t.Fatal("invalid source launched a computation")
			}
			if want == http.StatusAccepted {
				operations, err := postprocessing.OperationsFromJob(&runtime.job.EvaluationJobConfig)
				if err != nil || operations.ConfidenceInterval.ResultsDataRef.EvalJob.NumParallelThreads == nil || *operations.ConfidenceInterval.ResultsDataRef.EvalJob.NumParallelThreads != 2 {
					t.Fatalf("explicit parallelism was not preserved: %v", err)
				}
			}
		})
	}
	handler, store, _ := newPostProcessingServer(t)
	source := createPostProcessingSource(t, store, api.OverallStateCompleted)
	for _, sourceID := range []string{common.GUID(), source.Resource.ID} {
		body := postProcessingBody(fmt.Sprintf(`{"eval_job":{"id":%q}}`, sourceID))
		req := httptest.NewRequest(http.MethodPost, postProcessingPath, strings.NewReader(body))
		req.Header.Set("X-Tenant", "tenant-b")
		req.Header.Set("X-User", "user-b")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != http.StatusNotFound {
			t.Fatalf("missing/cross-tenant source status %d: %s", response.Code, response.Body.String())
		}
	}
}

func TestPostProcessingValidation(t *testing.T) {
	valid := postProcessingBody(`{"eval_job":{"id":"source"}}`)
	invalid := map[string]string{
		"blank name":                     strings.Replace(valid, `"confidence intervals"`, `" "`, 1),
		"missing operations":             `{"name":"test"}`,
		"empty operations":               `{"name":"test","operations":{}}`,
		"null operation config":          `{"operations":{"confidence_interval":null}}`,
		"null body":                      `null`,
		"missing source":                 strings.Replace(valid, `{"eval_job":{"id":"source"}}`, `{}`, 1),
		"null source":                    strings.Replace(valid, `{"eval_job":{"id":"source"}}`, `null`, 1),
		"multiple sources":               strings.Replace(valid, `{"eval_job":{"id":"source"}}`, `{"eval_job":{"id":"source"},"pvc":{"claim_name":"data"}}`, 1),
		"invalid source id":              strings.Replace(valid, `"id":"source"`, `"id":" "`, 1),
		"zero threads":                   strings.Replace(valid, `"id":"source"`, `"id":"source","num_parallel_threads":0`, 1),
		"fractional threads":             strings.Replace(valid, `"id":"source"`, `"id":"source","num_parallel_threads":1.5`, 1),
		"alpha zero":                     strings.Replace(valid, `"significance_level":0.05`, `"significance_level":0`, 1),
		"alpha one":                      strings.Replace(valid, `"significance_level":0.05`, `"significance_level":1`, 1),
		"missing alpha":                  strings.Replace(valid, `"significance_level":0.05`, "", 1),
		"empty calibration":              `{"operations":{"confidence_interval":{"results_data_ref":{"eval_job":{"id":"source"}},"calibration_data_ref":[],"significance_level":0.05}}}`,
		"eval job calibration":           strings.Replace(valid, `"pvc":{"claim_name":"calibration-data"}`, `"eval_job":{"id":"source"}`, 1),
		"mlflow calibration":             strings.Replace(valid, `"pvc":{"claim_name":"calibration-data"}`, `"mlflow":{"run_id":"run","artifact_path":"results"}`, 1),
		"missing data config":            strings.Replace(valid, `,"data_config":{"format":"jsonl","columns":{"label":"human","prediction":"judge"}}`, "", 1),
		"invalid hardware":               strings.Replace(valid, `"cpu":{"request":"500m"}`, `"hardware_profile_name":"cpu-profile","cpu":{"request":"500m"}`, 1),
		"external missing primary score": strings.Replace(postProcessingBody(`{"oci":{"coordinates":{"oci_host":"quay.io","oci_repository":"repo"},"artifact_path":"results"}}`), `,"primary_score":{"metric":"accuracy"}`, "", 1),
		"invalid digest":                 postProcessingBody(`{"oci":{"coordinates":{"oci_host":"quay.io","oci_repository":"repo"},"artifact_path":"results","digest":"abc"}}`),
		"s3 missing bucket":              postProcessingBody(`{"s3":{"key":"results","secret_ref":"s3-secret"}}`),
		"pvc invalid name":               postProcessingBody(`{"pvc":{"claim_name":"invalid claim name"}}`),
		"git missing ref":                postProcessingBody(`{"git":{"url":"https://github.com/example/results.git"}}`),
		"hf missing repo":                postProcessingBody(`{"hf":{"revision":"main"}}`),
		"mlflow missing path":            postProcessingBody(`{"mlflow":{"run_id":"run-1"}}`),
		"oci missing repo":               postProcessingBody(`{"oci":{"coordinates":{"oci_host":"quay.io"},"artifact_path":"results"}}`),
		"extra json":                     valid + `{}`,
	}
	handler, _, runtime := newPostProcessingServer(t)
	for name, body := range invalid {
		t.Run(name, func(t *testing.T) {
			response := postProcessingRequest(handler, http.MethodPost, postProcessingPath, body)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status %d: %s", response.Code, response.Body.String())
			}
			if runtime.job != nil {
				t.Fatal("invalid request launched a computation")
			}
		})
	}
}

func TestPostProcessingIgnoresUnknownFields(t *testing.T) {
	handler, store, _ := newPostProcessingServer(t)
	source := createPostProcessingSource(t, store, api.OverallStateCompleted)
	body := postProcessingBody(fmt.Sprintf(`{"eval_job":{"id":%q,"future_source_option":true}}`, source.Resource.ID))
	body = strings.Replace(body, `"pvc":{"claim_name":"calibration-data"}`, `"pvc":{"claim_name":"calibration-data"},"resolved_sha":"abc"`, 1)
	body = strings.Replace(body, `"name":`, `"future_request_option":true,"name":`, 1)
	body = strings.Replace(body, `"results_data_ref":`, `"future_config_option":true,"num_parallel_threads":99,"results_data_ref":`, 1)
	response := postProcessingRequest(handler, http.MethodPost, postProcessingPath, body)
	if response.Code != http.StatusAccepted {
		t.Fatalf("unknown fields rejected: %d: %s", response.Code, response.Body.String())
	}
	var result api.PostProcessingResource
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Operations.ConfidenceInterval.ResultsDataRef.EvalJob.NumParallelThreads != nil {
		t.Fatal("misplaced num_parallel_threads must be ignored")
	}
	if strings.Contains(response.Body.String(), "future_") {
		t.Fatal("unknown fields should not be persisted or echoed")
	}
}

func TestEvaluationCreationErrorsPreserveRequestID(t *testing.T) {
	for _, tc := range []struct {
		name       string
		path       string
		body       string
		runtimeErr error
	}{
		{
			name: "evaluation MLflow error",
			path: "/api/v1/evaluations/jobs",
			body: `{"name":"evaluation","model":{"name":"model","url":"https://model.example"},"benchmarks":[{"id":"ordinary-benchmark","provider_id":"ordinary-provider"}],"experiment":{"name":"experiment"}}`,
		},
		{
			name:       "post-processing runtime error",
			path:       postProcessingPath,
			body:       postProcessingBody(`{"pvc":{"claim_name":"results"}}`),
			runtimeErr: errors.New("runtime could not start"),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler, _, runtime := newPostProcessingServer(t)
			runtime.err = tc.runtimeErr
			const requestID = "evaluation-create-request-id"
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			req.Header.Set("X-Tenant", "tenant-a")
			req.Header.Set("X-User", "user-a")
			req.Header.Set(server.TransactionIDHeader, requestID)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			if response.Code < 400 {
				t.Fatalf("expected creation error, got %d", response.Code)
			}
			var result api.Error
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Trace != requestID || response.Header().Get(server.TransactionIDHeader) != requestID {
				t.Fatalf("request ID was lost: header=%q body=%s", response.Header().Get(server.TransactionIDHeader), response.Body.String())
			}
			if tc.runtimeErr == nil && result.MessageCode != "mlflow_required_for_experiment" {
				t.Fatalf("expected MLflow configuration error, got %s", response.Body.String())
			}
			if tc.runtimeErr != nil && runtime.job == nil {
				t.Fatal("request did not reach runtime launch")
			}
		})
	}
}

func TestPostProcessingRuntimeFailure(t *testing.T) {
	handler, store, runtime := newPostProcessingServer(t)
	runtime.err = errors.New("runtime could not start")
	response := postProcessingRequest(handler, http.MethodPost, postProcessingPath, postProcessingBody(`{"pvc":{"claim_name":"results"}}`))
	if response.Code < 400 || runtime.job == nil {
		t.Fatalf("unexpected startup failure response %d: %s", response.Code, response.Body.String())
	}
	job, err := store.GetEvaluationJob(runtime.job.Resource.ID)
	if err != nil || job.Status.State != api.OverallStateFailed {
		t.Fatalf("startup failure was not persisted: %v, %+v", err, job)
	}
}

func TestPostProcessingMethodsAndIdentity(t *testing.T) {
	handler, _, _ := newPostProcessingServer(t)
	for _, method := range []string{http.MethodGet, http.MethodDelete, http.MethodPut} {
		response := postProcessingRequest(handler, method, postProcessingPath, "")
		if response.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s returned %d", method, response.Code)
		}
	}
	for _, missing := range []string{"X-Tenant", "X-User"} {
		req := httptest.NewRequest(http.MethodPost, postProcessingPath, strings.NewReader(postProcessingBody(`{"pvc":{"claim_name":"results"}}`)))
		req.Header.Set("X-Tenant", "tenant-a")
		req.Header.Set("X-User", "user-a")
		req.Header.Del(missing)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code < 400 {
			t.Fatalf("missing %s accepted", missing)
		}
	}
}
