package handlers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/handlers"
	"github.com/eval-hub/eval-hub/internal/testhelpers"
	"github.com/eval-hub/eval-hub/pkg/api"
)

const repeatedBFCLCollectionJSON = `[
  {"provider_id":"inspect","id":"inspect/bfcl","weight":0.7,
   "primary_score":{"metric":"bfcl_scorer/accuracy"},"pass_criteria":{"threshold":0.6},
   "parameters":{"num_examples":50,"task_args":{"categories":["simple_python","parallel"]}}},
  {"provider_id":"inspect","id":"inspect/bfcl","weight":0.3,
   "primary_score":{"metric":"bfcl_scorer/accuracy"},"pass_criteria":{"threshold":0.4},
   "parameters":{"num_examples":50,"task_args":{"categories":["irrelevance","live_relevance"]}}}
]`

type repeatedBenchmarkOverrideCase struct {
	name           string
	collectionJSON string
	overridesJSON  string
	// Expected parameters are written independently of collection/request maps.
	wantParametersJSON string
}

func repeatedBenchmarkOverrideCases() []repeatedBenchmarkOverrideCase {
	return []repeatedBenchmarkOverrideCase{
		{
			name:           "single override broadcasts without replacing category defaults",
			collectionJSON: repeatedBFCLCollectionJSON,
			overridesJSON:  `[{"provider_id":"inspect","id":"inspect/bfcl","parameters":{"num_examples":3}}]`,
			wantParametersJSON: `[
			  {"num_examples":3,"task_args":{"categories":["simple_python","parallel"]}},
			  {"num_examples":3,"task_args":{"categories":["irrelevance","live_relevance"]}}
			]`,
		},
		{
			name:           "empty parameter removal is scoped to its occurrence",
			collectionJSON: repeatedBFCLCollectionJSON,
			overridesJSON: `[
			  {"provider_id":"inspect","id":"inspect/bfcl","parameters":{"task_args":null,"num_examples":0}},
			  {"provider_id":"inspect","id":"inspect/bfcl","parameters":{"num_examples":""}}
			]`,
			wantParametersJSON: `[
			  {"num_examples":0},
			  {"task_args":{"categories":["irrelevance","live_relevance"]}}
			]`,
		},
		{
			name: "interleaved repeated pairs match within each pair",
			collectionJSON: `[
			  {"provider_id":"inspect","id":"inspect/bfcl","parameters":{"keep":"first"}},
			  {"provider_id":"other","id":"inspect/bfcl","parameters":{"keep":"other provider"}},
			  {"provider_id":"inspect","id":"inspect/bfcl","parameters":{"keep":"second"}},
			  {"provider_id":"other","id":"inspect/bfcl","parameters":{"keep":"other second"}}
			]`,
			overridesJSON: `[
			  {"provider_id":"other","id":"inspect/bfcl","parameters":{"num_examples":7}},
			  {"provider_id":"inspect","id":"inspect/bfcl","parameters":{"num_examples":1}},
			  {"provider_id":"other","id":"inspect/bfcl","parameters":{"num_examples":9}},
			  {"provider_id":"inspect","id":"inspect/bfcl","parameters":{"num_examples":2}}
			]`,
			wantParametersJSON: `[
			  {"keep":"first","num_examples":1},
			  {"keep":"other provider","num_examples":7},
			  {"keep":"second","num_examples":2},
			  {"keep":"other second","num_examples":9}
			]`,
		},
		{
			name:           "unique benchmark retains sequential override merging",
			collectionJSON: `[{"provider_id":"inspect","id":"inspect/bfcl","parameters":{"keep":true}}]`,
			overridesJSON: `[
			  {"provider_id":"inspect","id":"inspect/bfcl","parameters":{"num_examples":1,"first":true}},
			  {"provider_id":"inspect","id":"inspect/bfcl","parameters":{"num_examples":2}}
			]`,
			wantParametersJSON: `[{"keep":true,"num_examples":2,"first":true}]`,
		},
		{
			name:           "BFCL collection defaults",
			collectionJSON: repeatedBFCLCollectionJSON,
			overridesJSON:  `[]`,
			wantParametersJSON: `[
			  {"num_examples":50,"task_args":{"categories":["simple_python","parallel"]}},
			  {"num_examples":50,"task_args":{"categories":["irrelevance","live_relevance"]}}
			]`,
		},
		{
			name:           "BFCL existing parameters round trip",
			collectionJSON: repeatedBFCLCollectionJSON,
			overridesJSON: `[
			  {"provider_id":"inspect","id":"inspect/bfcl","parameters":{"num_examples":50,"task_args":{"categories":["simple_python","parallel"]}}},
			  {"provider_id":"inspect","id":"inspect/bfcl","parameters":{"num_examples":50,"task_args":{"categories":["irrelevance","live_relevance"]}}}
			]`,
			wantParametersJSON: `[
			  {"num_examples":50,"task_args":{"categories":["simple_python","parallel"]}},
			  {"num_examples":50,"task_args":{"categories":["irrelevance","live_relevance"]}}
			]`,
		},
		{
			name:           "BFCL distinct changed limits and categories",
			collectionJSON: repeatedBFCLCollectionJSON,
			overridesJSON: `[
			  {"provider_id":"inspect","id":"inspect/bfcl","parameters":{"num_examples":1,"task_args":{"categories":["simple_java"]}}},
			  {"provider_id":"inspect","id":"inspect/bfcl","parameters":{"num_examples":2,"task_args":{"categories":["live_irrelevance"]}}}
			]`,
			wantParametersJSON: `[
			  {"num_examples":1,"task_args":{"categories":["simple_java"]}},
			  {"num_examples":2,"task_args":{"categories":["live_irrelevance"]}}
			]`,
		},
		{
			name:           "BFCL limit-only overrides retain category defaults",
			collectionJSON: repeatedBFCLCollectionJSON,
			overridesJSON: `[
			  {"provider_id":"inspect","id":"inspect/bfcl","parameters":{"num_examples":1}},
			  {"provider_id":"inspect","id":"inspect/bfcl","parameters":{"num_examples":2}}
			]`,
			wantParametersJSON: `[
			  {"num_examples":1,"task_args":{"categories":["simple_python","parallel"]}},
			  {"num_examples":2,"task_args":{"categories":["irrelevance","live_relevance"]}}
			]`,
		},
		{
			name: "Inspect custom distinct changed tasks",
			collectionJSON: `[
			  {"provider_id":"inspect","id":"inspect/custom","parameters":{"task":"inspect_evals/gsm8k","task_args":{"fewshot":0},"num_examples":50}},
			  {"provider_id":"inspect","id":"inspect/custom","parameters":{"task":"inspect_evals/mmlu_pro","task_args":{"fewshot":5},"num_examples":50}}
			]`,
			overridesJSON: `[
			  {"provider_id":"inspect","id":"inspect/custom","parameters":{"task":"inspect_evals/humaneval","task_args":{"fewshot":1},"num_examples":3}},
			  {"provider_id":"inspect","id":"inspect/custom","parameters":{"task":"inspect_evals/triviaqa","task_args":{"fewshot":2},"num_examples":4}}
			]`,
			wantParametersJSON: `[
			  {"task":"inspect_evals/humaneval","task_args":{"fewshot":1},"num_examples":3},
			  {"task":"inspect_evals/triviaqa","task_args":{"fewshot":2},"num_examples":4}
			]`,
		},
		{
			name: "partial reordered unique overrides leave interleaved duplicates unchanged",
			collectionJSON: `[
			  {"provider_id":"inspect","id":"inspect/bfcl","parameters":{"task_args":{"categories":["simple_python"]},"num_examples":50}},
			  {"provider_id":"inspect","id":"inspect/other","parameters":{"num_examples":50,"keep":"unique"}},
			  {"provider_id":"inspect","id":"inspect/bfcl","parameters":{"task_args":{"categories":["irrelevance"]},"num_examples":50}},
			  {"provider_id":"other","id":"inspect/bfcl","parameters":{"num_examples":50,"keep":"other provider"}}
			]`,
			overridesJSON: `[
			  {"provider_id":"other","id":"inspect/bfcl","parameters":{"num_examples":9}},
			  {"provider_id":"inspect","id":"inspect/other","parameters":{"num_examples":7}}
			]`,
			wantParametersJSON: `[
			  {"task_args":{"categories":["simple_python"]},"num_examples":50},
			  {"num_examples":7,"keep":"unique"},
			  {"task_args":{"categories":["irrelevance"]},"num_examples":50},
			  {"num_examples":9,"keep":"other provider"}
			]`,
		},
	}
}

