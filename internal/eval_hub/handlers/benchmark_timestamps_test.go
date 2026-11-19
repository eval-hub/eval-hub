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
	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/handlers"
	"github.com/eval-hub/eval-hub/internal/testhelpers"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestHandleCreateEvaluationInitializesPendingBenchmarks(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		runtime abstractions.Runtime
		code    int
	}{
		{"without runtime", nil, http.StatusAccepted},
		{"runtime accepted", &fakeRuntime{}, http.StatusAccepted},
		{"runtime failed", &fakeRuntime{err: errors.New("runtime failed")}, http.StatusInternalServerError},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			storage := &collectionRunCountStorage{fakeStorage: &fakeStorage{providerConfigs: map[string]api.ProviderResource{
				"provider": {ProviderConfig: api.ProviderConfig{Benchmarks: []api.BenchmarkResource{{ID: "repeated"}}}},
			}}}
			h := handlers.New(storage, testhelpers.NewValidator(t), scenario.runtime, nil, nil, nil, nil)
			req := &bodyRequest{
				MockRequest: createMockRequest("POST", "/api/v1/evaluations/jobs"),
				body: []byte(`{"name":"timestamps","model":{"name":"test","url":"http://model.example"},"benchmarks":[
					{"id":"repeated","provider_id":"provider"},{"id":"repeated","provider_id":"provider"}]}`),
			}
			ctx := executioncontext.NewExecutionContext(context.Background(), "timestamps", slog.New(slog.NewTextHandler(io.Discard, nil)), "user", "tenant")
			response := httptest.NewRecorder()
			h.HandleCreateEvaluation(ctx, req, MockResponseWrapper{recorder: response})
			if response.Code != scenario.code {
				t.Fatalf("HTTP %d, want %d: %s", response.Code, scenario.code, response.Body.String())
			}
			if storage.createdJob == nil {
				t.Fatal("job was not persisted")
			}
			assertInitialBenchmarkStatuses(t, storage.createdJob, storage.createdJob.Benchmarks)
			if response.Code == http.StatusAccepted {
				var job api.EvaluationJobResource
				if err := json.Unmarshal(response.Body.Bytes(), &job); err != nil {
					t.Fatal(err)
				}
				assertInitialBenchmarkStatuses(t, &job, job.Benchmarks)
			}
		})
	}
}

func assertInitialBenchmarkStatuses(t *testing.T, job *api.EvaluationJobResource, benchmarks []api.EvaluationBenchmarkConfig) {
	t.Helper()
	if job.Status == nil || len(job.Status.Benchmarks) != len(benchmarks) {
		t.Fatalf("missing pending benchmark statuses: %+v", job)
	}
	for index, benchmark := range benchmarks {
		status := job.Status.Benchmarks[index]
		if status.ID != benchmark.ID || status.ProviderID != benchmark.ProviderID || status.BenchmarkIndex != index ||
			status.Status != api.StatePending || status.StartedAt != "" || status.CompletedAt != "" {
			t.Errorf("benchmark %d was not initialized as pending: %+v", index, status)
		}
	}
}
