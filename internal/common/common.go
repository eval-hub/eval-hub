package common

import (
	"fmt"

	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/google/uuid"
)

func GUID() string {
	return uuid.New().String()
}

// GetCollectionFunc returns a collection by ID. Used to resolve job benchmarks from collection without depending on storage.
type GetCollectionFunc func(id string) (*api.CollectionResource, error)

// GetJobBenchmarks returns the effective benchmark list for a job: from the job's collection when set, otherwise from job.Benchmarks.
func GetJobBenchmarks(job *api.EvaluationJobResource, getCollection GetCollectionFunc) ([]api.BenchmarkConfig, error) {
	if job != nil && job.Collection != nil && job.Collection.ID != "" {
		if getCollection == nil {
			return nil, fmt.Errorf("collection is set but getCollection is not available for job %s", job.Resource.ID)
		}
		coll, err := getCollection(job.Collection.ID)
		if err != nil {
			return nil, fmt.Errorf("get collection %s: %w", job.Collection.ID, err)
		}
		if coll == nil || len(coll.Benchmarks) == 0 {
			return nil, fmt.Errorf("collection %s has no benchmarks", job.Collection.ID)
		}
		return coll.Benchmarks, nil
	}
	if len(job.Benchmarks) == 0 {
		return nil, fmt.Errorf("no benchmarks configured for job %s", job.Resource.ID)
	}
	return job.Benchmarks, nil
}
