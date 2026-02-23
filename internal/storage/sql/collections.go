package sql

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/messages"
	"github.com/eval-hub/eval-hub/internal/serviceerrors"
	se "github.com/eval-hub/eval-hub/internal/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
	jsonpatch "gopkg.in/evanphx/json-patch.v4"
)

//#######################################################################
// Collection operations
//#######################################################################

func (s *SQLStorage) CreateCollection(collection *api.CollectionResource) error {
	collectionId := collection.Resource.ID
	scope := collection.Type
	tenant := s.tenant
	collectionJSON, err := s.createCollectionEntity(collection)
	if err != nil {
		return serviceerrors.NewServiceError(messages.InternalServerError, "Error", err)
	}
	addEntityStatement, err := createAddEntityStatement(s.sqlConfig.Driver, TABLE_COLLECTIONS)
	if err != nil {
		return serviceerrors.NewServiceError(messages.InternalServerError, "Error", err)
	}
	_, err = s.exec(nil, addEntityStatement, collectionId, tenant, scope, string(collectionJSON))
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
	return s.getCollectionTransactional(nil, id)
}

func (s *SQLStorage) getCollectionTransactional(txn *sql.Tx, id string) (*api.CollectionResource, error) {
	// Build the SELECT query
	selectQuery, err := createGetEntityStatement(s.sqlConfig.Driver, TABLE_COLLECTIONS)
	if err != nil {
		return nil, serviceerrors.NewServiceError(messages.InternalServerError, "Error", err.Error())
	}

	// Query the database
	var dbID string
	var createdAt, updatedAt time.Time
	var tenantID string
	var scope string
	var collectionsJSON string

	err = s.queryRow(txn, selectQuery, id).Scan(&dbID, &createdAt, &updatedAt, &tenantID, &scope, &collectionsJSON)
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
	err = json.Unmarshal([]byte(collectionsJSON), &collectionConfig)
	if err != nil {
		s.logger.Error("Failed to unmarshal collection config", "error", err, "id", id)
		return nil, se.NewServiceError(messages.JSONUnmarshalFailed, "Type", "collection", "Error", err.Error())
	}

	collectionResource, err := s.constructCollectionResource(dbID, createdAt, updatedAt, tenantID, scope, &collectionConfig)
	if err != nil {
		return nil, se.WithRollback(err)
	}
	return collectionResource, nil

}

func (s *SQLStorage) constructCollectionResource(dbID string, createdAt time.Time, updatedAt time.Time, tenantID string, scope string, collectionConfig *api.CollectionConfig) (*api.CollectionResource, error) {
	if collectionConfig == nil {
		s.logger.Error("Failed to construct collection resource", "error", "Collection config does not exist", "id", dbID)
		return nil, se.NewServiceError(messages.InternalServerError, "Error", "Collection config does not exist")
	}
	tenant := api.Tenant(tenantID)
	return &api.CollectionResource{
		Resource: api.Resource{
			ID:        dbID,
			Tenant:    &tenant,
			CreatedAt: &createdAt,
			UpdatedAt: &updatedAt,
		},
		Type:             scope,
		CollectionConfig: *collectionConfig,
	}, nil
}

func (s *SQLStorage) GetCollections(limit int, offset int) (*abstractions.QueryResults[api.CollectionResource], error) {
	// Get total count (there are no filters for collections)
	countQuery, _, err := createCountEntitiesStatement(s.sqlConfig.Driver, TABLE_COLLECTIONS, "")
	if err != nil {
		return nil, err
	}

	var totalCount int
	err = s.queryRow(nil, countQuery).Scan(&totalCount)
	if err != nil {
		s.logger.Error("Failed to count collections", "error", err)
		return nil, se.NewServiceError(messages.QueryFailed, "Type", "collections", "Error", err.Error())
	}

	// Build the list query with pagination and status filter
	listQuery, listArgs, err := createListEntitiesStatement(s.sqlConfig.Driver, TABLE_COLLECTIONS, limit, offset, "")
	if err != nil {
		return nil, err
	}

	// Query the database
	rows, err := s.query(nil, listQuery, listArgs...)
	if err != nil {
		s.logger.Error("Failed to list collections", "error", err)
		return nil, se.NewServiceError(messages.QueryFailed, "Type", "collections", "Error", err.Error())
	}
	defer rows.Close()

	// Process rows
	var constructErrs []string
	var items []api.CollectionResource
	for rows.Next() {
		var dbID string
		var createdAt, updatedAt time.Time
		var tenantID string
		var scope string
		var entityJSON string

		err = rows.Scan(&dbID, &createdAt, &updatedAt, &tenantID, &scope, &entityJSON)
		if err != nil {
			s.logger.Error("Failed to scan collection row", "error", err)
			return nil, se.NewServiceError(messages.DatabaseOperationFailed, "Type", "collection", "ResourceId", dbID, "Error", err.Error())
		}

		// Unmarshal the entity JSON into collectionConfig
		var collectionConfig api.CollectionConfig
		err = json.Unmarshal([]byte(entityJSON), &collectionConfig)
		if err != nil {
			s.logger.Error("Failed to unmarshal collection entity", "error", err, "id", dbID)
			return nil, se.NewServiceError(messages.JSONUnmarshalFailed, "Type", "collection", "Error", err.Error())
		}

		// Construct the CollectionResource
		resource, err := s.constructCollectionResource(dbID, createdAt, updatedAt, tenantID, scope, &collectionConfig)
		if err != nil {
			constructErrs = append(constructErrs, err.Error())
			totalCount--
			continue
		}

		items = append(items, *resource)
	}

	if err = rows.Err(); err != nil {
		s.logger.Error("Error iterating collection rows", "error", err)
		return nil, se.NewServiceError(messages.QueryFailed, "Type", "collections", "Error", err.Error())
	}

	return &abstractions.QueryResults[api.CollectionResource]{
		Items:       items,
		TotalStored: totalCount,
		Errors:      constructErrs,
	}, nil
}

