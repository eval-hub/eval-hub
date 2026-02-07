package storage_test

import (
	"encoding/json"
	"maps"
	"net/url"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/logging"
	"github.com/eval-hub/eval-hub/internal/storage"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/go-playground/validator/v10"
)

type testRequestWrapper struct {
	method  string
	uri     *url.URL
	headers map[string]string
}

func (r *testRequestWrapper) Method() string {
	return r.method
}

func (r *testRequestWrapper) URI() string {
	return r.uri.String()
}

func (r *testRequestWrapper) Header(key string) string {
	return r.headers[key]
}

func (r *testRequestWrapper) SetHeader(key string, value string) {
	r.headers[key] = value
}

func (r *testRequestWrapper) Path() string {
	return r.uri.Path
}

func (r *testRequestWrapper) Query(key string) []string {
	return r.uri.Query()[key]
}

func (r *testRequestWrapper) BodyAsBytes() ([]byte, error) {
	return nil, nil
}

func (r *testRequestWrapper) PathValue(name string) string {
	return ""
}

// TestStorage tests the storage implementation and provides
// a simple way to debug the storage implementation.
func TestStorage(t *testing.T) {
	var logger = logging.FallbackLogger()
	var store abstractions.Storage
	var evaluationId string

	var benchmarkConfig = api.BenchmarkConfig{
		Ref:        api.Ref{ID: "bench-1"},
		ProviderID: "garak",
	}

	t.Run("NewStorage creates a new storage instance", func(t *testing.T) {
		databaseConfig := map[string]any{}
		databaseConfig["driver"] = "sqlite"
		databaseConfig["url"] = "file::memory:?mode=memory&cache=shared"
		databaseConfig["database_name"] = "eval_hub"
		s, err := storage.NewStorage(&databaseConfig, logger)
		if err != nil {
			t.Fatalf("Failed to create storage: %v", err)
		}
		store = s.WithLogger(logger)
	})

	t.Run("CreateEvaluationJob creates a new evaluation job", func(t *testing.T) {
		job := &api.EvaluationJobConfig{
			Model: api.ModelRef{
				URL:  "http://test.com",
				Name: "test",
			},
			Benchmarks: []api.BenchmarkConfig{benchmarkConfig},
		}
		resp, err := store.CreateEvaluationJob(job, "")
		if err != nil {
			t.Fatalf("Failed to create evaluation job: %v", err)
		}
		evaluationId = resp.Resource.ID
		if evaluationId == "" {
			t.Fatalf("Evaluation ID is empty")
		}
	})

	t.Run("GetEvaluationJob returns the evaluation job", func(t *testing.T) {
		resp, err := store.GetEvaluationJob(evaluationId)
		if err != nil {
			t.Fatalf("Failed to get evaluation job: %v", err)
		}
		if resp.Resource.ID != evaluationId {
			t.Fatalf("Evaluation ID mismatch: %s != %s", resp.Resource.ID, evaluationId)
		}
	})

	t.Run("GetEvaluationJobs returns the evaluation jobs", func(t *testing.T) {
		resp, err := store.GetEvaluationJobs(10, 0, "")
		if err != nil {
			t.Fatalf("Failed to get evaluation jobs: %v", err)
		}
		if len(resp.Items) == 0 {
			t.Fatalf("No evaluation jobs found")
		}
	})

	t.Run("UpdateEvaluationJob updates the evaluation job", func(t *testing.T) {
		metrics := map[string]any{
			"metric-1": 1.0,
			"metric-2": 2.0,
		}
		now := time.Now()
		status := &api.StatusEvent{
			BenchmarkStatusEvent: &api.BenchmarkStatus{
				ID:         benchmarkConfig.ID,
				ProviderID: benchmarkConfig.ProviderID,
				// the job status needs to be completed to update the metrics and artifacts
				Status:      api.StateCompleted,
				CompletedAt: api.DateTimeToString(now),
				Metrics:     metrics,
				Artifacts:   map[string]any{},
				ErrorMessage: &api.MessageInfo{
					Message:     "Test error message",
					MessageCode: "TEST_ERROR_MESSAGE",
				},
			},
		}
		completedAtStr := status.BenchmarkStatusEvent.CompletedAt
		if completedAtStr == "" {
			t.Fatalf("CompletedAt is empty")
		}
		val := validator.New()
		err := val.Struct(status)
		if err != nil {
			t.Fatalf("Failed to validate status: %v", err)
		}
		err = store.UpdateEvaluationJob(evaluationId, status)
		if err != nil {
			t.Fatalf("Failed to update evaluation job: %v", err)
		}

		// now get the evaluation job and check the updated values
		job, err := store.GetEvaluationJob(evaluationId)
		if err != nil {
			t.Fatalf("Failed to get evaluation job: %v", err)
		}
		js, err := json.MarshalIndent(job, "", "  ")
		if err != nil {
			t.Fatalf("Failed to marshal job: %v", err)
		}
		t.Logf("Job: %s\n", string(js))
		if len(job.Results.Benchmarks) == 0 {
			t.Fatalf("No benchmarks found")
		}
		if !maps.Equal(job.Results.Benchmarks[0].Metrics, metrics) {
			t.Fatalf("Metrics mismatch: %v != %v", job.Results.Benchmarks[0].Metrics, metrics)
		}

		/* TODO later when the status updates are correct
		if job.Results.Benchmarks[0].CompletedAt == "" {
			t.Fatalf("CompletedAt is nil")
		}
		completedAt, err := api.DateTimeFromString(job.Results.Benchmarks[0].CompletedAt)
		if err != nil {
			t.Fatalf("Failed to convert CompletedAt to time: %v", err)
		}
		if completedAt.UnixMilli() != now.UnixMilli() {
			t.Fatalf("CompletedAt mismatch: %v != %v", job.Results.Benchmarks[0].CompletedAt, now)
		}
		*/
	})

	t.Run("DeleteEvaluationJob deletes the evaluation job", func(t *testing.T) {
		err := store.DeleteEvaluationJob(evaluationId, false)
		if err != nil {
			t.Fatalf("Failed to delete evaluation job: %v", err)
		}
	})
}
