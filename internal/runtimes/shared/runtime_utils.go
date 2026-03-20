package shared

import (
	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/common"
	"github.com/eval-hub/eval-hub/internal/messages"
	"github.com/eval-hub/eval-hub/internal/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// ResolveBenchmarks returns the benchmarks to run: from the job's Collection when set, otherwise from the job's Benchmarks.
func ResolveBenchmarks(evaluation *api.EvaluationJobResource, storage abstractions.Storage) ([]api.BenchmarkConfig, error) {
	if evaluation.Collection != nil && evaluation.Collection.ID != "" {
		collection, err := common.ResolveCollection(evaluation.Collection.ID, storage)
		if err != nil {
			return nil, err
		}
		if collection == nil || len(collection.Benchmarks) == 0 {
			return nil, serviceerrors.NewServiceError(messages.CollectionEmpty, "CollectionID", evaluation.Collection.ID)
		}
		return collection.Benchmarks, nil
	}
	if len(evaluation.Benchmarks) == 0 {
		return nil, serviceerrors.NewServiceError(messages.EvaluationJobEmpty, "EvaluationJobID", evaluation.Resource.ID)
	}
	return evaluation.Benchmarks, nil
}
