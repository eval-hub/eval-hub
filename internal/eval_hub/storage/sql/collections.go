package sql

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	se "github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
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

func (s *sqlStorage) createCollectionEntity(collection *api.CollectionResource) ([]byte, error) {
	collectionJSON, err := json.Marshal(collection.CollectionConfig)
	if err != nil {
		return nil, serviceerrors.NewServiceError(messages.InternalServerError, "Error", err.Error())
	}
	return collectionJSON, nil
}

func (s *sqlStorage) GetCollection(id string) (*api.CollectionResource, error) {
	return s.getCollectionTransactional(nil, id)
}

func (s *sqlStorage) getCollectionTransactional(txn *sql.Tx, id string) (*api.CollectionResource, error) {
	query := shared.EntityQuery{Resource: api.Resource{ID: id, Tenant: s.tenant}}
	selectQuery, selectArgs, queryArgs := s.statementsFactory.CreateCollectionGetEntityStatement(&query)

	err := s.queryRow(txn, selectQuery, selectArgs...).Scan(queryArgs...)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, se.NewServiceError(messages.ResourceNotFound, "Type", "collection", "ResourceId", id)
		}
		// For now we differentiate between no rows found and other errors but this might be confusing
		s.logger.Error("Failed to get collection", "error", err, "id", id)
		return nil, se.NewServiceError(messages.DatabaseOperationFailed, "Type", "collection", "ResourceId", id, "Error", err.Error())
	}

	// now check that the tenant_id is allowed to see this resource
	if !s.isVisibleResource(&query.Resource) {
		return nil, se.NewServiceError(messages.ResourceNotFound, "Type", "collection", "ResourceId", id)
	}

	// Unmarshal the entity JSON into EvaluationJobConfig
	var collectionConfig api.CollectionConfig
	err = json.Unmarshal([]byte(query.EntityJSON), &collectionConfig)
	if err != nil {
		s.logger.Error("Failed to unmarshal collection config", "error", err, "id", id)
		return nil, se.NewServiceError(messages.JSONUnmarshalFailed, "Type", "collection", "Error", err.Error())
	}

	collectionResource := api.CollectionResource{
		Resource:         query.Resource,
		CollectionConfig: collectionConfig,
	}

	return &collectionResource, nil
}

func (s *sqlStorage) GetCollections(filter *abstractions.QueryFilter) (*abstractions.QueryResults[api.CollectionResource], error) {
	return s.getCollectionsTransactional(nil, filter)
}

func (s *sqlStorage) getCollectionsTransactional(txn *sql.Tx, filter *abstractions.QueryFilter) (*abstractions.QueryResults[api.CollectionResource], error) {
	return listEntities[api.CollectionResource](s, txn, shared.TABLE_COLLECTIONS, filter)
}

func (s *sqlStorage) UpdateCollection(id string, collection *api.CollectionConfig) (*api.CollectionResource, error) {
	var updated *api.CollectionResource

	err := s.withTransaction("update collection", id, func(txn *sql.Tx) error {
		persistedCollection, err := s.getCollectionTransactional(txn, id)
		if err != nil {
			return err
		}
		if persistedCollection.Resource.IsSystemResource() {
			return se.NewServiceError(
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
	updateCollectionStatement, args := s.statementsFactory.CreateUpdateEntityStatement(s.tenant, shared.TABLE_COLLECTIONS, collectionID, string(collectionJSON), nil)
	_, err = s.exec(txn, updateCollectionStatement, args...)
	if err != nil {
		return serviceerrors.WithRollback(err)
	}
	return nil
}

func (s *sqlStorage) deleteCollectionTxn(txn *sql.Tx, id string) error {
	deleteQuery, args := s.statementsFactory.CreateDeleteEntityStatement(s.tenant, shared.TABLE_COLLECTIONS, id)

	_, err := s.exec(txn, deleteQuery, args...)
	if err != nil {
		s.logger.Error("Failed to delete collection", "error", err, "id", id)
		return se.NewServiceError(messages.DatabaseOperationFailed, "Type", "collection", "ResourceId", id, "Error", err.Error())
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
			return se.NewServiceError(
				messages.ReadOnlyCollection,
				"CollectionID", persistedCollection.Resource.ID,
			)
		}
		return s.deleteCollectionTxn(txn, persistedCollection.Resource.ID)
	})
}

func (s *sqlStorage) PatchCollection(id string, patches *api.Patch) (*api.CollectionResource, error) {
	var updated *api.CollectionResource

	err := s.withTransaction("patch collection", id, func(txn *sql.Tx) error {
		persistedCollection, err := s.getCollectionTransactional(txn, id)
		if err != nil {
			return err
		}
		if persistedCollection.Resource.Owner == "system" {
			return se.NewServiceError(
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
		// convert the patchedCollectionJSON back to a CollectionResource
		var patchedCollection api.CollectionResource
		err = json.Unmarshal([]byte(patchedCollectionJSON), &patchedCollection)
		if err != nil {
			return err
		}
		// convert the patched config back to a CollectionResource
		resource := patchedCollection.Resource
		if resource.CreatedAt.IsZero() {
			resource.CreatedAt = time.Now()
		}
		if resource.UpdatedAt.IsZero() {
			resource.UpdatedAt = resource.CreatedAt
		}
		result := api.CollectionResource{
			Resource:         resource,
			CollectionConfig: patchedCollection.CollectionConfig,
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
