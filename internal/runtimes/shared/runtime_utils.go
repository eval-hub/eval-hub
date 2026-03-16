package shared

import (
	"fmt"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/common"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// ResolveBenchmarks returns the benchmarks to run: from the job's Collection when set, otherwise from the job's Benchmarks.
// Collections are resolved first from the in-memory collectionConfigs, then from storage.
func ResolveBenchmarks(evaluation *api.EvaluationJobResource, collectionConfigs map[string]api.CollectionResource, storage abstractions.Storage) ([]api.BenchmarkConfig, error) {
	if evaluation.Collection != nil && evaluation.Collection.ID != "" {
		collection, err := common.ResolveCollection(evaluation.Collection.ID, collectionConfigs, storage)
		if err != nil {
			return nil, fmt.Errorf("get collection %s for job %s: %w", evaluation.Collection.ID, evaluation.Resource.ID, err)
		}
		if collection == nil || len(collection.Benchmarks) == 0 {
			return nil, fmt.Errorf("collection %s has no benchmarks for job %s", evaluation.Collection.ID, evaluation.Resource.ID)
		}
		return collection.Benchmarks, nil
	}
	if len(evaluation.Benchmarks) == 0 {
		return nil, fmt.Errorf("no benchmarks configured for job %s", evaluation.Resource.ID)
	}
	return evaluation.Benchmarks, nil
}
