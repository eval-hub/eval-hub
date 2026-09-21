package sql

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/internal/eval_hub/storage/sql/shared"
	"github.com/eval-hub/eval-hub/pkg/api"
)

//#######################################################################
// Collection operations
//#######################################################################

func (s *sqlStorage) CreateCollection(collection *api.CollectionResource) error {
	return s.createCollectionTxn(nil, collection)
}

func (s *sqlStorage) createCollectionTxn(txn *sql.Tx, collection *api.CollectionResource) error {
	if collection.Resource.CreatedAt.IsZero() {
		collection.Resource.CreatedAt = time.Now()
	}
	if collection.Resource.UpdatedAt.IsZero() {
		collection.Resource.UpdatedAt = collection.Resource.CreatedAt
	}
	collectionJSON, err := s.createCollectionEntity(collection)
	if err != nil {
		return serviceerrors.NewServiceError(messages.InternalServerError, "Error", err)
	}
	addEntityStatement, args := s.statementsFactory.CreateCollectionAddEntityStatement(collection, string(collectionJSON))
	_, err = s.exec(txn, addEntityStatement, args...)
	if err != nil {
		return serviceerrors.NewServiceError(messages.InternalServerError, "Error", err)
	}

	s.logger.Info("Stored collection", "id", collection.Resource.ID, "resource", s.prettyPrint(collection.Resource))

	return nil
}

// collectionStoredEntity is the internal representation persisted in the entity JSON column.
// It embeds CollectionConfig (so all config fields are at the top level) and adds
// server-managed fields that must also be persisted without requiring a schema migration.
// Backward compatibility: old entity JSON missing these fields deserialises with zero values.
type collectionStoredEntity struct {
	api.CollectionConfig
	DerivedFrom string                `json:"derived_from,omitempty"`
	PinnedOrder int                   `json:"pinned_order,omitempty"`
	Status      *api.CollectionStatus `json:"status,omitempty"`
}

func (s *sqlStorage) createCollectionEntity(collection *api.CollectionResource) ([]byte, error) {
	entity := collectionStoredEntity{
		CollectionConfig: collection.CollectionConfig,
		DerivedFrom:      collection.DerivedFrom,
		PinnedOrder:      collection.PinnedOrder,
		Status:           collection.Status,
	}
	collectionJSON, err := json.Marshal(entity)
	if err != nil {
		return nil, serviceerrors.NewServiceError(messages.InternalServerError, "Error", err.Error())
	}
	return collectionJSON, nil
}

func (s *sqlStorage) GetCollection(id string) (*api.CollectionResource, error) {
	return s.getCollectionTransactional(nil, id)
}

func (s *sqlStorage) getCollectionTransactional(txn *sql.Tx, id string) (*api.CollectionResource, error) {
	return s.getCollectionTransactionalWithLock(txn, id, false)
}

func (s *sqlStorage) getCollectionTransactionalForUpdate(txn *sql.Tx, id string) (*api.CollectionResource, error) {
	return s.getCollectionTransactionalWithLock(txn, id, true)
}

func (s *sqlStorage) getCollectionTransactionalWithLock(txn *sql.Tx, id string, forUpdate bool) (*api.CollectionResource, error) {
	query := shared.EntityQuery{Resource: api.Resource{ID: id, Tenant: s.tenant}}
	var selectQuery string
	var selectArgs, queryArgs []any
	if forUpdate {
		selectQuery, selectArgs, queryArgs = s.statementsFactory.CreateCollectionGetEntityForUpdateStatement(&query)
	} else {
		selectQuery, selectArgs, queryArgs = s.statementsFactory.CreateCollectionGetEntityStatement(&query)
	}

	err := s.queryRow(txn, selectQuery, selectArgs...).Scan(queryArgs...)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, serviceerrors.NewServiceError(messages.ResourceNotFound, "Type", "collection", "ResourceId", id)
		}
		// For now we differentiate between no rows found and other errors but this might be confusing
		s.logger.Error("Failed to get collection", "error", err, "id", id)
		return nil, serviceerrors.NewServiceError(messages.DatabaseOperationFailed, "Type", "collection", "ResourceId", id, "Error", err.Error())
	}

	// now check that the tenant_id is allowed to see this resource
	if !s.isVisibleResource(&query.Resource) {
		return nil, serviceerrors.NewServiceError(messages.ResourceNotFound, "Type", "collection", "ResourceId", id)
	}

	// Unmarshal the entity JSON into the stored entity (includes DerivedFrom and Status)
	var entity collectionStoredEntity
	err = json.Unmarshal([]byte(query.EntityJSON), &entity)
	if err != nil {
		s.logger.Error("Failed to unmarshal collection entity", "error", err, "id", id)
		return nil, serviceerrors.NewServiceError(messages.JSONUnmarshalFailed, "Type", "collection", "Error", err.Error())
	}

	collectionResource := api.CollectionResource{
		Resource:         query.Resource,
		DerivedFrom:      entity.DerivedFrom,
		PinnedOrder:      entity.PinnedOrder,
		CollectionConfig: entity.CollectionConfig,
		Status:           entity.Status,
	}

	return &collectionResource, nil
}

func (s *sqlStorage) GetCollections(filter *abstractions.QueryFilter) (*abstractions.QueryResults[api.CollectionResource], error) {
	return s.getCollectionsTransactional(nil, filter)
}

func (s *sqlStorage) getCollectionsTransactional(txn *sql.Tx, filter *abstractions.QueryFilter) (*abstractions.QueryResults[api.CollectionResource], error) {
	return listEntities[api.CollectionResource](s, txn, shared.TableCollections, filter)
}