// Repeated pairs match by occurrence, while unique pairs remain order-free and
// a single override retains broadcast behavior for compatibility.
func TestGetJobBenchmarks_RepeatedRowsPreserveParameters(t *testing.T) {
	t.Parallel()
	for _, tc := range repeatedBenchmarkOverrideCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			collection, job, want := newRepeatedBenchmarkFixture(t, tc)
			collectionBefore := benchmarkJSON(t, collection)
			requestBefore := benchmarkJSON(t, job)
			wantBefore := benchmarkJSON(t, want)

			got, err := handlers.GetJobBenchmarks(job, collection)

			assertBenchmarkJSON(t, "collection must not change", collection, collectionBefore)
			assertBenchmarkJSON(t, "request must not change", job, requestBefore)
			if err != nil {
				t.Fatalf("GetJobBenchmarks: %v", err)
			}
			assertBenchmarkJSON(t, "effective benchmarks", got, wantBefore)
		})
	}
}

// The POST response retains the submitted overrides, so checking it alone would
// miss this bug. Assert the effective list passed to the runtime as well as the
// original collection and persisted request.
func TestHandleCreateEvaluation_RepeatedBenchmarkOverridesReachRuntime(t *testing.T) {
	for _, tc := range repeatedBenchmarkOverrideCases() {
		t.Run(tc.name, func(t *testing.T) {
			collection, job, want := newRepeatedBenchmarkFixture(t, tc)
			collectionBefore := benchmarkJSON(t, collection)
			overridesBefore := benchmarkJSON(t, job.Collection.Benchmarks)
			wantBefore := benchmarkJSON(t, want)
			providers := make(map[string]api.ProviderResource)
			for _, benchmark := range collection.Benchmarks {
				provider := providers[benchmark.ProviderID]
				provider.Resource.ID = benchmark.ProviderID
				provider.Benchmarks = append(provider.Benchmarks, api.BenchmarkResource{ID: benchmark.ID})
				providers[benchmark.ProviderID] = provider
			}
			storage := &collectionRunCountStorage{
				fakeStorage: &fakeStorage{providerConfigs: providers},
				collection:  collection,
			}
			runtime := &repeatedBenchmarkCaptureRuntime{}
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			h := handlers.New(storage, testhelpers.NewValidator(t), runtime, nil, nil, nil, nil)
			req := &bodyRequest{
				MockRequest: createMockRequest("POST", "/api/v1/evaluations/jobs"),
				body:        []byte(benchmarkJSON(t, job.EvaluationJobConfig)),
			}
			recorder := httptest.NewRecorder()
			ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, "test-user", "test-tenant")

			h.HandleCreateEvaluation(ctx, req, MockResponseWrapper{recorder: recorder})

			assertBenchmarkJSON(t, "collection must not change", collection, collectionBefore)
			if recorder.Code != http.StatusAccepted {
				t.Fatalf("status = %d, want %d; body: %s", recorder.Code, http.StatusAccepted, recorder.Body.String())
			}
			if storage.createdJob == nil {
				t.Fatal("expected a persisted job")
			}
			assertBenchmarkJSON(t, "persisted overrides", storage.createdJob.Collection.Benchmarks, overridesBefore)
			if runtime.runCalls != 1 {
				t.Fatalf("runtime calls = %d, want 1", runtime.runCalls)
			}
			assertBenchmarkJSON(t, "runtime benchmarks", runtime.benchmarks, wantBefore)
		})
	}
}

