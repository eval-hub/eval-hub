package common

import (
	"fmt"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/messages"
	"github.com/eval-hub/eval-hub/internal/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/google/uuid"
)

func GUID() string {
	return uuid.New().String()
}

// ResolveProvider returns the provider for providerID from the in-memory map, or from storage if not present and storage is non-nil.
func ResolveProvider(providerID string, providers map[string]api.ProviderResource, storage abstractions.Storage) (*api.ProviderResource, error) {
	if p, ok := providers[providerID]; ok {
		return &p, nil
	}
	if storage != nil {
		p, err := storage.GetProvider(providerID)
		if err != nil {
			return nil, fmt.Errorf("get provider %s: %w", providerID, err)
		}
		if p != nil {
			return p, nil
		}
	}
	return nil, fmt.Errorf("provider %q not found", providerID)
}

// GetCollectionFunc returns a collection by ID. Used to resolve job benchmarks from collection without depending on storage.
type GetCollectionFunc func(id string) (*api.CollectionResource, error)

// GetJobBenchmarks returns the effective benchmark list for a job: from the job's collection when set, otherwise from job.Benchmarks.
func GetJobBenchmarks(job *api.EvaluationJobResource, getCollection GetCollectionFunc) ([]api.BenchmarkConfig, error) {
	if job != nil && job.Collection != nil && job.Collection.ID != "" {
		if getCollection == nil {
			return nil, serviceerrors.NewServiceError(
				messages.InternalServerError,
				"ParameterName", "Error",
				"Value", "Error while fetching the collection",
			)
		}
		coll, err := getCollection(job.Collection.ID)
		if err != nil || coll == nil {
			return nil, serviceerrors.NewServiceError(
				messages.ResourceNotFound,
				"ParameterName", "Type",
				"Value", "Collection",
				"ParameterName", "ResourceId",
				"Value", job.Collection.ID,
			)
		}
		if len(coll.Benchmarks) == 0 {
			return nil, serviceerrors.NewServiceError(
				messages.CollectionEmpty,
				"CollectionID", job.Collection.ID,
			)
		}
		return coll.Benchmarks, nil
	}
	if len(job.Benchmarks) == 0 {
		return nil, serviceerrors.NewServiceError(
			messages.EvaluationJobEmpty,
			"EvaluationJobID", job.Resource.ID,
		)
	}
	return job.Benchmarks, nil
}
