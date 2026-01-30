package handlers_test

import (
	"testing"

	"github.com/eval-hub/eval-hub/internal/handlers"
)

func TestGetEvaluations(t *testing.T) {
	t.Run("check exraction of id from path", func(t *testing.T) {
		paths := [][]string{
			{"/api/v1/evaluations/jobs/1", "1"},
			{"/api/v1/evaluations/jobs/1?hard_delete=true", "1"},
			{"/api/v1/evaluations/jobs/1/update", "1"},
			{"/api/v1/evaluations/jobs/123", "123"},
			{"/api/v1/evaluations/jobs/123?hard_delete=true", "123"},
			{"/api/v1/evaluations/jobs/123/update", "123"},
		}
		for _, path := range paths {
			id := handlers.GetEvaluationJobID(createExecutionContext("GET", path[0]))
			if id != path[1] {
				t.Errorf("expected %s, got %s", path[1], id)
			}
		}
	})
}