func TestGetJobBenchmarks_RepeatedOverridesKeepHardwareAndTestDataScoped(t *testing.T) {
	tc := repeatedBenchmarkOverrideCase{
		collectionJSON: `[
		  {"provider_id":"inspect","id":"inspect/bfcl","test_data_ref":{"pvc":{"claim_name":"default-first"}}},
		  {"provider_id":"inspect","id":"inspect/bfcl","test_data_ref":{"pvc":{"claim_name":"default-second"}}}
		]`,
		overridesJSON: `[
		  {"provider_id":"inspect","hardware_config":{"hardware_profile_name":"provider-default"}},
		  {"provider_id":"inspect","id":"inspect/bfcl","hardware_config":{"hardware_profile_name":"first-profile"},"test_data_ref":{"pvc":{"claim_name":"first-override"}}},
		  {"provider_id":"inspect","id":"inspect/bfcl"}
		]`,
		wantParametersJSON: `[{},{}]`,
	}
	collection, job, _ := newRepeatedBenchmarkFixture(t, tc)
	collectionBefore, requestBefore := benchmarkJSON(t, collection), benchmarkJSON(t, job)
	got, err := handlers.GetJobBenchmarks(job, collection)
	if err != nil {
		t.Fatal(err)
	}
	var want []api.EvaluationBenchmarkConfig
	if err := json.Unmarshal([]byte(`[
	  {"provider_id":"inspect","id":"inspect/bfcl","hardware_config":{"hardware_profile_name":"first-profile"},"test_data_ref":{"pvc":{"claim_name":"first-override"}}},
	  {"provider_id":"inspect","id":"inspect/bfcl","hardware_config":{"hardware_profile_name":"provider-default"},"test_data_ref":{"pvc":{"claim_name":"default-second"}}}
	]`), &want); err != nil {
		t.Fatal(err)
	}
	assertBenchmarkJSON(t, "effective hardware and test data", got, benchmarkJSON(t, want))
	assertBenchmarkJSON(t, "collection must not change", collection, collectionBefore)
	assertBenchmarkJSON(t, "request must not change", job, requestBefore)
}