func (s *SQLStorage) UpdateCollection(collection *api.CollectionResource) error {
	err := s.withTransaction("update collection", collection.Resource.ID, func(txn *sql.Tx) error {
		persistedCollection, err := s.getCollectionTransactional(txn, collection.Resource.ID)
		if err != nil {
			return err
		}
		if persistedCollection.Type == "system" {
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
	updateCollectionStatement, args, err := CreateUpdateCollectionStatement(s.sqlConfig.Driver, TABLE_COLLECTIONS, collectionID, string(collectionJSON))
	if err != nil {
		return serviceerrors.NewServiceError(messages.InternalServerError, "Error", err)
	}
	_, err = s.exec(txn, updateCollectionStatement, args...)
	if err != nil {
		return serviceerrors.WithRollback(err)
	}
	return nil
}

func (s *SQLStorage) DeleteCollection(id string) error {
	// Build the DELETE query
	deleteQuery, err := createDeleteEntityStatement(s.sqlConfig.Driver, TABLE_COLLECTIONS)
	if err != nil {
		return se.NewServiceError(messages.InternalServerError, "Error", "Error while building delete collection query")
	}

	// Execute the DELETE query
	_, err = s.exec(nil, deleteQuery, id)
	if err != nil {
		s.logger.Error("Failed to delete collection", "error", err, "id", id)
		return se.NewServiceError(messages.DatabaseOperationFailed, "Type", "collection", "ResourceId", id, "Error", err.Error())
	}

	s.logger.Info("Deleted collection", "id", id)

	return nil
}

func (s *SQLStorage) PatchCollection(id string, patches *api.Patch) error {
	err := s.withTransaction("patch collection", id, func(txn *sql.Tx) error {
		persistedCollection, err := s.getCollectionTransactional(txn, id)
		if err != nil {
			return err
		}
		if persistedCollection.Type == "system" {
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
		//convert the patchedCollectionJSON back to a CollectionConfig
		var patchedCollectionConfig api.CollectionConfig
		err = json.Unmarshal([]byte(patchedCollectionJSON), &patchedCollectionConfig)
		if err != nil {
			return err
		}
		//convert the patched config back to a CollectionResource
		var createdAt, updatedAt time.Time
		if persistedCollection.Resource.CreatedAt != nil {
			createdAt = *persistedCollection.Resource.CreatedAt
		}
		if persistedCollection.Resource.UpdatedAt != nil {
			updatedAt = *persistedCollection.Resource.UpdatedAt
		}
		tenantID := ""
		if persistedCollection.Resource.Tenant != nil {
			tenantID = string(*persistedCollection.Resource.Tenant)
		}
		persistedCollection, err = s.constructCollectionResource(id,
			createdAt,
			updatedAt,
			tenantID,
			persistedCollection.Type,
			&patchedCollectionConfig)
		if err != nil {
			return err
		}
		return s.updateCollectionTransactional(txn, id, persistedCollection)
	})
	return err
}

func applyPatches(s string, patches *api.Patch) ([]byte, error) {
	if patches == nil || len(*patches) == 0 {
		return []byte(s), nil
	}
	patchesJSON, err := json.Marshal(patches)
	if err != nil {
		return nil, err
	}
	patch, err := jsonpatch.DecodePatch(patchesJSON)
	if err != nil {
		return nil, err
	}
	return patch.Apply([]byte(s))
}
