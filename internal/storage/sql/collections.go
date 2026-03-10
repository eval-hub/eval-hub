package sql

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/messages"
	"github.com/eval-hub/eval-hub/internal/serviceerrors"
	se "github.com/eval-hub/eval-hub/internal/serviceerrors"
	"github.com/eval-hub/eval-hub/internal/storage/sql/shared"
	"github.com/eval-hub/eval-hub/pkg/api"
)

//#######################################################################
// Collection operations
//#######################################################################

func (s *SQLStorage) CreateCollection(collection *api.CollectionResource) error {
	if err := s.verifyTenant(); err != nil {
		return err
	}

	collectionJSON, err := s.createCollectionEntity(collection)
	if err != nil {
		return serviceerrors.NewServiceError(messages.InternalServerError, "Error", err)
	}
	addEntityStatement, args := s.statementsFactory.CreateCollectionAddEntityStatement(collection, string(collectionJSON))
	_, err = s.exec(nil, addEntityStatement, args...)
	if err != nil {
		return serviceerrors.NewServiceError(messages.InternalServerError, "Error", err)
	}
	return nil
}

func (s *SQLStorage) createCollectionEntity(collection *api.CollectionResource) ([]byte, error) {
	collectionJSON, err := json.Marshal(collection.CollectionConfig)
	if err != nil {
		return nil, serviceerrors.NewServiceError(messages.InternalServerError, "Error", err.Error())
	}
	return collectionJSON, nil
}

func (s *SQLStorage) GetCollection(id string) (*api.CollectionResource, error) {
	if err := s.verifyTenant(); err != nil {
		return nil, err
	}

	return s.getCollectionTransactional(nil, id)
}

func (s *SQLStorage) getCollectionTransactional(txn *sql.Tx, id string) (*api.CollectionResource, error) {
	// Build the SELECT query
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

func (s *SQLStorage) GetCollections(filter *abstractions.QueryFilter) (*abstractions.QueryResults[api.CollectionResource], error) {
	if err := s.verifyTenant(); err != nil {
		return nil, err
	}

	var txn *sql.Tx
	return listEntities[api.CollectionResource](s, txn, shared.TABLE_COLLECTIONS, filter)
}

func (s *SQLStorage) UpdateCollection(collection *api.CollectionResource) error {
	if err := s.verifyTenant(); err != nil {
		return err
	}

	err := s.withTransaction("update collection", collection.Resource.ID, func(txn *sql.Tx) error {
		persistedCollection, err := s.getCollectionTransactional(txn, collection.Resource.ID)
		if err != nil {
			return err
		}
		if persistedCollection.Resource.Owner == "system" {
			return se.NewServiceError(messages.BadRequest, "Type", "collection", "ResourceId", collection.Resource.ID, "Error", "System collections cannot be updated")
		}
		persistedCollection.CollectionConfig = collection.CollectionConfig
		return s.updateCollectionTransactional(txn, collection.Resource.ID, persistedCollection)
	})
	return err
}

func (s *SQLStorage) updateCollectionTransactional(txn *sql.Tx, collectionID string, collection *api.CollectionResource) error {
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

func (s *SQLStorage) DeleteCollection(id string) error {
	if err := s.verifyTenant(); err != nil {
		return err
	}

	// Build the DELETE query
	deleteQuery, args := s.statementsFactory.CreateDeleteEntityStatement(s.tenant, shared.TABLE_COLLECTIONS, id)

	// Execute the DELETE query
	_, err := s.exec(nil, deleteQuery, args...)
	if err != nil {
		s.logger.Error("Failed to delete collection", "error", err, "id", id)
		return se.NewServiceError(messages.DatabaseOperationFailed, "Type", "collection", "ResourceId", id, "Error", err.Error())
	}

	s.logger.Info("Deleted collection", "id", id)

	return nil
}

func (s *SQLStorage) PatchCollection(id string, patches *api.Patch) error {
	if err := s.verifyTenant(); err != nil {
		return err
	}

	err := s.withTransaction("patch collection", id, func(txn *sql.Tx) error {
		persistedCollection, err := s.getCollectionTransactional(txn, id)
		if err != nil {
			return err
		}
		if persistedCollection.Resource.Owner == "system" {
			return se.NewServiceError(messages.BadRequest, "Type", "collection", "ResourceId", id, "Error", "System collections cannot be patched")
		}
		//conevert persistedCollection to json
		persistedCollectionJSON, err := s.createCollectionEntity(persistedCollection)
		if err != nil {
			return err
		}
		//apply the patches to the persistedCollectionJSON
		patchedCollectionJSON, err := applyPatches(string(persistedCollectionJSON), patches)
		if err != nil {
			return err
		}
		//convert the patchedCollectionJSON back to a CollectionResource
		var patchedCollection api.CollectionResource
		err = json.Unmarshal([]byte(patchedCollectionJSON), &patchedCollection)
		if err != nil {
			return err
		}
		//convert the patched config back to a CollectionResource
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
		return s.updateCollectionTransactional(txn, id, &result)
	})
	return err
}