func (s *sqlStorage) UpdateCollection(id string, collection *api.CollectionConfig) (*api.CollectionResource, error) {
	var updated *api.CollectionResource

	err := s.withTransaction("update collection", id, func(txn *sql.Tx) error {
		persistedCollection, err := s.getCollectionTransactionalForUpdate(txn, id)
		if err != nil {
			return err
		}
		if persistedCollection.Resource.IsSystemResource() || persistedCollection.CurationOrder > 0 {
			return serviceerrors.NewServiceError(
				messages.ReadOnlyCollection,
				"CollectionID", id,
			)
		}
		persistedCollection.CollectionConfig = *collection
		err = s.updateCollectionTransactional(txn, id, persistedCollection)
		if err != nil {
			return err
		}
		updated, err = s.getCollectionTransactional(txn, id)

		return err
	})

	return updated, err
}

func (s *sqlStorage) updateCollectionTransactional(txn *sql.Tx, collectionID string, collection *api.CollectionResource) error {
	collectionJSON, err := s.createCollectionEntity(collection)
	if err != nil {
		return serviceerrors.NewServiceError(messages.InternalServerError, "Error", err)
	}
	updateCollectionStatement, args := s.statementsFactory.CreateUpdateEntityStatement(s.tenant, shared.TableCollections, collectionID, string(collectionJSON), nil)
	_, err = s.exec(txn, updateCollectionStatement, args...)
	if err != nil {
		return serviceerrors.WithRollback(err)
	}
	return nil
}

func (s *sqlStorage) deleteCollectionTxn(txn *sql.Tx, id string) error {
	deleteQuery, args := s.statementsFactory.CreateDeleteEntityStatement(s.tenant, shared.TableCollections, id)

	_, err := s.exec(txn, deleteQuery, args...)
	if err != nil {
		s.logger.Error("Failed to delete collection", "error", err, "id", id)
		return serviceerrors.NewServiceError(messages.DatabaseOperationFailed, "Type", "collection", "ResourceId", id, "Error", err.Error())
	}

	s.logger.Debug("Deleted collection", "id", id)

	return nil
}

func (s *sqlStorage) DeleteCollection(id string) error {
	return s.withTransaction("delete collection", id, func(txn *sql.Tx) error {
		persistedCollection, err := s.getCollectionTransactional(txn, id)
		if err != nil {
			return err
		}
		if persistedCollection.Resource.IsSystemResource() {
			return serviceerrors.NewServiceError(
				messages.ReadOnlyCollection,
				"CollectionID", persistedCollection.Resource.ID,
			)
		}
		return s.deleteCollectionTxn(txn, persistedCollection.Resource.ID)
	})
}

func (s *sqlStorage) UpdateCollectionStatus(id string, state *api.CollectionStatus) (*api.CollectionResource, error) {
	var updated *api.CollectionResource

	err := s.withTransaction("update collection state", id, func(txn *sql.Tx) error {
		coll, err := s.getCollectionTransactionalForUpdate(txn, id)
		if err != nil {
			return err
		}
		coll.Status = state
		if err = s.updateCollectionTransactional(txn, id, coll); err != nil {
			return err
		}
		updated, err = s.getCollectionTransactional(txn, id)
		return err
	})

	return updated, err
}

func (s *sqlStorage) PatchCollection(id string, patches *api.Patch) (*api.CollectionResource, error) {
	var updated *api.CollectionResource

	err := s.withTransaction("patch collection", id, func(txn *sql.Tx) error {
		// Test hook: no-op unless a test installs a callback (see test_hooks.go).
		invokeCollectionPatchBeforeLockedReadHook(id)
		persistedCollection, err := s.getCollectionTransactionalForUpdate(txn, id)
		if err != nil {
			return err
		}
		// Test hook: no-op unless a test installs a callback (see test_hooks.go).
		invokeCollectionPatchAfterLockedReadHook(id)
		if persistedCollection.Resource.Owner == "system" || persistedCollection.CurationOrder > 0 {
			return serviceerrors.NewServiceError(
				messages.ReadOnlyCollection,
				"CollectionID", id,
			)
		}
		// convert persistedCollection to json
		persistedCollectionJSON, err := s.createCollectionEntity(persistedCollection)
		if err != nil {
			return err
		}
		// apply the patches to the persistedCollectionJSON
		patchedCollectionJSON, err := applyPatches(string(persistedCollectionJSON), patches)
		if err != nil {
			return err
		}
		// Unmarshal back into the stored entity to preserve DerivedFrom and Status
		var patchedEntity collectionStoredEntity
		err = json.Unmarshal([]byte(patchedCollectionJSON), &patchedEntity)
		if err != nil {
			return err
		}
		if patchedEntity.PinnedOrder < 0 {
			return serviceerrors.NewServiceError(messages.RequestValidationFailed, "Error", "pinned_order must be 0 or positive")
		}
		result := api.CollectionResource{
			Resource:         persistedCollection.Resource,
			DerivedFrom:      patchedEntity.DerivedFrom,
			PinnedOrder:      patchedEntity.PinnedOrder,
			CollectionConfig: patchedEntity.CollectionConfig,
			Status:           patchedEntity.Status,
		}
		err = s.updateCollectionTransactional(txn, id, &result)
		if err != nil {
			return err
		}
		updated, err = s.getCollectionTransactional(txn, id)
		return err
	})

	return updated, err
}
