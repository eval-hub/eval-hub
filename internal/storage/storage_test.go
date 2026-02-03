package storage_test

import (
	"context"
	"log/slog"
	"net/url"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"github.com/eval-hub/eval-hub/internal/logging"
	"github.com/eval-hub/eval-hub/internal/storage"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/google/uuid"
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

func createExecutionContext(logger *slog.Logger) *executioncontext.ExecutionContext {
	return executioncontext.NewExecutionContext(context.Background(), uuid.New().String(), logger, 60*time.Second)
}

// TestStorage tests the storage implementation and provides
// a simple way to debug the storage implementation.
func TestStorage(t *testing.T) {
	var logger = logging.FallbackLogger()
	var store abstractions.Storage
	var evaluationId string

	t.Run("NewStorage creates a new storage instance", func(t *testing.T) {
		databaseConfig := map[string]any{}
		databaseConfig["driver"] = "sqlite"
		databaseConfig["url"] = "file::memory:?mode=memory&cache=shared"
		databaseConfig["database_name"] = "eval_hub"
		s, err := storage.NewStorage(&databaseConfig, logger)
		if err != nil {
			t.Fatalf("Failed to create storage: %v", err)
		}
		store = s
	})

	t.Run("CreateEvaluationJob creates a new evaluation job", func(t *testing.T) {
		ctx := createExecutionContext(logger)
		job := &api.EvaluationJobConfig{
			Model: api.ModelRef{
				URL:  "http://test.com",
				Name: "test",
			},
		}
		resp, err := store.CreateEvaluationJob(ctx, job)
		if err != nil {
			t.Fatalf("Failed to create evaluation job: %v", err)
		}
		evaluationId = resp.Resource.ID
		if evaluationId == "" {
			t.Fatalf("Evaluation ID is empty")
		}
	})

	t.Run("GetEvaluationJob returns the evaluation job", func(t *testing.T) {
		ctx := createExecutionContext(logger)
		resp, err := store.GetEvaluationJob(ctx, evaluationId)
		if err != nil {
			t.Fatalf("Failed to get evaluation job: %v", err)
		}
		if resp.Resource.ID != evaluationId {
			t.Fatalf("Evaluation ID mismatch: %s != %s", resp.Resource.ID, evaluationId)
		}
	})

	t.Run("GetEvaluationJobs returns the evaluation jobs", func(t *testing.T) {
		ctx := createExecutionContext(logger)
		resp, err := store.GetEvaluationJobs(ctx, 10, 0, "")
		if err != nil {
			t.Fatalf("Failed to get evaluation jobs: %v", err)
		}
		if len(resp.Items) == 0 {
			t.Fatalf("No evaluation jobs found")
		}
	})

	t.Run("DeleteEvaluationJob deletes the evaluation job", func(t *testing.T) {
		ctx := createExecutionContext(logger)
		err := store.DeleteEvaluationJob(ctx, evaluationId, false)
		if err != nil {
			t.Fatalf("Failed to delete evaluation job: %v", err)
		}
	})
}
