package sql

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/internal/eval_hub/storage/sql/shared"
	"github.com/eval-hub/eval-hub/pkg/api"
)

const systemResourceListPageSize = 200

// LoadSystemResources reloads system-owned providers and collections into the
// database. It deletes all existing system resources and inserts the new ones.
// CreatedAt is preserved for IDs that already existed. UpdatedAt is preserved
// only when the serialized config matches the snapshot; otherwise it is set to
// the current time so consumers can detect definition changes.
//
// Concurrent calls are serialized via systemResourcesMu so overlapping config
// watcher reloads cannot interleave delete/insert work.
func (s *sqlStorage) LoadSystemResources(systemCollections map[string]api.CollectionResource, systemProviders map[string]api.ProviderResource) error {
	s.systemResourcesMu.Lock()
	defer s.systemResourcesMu.Unlock()

	s.logger.Info("Loading system resources")

	return s.withTransaction("load-system-resources", "system", func(txn *sql.Tx) error {
		// Full replace of the desired system set:
		// 1. snapshot existing system resources (for timestamps + change logging)
		// 2. delete all system-owned rows in one statement (avoids offset/delete races)
		// 3. insert the new system resources
		// Both delete and insert always run so orphaned records are removed when
		// config files are deleted (empty maps mean "no system resources").
		{
			existingCollections, err := s.listAllSystemCollections(txn)
			if err != nil {
				return serviceerrors.WithRollback(err)
			}

			deleteStmt, deleteArgs := s.statementsFactory.CreateDeleteSystemEntitiesStatement(shared.TableCollections)
			if _, err := s.exec(txn, deleteStmt, deleteArgs...); err != nil {
				return serviceerrors.WithRollback(err)
			}

			var deletedCollections []string
			var updatedCollections []string
			var addedCollections []string
			for id := range existingCollections {
				if _, ok := systemCollections[id]; !ok {
					deletedCollections = append(deletedCollections, id)
				}
			}
			now := time.Now()
			for _, collection := range systemCollections {
				// make sure that these are set
				collection.Resource.Tenant = ""
				collection.Resource.Owner = "system"
				if existingCollection, ok := existingCollections[collection.Resource.ID]; ok {
					collection.Resource.CreatedAt = existingCollection.Resource.CreatedAt
					if systemCollectionConfigEqual(existingCollection, collection) {
						collection.Resource.UpdatedAt = existingCollection.Resource.UpdatedAt
					} else {
						collection.Resource.UpdatedAt = now
					}
				}
				if collection.Resource.CreatedAt.IsZero() {
					collection.Resource.CreatedAt = now
				}
				if collection.Resource.UpdatedAt.IsZero() {
					collection.Resource.UpdatedAt = collection.Resource.CreatedAt
				}
				err := s.createCollectionTxn(txn, &collection)
				if err != nil {
					return serviceerrors.WithRollback(err)
				}
				if _, ok := existingCollections[collection.Resource.ID]; ok {
					updatedCollections = append(updatedCollections, collection.Resource.ID)
				} else {
					addedCollections = append(addedCollections, collection.Resource.ID)
				}
			}
			slices.Sort(deletedCollections)
			slices.Sort(updatedCollections)
			slices.Sort(addedCollections)
			s.logger.Info("Loaded system collections", "added", strings.Join(addedCollections, ","), "updated", strings.Join(updatedCollections, ","), "deleted", strings.Join(deletedCollections, ","))
		}
		{
			existingProviders, err := s.listAllSystemProviders(txn)
			if err != nil {
				return serviceerrors.WithRollback(err)
			}

			deleteStmt, deleteArgs := s.statementsFactory.CreateDeleteSystemEntitiesStatement(shared.TableProviders)
			if _, err := s.exec(txn, deleteStmt, deleteArgs...); err != nil {
				return serviceerrors.WithRollback(err)
			}

			var deletedProviders []string
			var updatedProviders []string
			var addedProviders []string
			for id := range existingProviders {
				if _, ok := systemProviders[id]; !ok {
					deletedProviders = append(deletedProviders, id)
				}
			}
			now := time.Now()
			for _, provider := range systemProviders {
				// make sure that these are set
				provider.Resource.Tenant = ""
				provider.Resource.Owner = "system"
				if existingProvider, ok := existingProviders[provider.Resource.ID]; ok {
					provider.Resource.CreatedAt = existingProvider.Resource.CreatedAt
					if systemProviderConfigEqual(existingProvider, provider) {
						provider.Resource.UpdatedAt = existingProvider.Resource.UpdatedAt
					} else {
						provider.Resource.UpdatedAt = now
					}
				}
				if provider.Resource.CreatedAt.IsZero() {
					provider.Resource.CreatedAt = now
				}
				if provider.Resource.UpdatedAt.IsZero() {
					provider.Resource.UpdatedAt = provider.Resource.CreatedAt
				}
				err := s.createProviderTxn(txn, &provider)
				if err != nil {
					return serviceerrors.WithRollback(err)
				}
				if _, ok := existingProviders[provider.Resource.ID]; ok {
					updatedProviders = append(updatedProviders, provider.Resource.ID)
				} else {
					addedProviders = append(addedProviders, provider.Resource.ID)
				}
			}
			slices.Sort(deletedProviders)
			slices.Sort(updatedProviders)
			slices.Sort(addedProviders)
			s.logger.Info("Loaded system providers", "added", strings.Join(addedProviders, ","), "updated", strings.Join(updatedProviders, ","), "deleted", strings.Join(deletedProviders, ","))
		}
		s.logger.Info("Loaded system resources")
		return nil
	})
}

func systemProviderConfigEqual(existing, next api.ProviderResource) bool {
	a, errA := json.Marshal(existing.ProviderConfig)
	b, errB := json.Marshal(next.ProviderConfig)
	if errA != nil || errB != nil {
		return false
	}
	return bytes.Equal(a, b)
}

func systemCollectionConfigEqual(existing, next api.CollectionResource) bool {
	a, errA := json.Marshal(existing.CollectionConfig)
	b, errB := json.Marshal(next.CollectionConfig)
	if errA != nil || errB != nil {
		return false
	}
	return bytes.Equal(a, b)
}

func (s *sqlStorage) listAllSystemCollections(txn *sql.Tx) (map[string]api.CollectionResource, error) {
	existing := make(map[string]api.CollectionResource)
	offset := 0
	for {
		filter := abstractions.QueryFilter{
			Limit:  systemResourceListPageSize,
			Offset: offset,
			Params: map[string]any{"scope": abstractions.ScopeSystem},
		}
		collections, err := s.getCollectionsTransactional(txn, &filter)
		if err != nil {
			return nil, err
		}
		for _, collection := range collections.Items {
			existing[collection.Resource.ID] = collection
		}
		if len(collections.Items) < filter.Limit {
			break
		}
		offset += filter.Limit
	}
	return existing, nil
}

func (s *sqlStorage) listAllSystemProviders(txn *sql.Tx) (map[string]api.ProviderResource, error) {
	existing := make(map[string]api.ProviderResource)
	offset := 0
	for {
		filter := abstractions.QueryFilter{
			Limit:  systemResourceListPageSize,
			Offset: offset,
			Params: map[string]any{"scope": abstractions.ScopeSystem},
		}
		providers, err := s.getProvidersTransactional(txn, &filter)
		if err != nil {
			return nil, err
		}
		for _, provider := range providers.Items {
			existing[provider.Resource.ID] = provider
		}
		if len(providers.Items) < filter.Limit {
			break
		}
		offset += filter.Limit
	}
	return existing, nil
}