func TestRepeatedBenchmarkOverridesRejectAmbiguousCountsBeforeCreatingJob(t *testing.T) {
	for _, counts := range [][2]int{{3, 2}, {2, 3}} {
		t.Run(fmt.Sprintf("%d rows %d overrides", counts[0], counts[1]), func(t *testing.T) {
			collection := &api.CollectionResource{
				Resource:         api.Resource{ID: "repeated-benchmarks", Owner: "system"},
				CollectionConfig: api.CollectionConfig{Name: "Repeated benchmarks", Category: "test"},
			}
			for range counts[0] {
				collection.Benchmarks = append(collection.Benchmarks, api.CollectionBenchmarkConfig{
					Ref: api.Ref{ID: "inspect/bfcl"}, ProviderID: "inspect",
				})
			}
			job := &api.EvaluationJobResource{EvaluationJobConfig: api.EvaluationJobConfig{
				Name: "ambiguous-overrides", Collection: &api.CollectionRef{ID: collection.Resource.ID},
			}}
			// Decode the same model shape used by successful POST fixtures.
			if err := json.Unmarshal([]byte(`{"url":"http://model.example.com/v1","name":"test-model"}`), &job.Model); err != nil {
				t.Fatal(err)
			}
			for i := range counts[1] {
				job.Collection.Benchmarks = append(job.Collection.Benchmarks, api.EvaluationBenchmarkConfig{
					Ref: api.Ref{ID: "inspect/bfcl"}, ProviderID: "inspect",
					Parameters: map[string]any{"num_examples": i + 1},
				})
			}
			collectionBefore, requestBefore := benchmarkJSON(t, collection), benchmarkJSON(t, job)
			got, err := handlers.GetJobBenchmarks(job, collection)
			// Shared resolution must still accept configurations persisted by
			// older versions, using their previous last-override-wins behavior.
			if err != nil || len(got) != counts[0] {
				t.Fatalf("legacy resolution: got %v, %v", got, err)
			}
			for _, benchmark := range got {
				if benchmark.Parameters["num_examples"] != counts[1] {
					t.Fatalf("legacy parameters = %v, want last override %d", benchmark.Parameters, counts[1])
				}
			}
			storage := &collectionRunCountStorage{fakeStorage: &fakeStorage{}, collection: collection}
			runtime := &repeatedBenchmarkCaptureRuntime{}
			h := handlers.New(storage, testhelpers.NewValidator(t), runtime, nil, nil, nil, nil)
			req := &bodyRequest{
				MockRequest: createMockRequest("POST", "/api/v1/evaluations/jobs"),
				body:        []byte(benchmarkJSON(t, job.EvaluationJobConfig)),
			}
			recorder := httptest.NewRecorder()
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, "test-user", "test-tenant")
			h.HandleCreateEvaluation(ctx, req, MockResponseWrapper{recorder: recorder})
			if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "collection occurrence order") {
				t.Fatalf("expected count-validation 400, got %d: %s", recorder.Code, recorder.Body.String())
			}
			if storage.createdJob != nil || runtime.runCalls != 0 {
				t.Fatal("invalid overrides must not persist a job or invoke the runtime")
			}
			assertBenchmarkJSON(t, "collection must not change", collection, collectionBefore)
			assertBenchmarkJSON(t, "request must not change", job, requestBefore)
		})
	}
}

