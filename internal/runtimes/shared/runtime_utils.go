package shared

import (
	"fmt"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// ResolveBenchmarks returns the benchmarks to run: from the job's Collection when set (via storage.GetCollection), otherwise from the job's Benchmarks.
// If evaluation.Collection is set, storage must be non-nil and *storage must be non-nil.
func ResolveBenchmarks(evaluation *api.EvaluationJobResource, storage *abstractions.Storage) ([]api.BenchmarkConfig, error) {
	if evaluation.Collection != nil {
		if storage == nil || *storage == nil {
			return nil, fmt.Errorf("collection is set but storage is not available for job %s", evaluation.Resource.ID)
		}
		collection, err := (*storage).GetCollection(evaluation.Collection.ID)
		if err != nil {
			return nil, fmt.Errorf("get collection %s for job %s: %w", evaluation.Collection.ID, evaluation.Resource.ID, err)
		}
		if len(collection.Benchmarks) == 0 {
			return nil, fmt.Errorf("collection %s has no benchmarks for job %s", evaluation.Collection.ID, evaluation.Resource.ID)
		}
		return collection.Benchmarks, nil
	}
	if len(evaluation.Benchmarks) == 0 {
		return nil, fmt.Errorf("no benchmarks configured for job %s", evaluation.Resource.ID)
	}
	return evaluation.Benchmarks, nil
}
