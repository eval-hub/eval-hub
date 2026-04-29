package sql

import (
	"database/sql"
	"slices"
	"strings"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// LoadSystemResources reloads system-owned providers and collections into the
// database. It deletes all existing system resources and inserts the new ones,
// preserving CreatedAt/UpdatedAt timestamps for resources that already existed.
func (s *sqlStorage) LoadSystemResources(systemCollections map[string]api.CollectionResource, systemProviders map[string]api.ProviderResource) error {
	s.logger.Info("Loading system resources")

	return s.withTransaction("load-system-resources", "system", func(txn *sql.Tx) error {
		// we take the simplest approach here:
		// 1. delete all existing system resources
		// 2. insert the new system resources
		// Both steps always run so that orphaned records are removed when
		// config files are deleted (empty maps mean "no system resources").
		{
			var deletedCollections []string
			var updatedCollections []string
			var addedCollections []string
			existingCollections := make(map[string]api.CollectionResource)
			cont := true
			offset := 0
			for cont {
				// as we filter with an empty tenant we will only get the system collections
				filter := abstractions.QueryFilter{
					Limit:  200,
					Offset: offset,
					Params: map[string]any{},
				}
				collections, err := s.getCollectionsTransactional(txn, &filter)
				if err != nil {
					return serviceerrors.WithRollback(err)
				}
				for _, collection := range collections.Items {
					// double check that this is a system collection
					if collection.Resource.IsSystemResource() {
						err := s.deleteCollectionTxn(txn, collection.Resource.ID)
						if err != nil {
							return serviceerrors.WithRollback(err)
						}
						deletedCollections = append(deletedCollections, collection.Resource.ID)
						existingCollections[collection.Resource.ID] = collection
					}
				}
				if collections.TotalCount < filter.Limit {
					cont = false
				} else {
					offset += filter.Limit
				}
			}
			for _, collection := range systemCollections {
				// make sure that these are set
				collection.Resource.Tenant = ""
				collection.Resource.Owner = "system"
				if existingCollection, ok := existingCollections[collection.Resource.ID]; ok {
					collection.Resource.CreatedAt = existingCollection.Resource.CreatedAt
					collection.Resource.UpdatedAt = existingCollection.Resource.UpdatedAt
				}
				if collection.Resource.CreatedAt.IsZero() {
					collection.Resource.CreatedAt = time.Now()
				}
				if collection.Resource.UpdatedAt.IsZero() {
					collection.Resource.UpdatedAt = collection.Resource.CreatedAt
				}
				err := s.createCollectionTxn(txn, &collection)
				if err != nil {
					return serviceerrors.WithRollback(err)
				}
				if slices.Contains(deletedCollections, collection.Resource.ID) {
					updatedCollections = append(updatedCollections, collection.Resource.ID)
					deletedCollections = slices.DeleteFunc(deletedCollections, func(id string) bool {
						return id == collection.Resource.ID
					})
				} else {
					addedCollections = append(addedCollections, collection.Resource.ID)
				}
			}
			s.logger.Info("Loaded system collections", "added", strings.Join(addedCollections, ","), "updated", strings.Join(updatedCollections, ","), "deleted", strings.Join(deletedCollections, ","))
		}
		{
			var deletedProviders []string
			var updatedProviders []string
			var addedProviders []string
			existingProviders := make(map[string]api.ProviderResource)
			cont := true
			offset := 0
			for cont {
				// as we filter with an empty tenant we will only get the system providers
				filter := abstractions.QueryFilter{
					Limit:  200,
					Offset: offset,
					Params: map[string]any{},
				}
				providers, err := s.getProvidersTransactional(txn, &filter)
				if err != nil {
					return serviceerrors.WithRollback(err)
				}
				for _, provider := range providers.Items {
					// double check that this is a system provider
					if provider.Resource.IsSystemResource() {
						err := s.deleteProviderTxn(txn, provider.Resource.ID)
						if err != nil {
							return serviceerrors.WithRollback(err)
						}
						deletedProviders = append(deletedProviders, provider.Resource.ID)
						existingProviders[provider.Resource.ID] = provider
					}
				}
				if providers.TotalCount < filter.Limit {
					cont = false
				} else {
					offset += filter.Limit
				}
			}
			for _, provider := range systemProviders {
				// make sure that these are set
				provider.Resource.Tenant = ""
				provider.Resource.Owner = "system"
				if existingProvider, ok := existingProviders[provider.Resource.ID]; ok {
					provider.Resource.CreatedAt = existingProvider.Resource.CreatedAt
					provider.Resource.UpdatedAt = existingProvider.Resource.UpdatedAt
				}
				if provider.Resource.CreatedAt.IsZero() {
					provider.Resource.CreatedAt = time.Now()
				}
				if provider.Resource.UpdatedAt.IsZero() {
					provider.Resource.UpdatedAt = provider.Resource.CreatedAt
				}
				err := s.createProviderTxn(txn, &provider)
				if err != nil {
					return serviceerrors.WithRollback(err)
				}
				if slices.Contains(deletedProviders, provider.Resource.ID) {
					updatedProviders = append(updatedProviders, provider.Resource.ID)
					deletedProviders = slices.DeleteFunc(deletedProviders, func(id string) bool {
						return id == provider.Resource.ID
					})
				} else {
					addedProviders = append(addedProviders, provider.Resource.ID)
				}
			}
			s.logger.Info("Loaded system providers", "added", strings.Join(addedProviders, ","), "updated", strings.Join(updatedProviders, ","), "deleted", strings.Join(deletedProviders, ","))
		}
		s.logger.Info("Loaded system resources")
		return nil
	})
}