// Each invocation decodes independent input and expected maps. No mutable
// fixtures are shared across parallel tests, and expected bytes are frozen
// before calling production code.
func newRepeatedBenchmarkFixture(t *testing.T, tc repeatedBenchmarkOverrideCase) (*api.CollectionResource, *api.EvaluationJobResource, []api.EvaluationBenchmarkConfig) {
	t.Helper()
	collection := &api.CollectionResource{
		Resource: api.Resource{ID: "repeated-benchmarks", Owner: "system"},
		CollectionConfig: api.CollectionConfig{
			Name: "Repeated benchmarks", Category: "test",
		},
	}
	if err := json.Unmarshal([]byte(tc.collectionJSON), &collection.Benchmarks); err != nil {
		t.Fatal(err)
	}
	job := &api.EvaluationJobResource{}
	body := fmt.Sprintf(`{"name":"repeated-benchmark-overrides","model":{"url":"http://model.example.com/v1","name":"test-model"},"collection":{"id":"repeated-benchmarks","benchmarks":%s}}`, tc.overridesJSON)
	if err := json.Unmarshal([]byte(body), &job.EvaluationJobConfig); err != nil {
		t.Fatal(err)
	}
	// An empty list is omitted by the API's omitempty tag and decodes as nil.
	if len(job.Collection.Benchmarks) == 0 {
		job.Collection.Benchmarks = nil
	}

	// Decode metadata separately; never derive the expected result from an input
	// map that the merge implementation could mutate during the test.
	var want []api.EvaluationBenchmarkConfig
	if err := json.Unmarshal([]byte(tc.collectionJSON), &want); err != nil {
		t.Fatal(err)
	}
	var parameters []map[string]any
	if err := json.Unmarshal([]byte(tc.wantParametersJSON), &parameters); err != nil {
		t.Fatal(err)
	}
	if len(want) != len(parameters) {
		t.Fatalf("fixture has %d rows but %d expected parameter sets", len(want), len(parameters))
	}
	for i := range want {
		want[i].Parameters = parameters[i]
	}
	return collection, job, want
}

func benchmarkJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func assertBenchmarkJSON(t *testing.T, label string, got any, wantJSON string) {
	t.Helper()
	if actual := benchmarkJSON(t, got); actual != wantJSON {
		t.Errorf("%s = %s; want %s", label, actual, wantJSON)
	}
}

type repeatedBenchmarkCaptureRuntime struct {
	fakeRuntime
	runCalls   int
	benchmarks []api.EvaluationBenchmarkConfig
}

func (r *repeatedBenchmarkCaptureRuntime) WithLogger(_ *slog.Logger) abstractions.Runtime {
	return r
}

func (r *repeatedBenchmarkCaptureRuntime) WithContext(_ context.Context) abstractions.Runtime {
	return r
}

func (r *repeatedBenchmarkCaptureRuntime) RunEvaluationJob(_ *api.EvaluationJobResource, benchmarks []api.EvaluationBenchmarkConfig, _ abstractions.RuntimeStorage) error {
	r.runCalls++
	r.benchmarks = benchmarks
	return nil
}
