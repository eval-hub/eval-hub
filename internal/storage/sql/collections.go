package sql

import (
	"database/sql"
	"encoding/json"
	"maps"
	"slices"
	"time"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/messages"
	"github.com/eval-hub/eval-hub/internal/serviceerrors"
	se "github.com/eval-hub/eval-hub/internal/serviceerrors"
	"github.com/eval-hub/eval-hub/internal/storage/sql/shared"
	"github.com/eval-hub/eval-hub/pkg/api"
	jsonpatch "gopkg.in/evanphx/json-patch.v4"
)

//#######################################################################
// Collection operations
//#######################################################################

func (s *SQLStorage) CreateCollection(collection *api.CollectionResource) error {
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
	return s.getCollectionTransactional(nil, id)
}

func (s *SQLStorage) getCollectionTransactional(txn *sql.Tx, id string) (*api.CollectionResource, error) {
	// Build the SELECT query
	query := shared.CollectionQuery{Resource: api.Resource{ID: id}}
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

	collectionResource, err := s.constructCollectionResource(&query.Resource, &collectionConfig)
	if err != nil {
		return nil, se.WithRollback(err)
	}
	return collectionResource, nil
}

func (s *SQLStorage) constructCollectionResource(resource *api.Resource, collectionConfig *api.CollectionConfig) (*api.CollectionResource, error) {
	if collectionConfig == nil {
		s.logger.Error("Failed to construct collection resource", "error", "Collection config does not exist", "id", resource.ID)
		return nil, se.NewServiceError(messages.InternalServerError, "Error", "Collection config does not exist")
	}
	return &api.CollectionResource{
		Resource:         *resource,
		CollectionConfig: *collectionConfig,
	}, nil
}

func (s *SQLStorage) GetCollections(filter *abstractions.QueryFilter) (*abstractions.QueryResults[api.CollectionResource], error) {
	filter = shared.ExtractQueryParams(filter)
	params := filter.Params
	limit := filter.Limit
	offset := filter.Offset

	if err := shared.ValidateFilter(slices.Collect(maps.Keys(params)), []string{"tenant_id", "owner"}); err != nil {
		return nil, err
	}

	// Get total count (there are no filters for collections)
	countQuery, _ := s.statementsFactory.CreateCountEntitiesStatement(shared.TABLE_COLLECTIONS, params)

	var totalCount int
	err := s.queryRow(nil, countQuery).Scan(&totalCount)
	if err != nil {
		s.logger.Error("Failed to count collections", "error", err)
		return nil, se.NewServiceError(messages.QueryFailed, "Type", "collections", "Error", err.Error())
	}

	// Build the list query with pagination and status filter
	listQuery, listArgs := s.statementsFactory.CreateListEntitiesStatement(shared.TABLE_COLLECTIONS, limit, offset, nil)

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
		resource := api.Resource{}
		var entityJSON string

		err = rows.Scan(&resource.ID, &resource.CreatedAt, &resource.UpdatedAt, &resource.Tenant, &resource.Owner, &entityJSON)
		if err != nil {
			s.logger.Error("Failed to scan collection row", "error", err)
			return nil, se.NewServiceError(messages.DatabaseOperationFailed, "Type", "collection", "ResourceId", resource.ID, "Error", err.Error())
		}
		// Unmarshal the entity JSON into collectionConfig
		var collectionConfig api.CollectionConfig
		err = json.Unmarshal([]byte(entityJSON), &collectionConfig)
		if err != nil {
			s.logger.Error("Failed to unmarshal collection entity", "error", err, "id", resource.ID)
			return nil, se.NewServiceError(messages.JSONUnmarshalFailed, "Type", "collection", "Error", err.Error())
		}

		// Construct the CollectionResource
		collectionResource, err := s.constructCollectionResource(&resource, &collectionConfig)
		if err != nil {
			constructErrs = append(constructErrs, err.Error())
			totalCount--
			continue
		}

		items = append(items, *collectionResource)
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
	updateCollectionStatement, args := s.statementsFactory.CreateUpdateEntityStatement(shared.TABLE_COLLECTIONS, collectionID, string(collectionJSON), nil)
	_, err = s.exec(txn, updateCollectionStatement, args...)
	if err != nil {
		return serviceerrors.WithRollback(err)
	}
	return nil
}

func (s *SQLStorage) DeleteCollection(id string) error {
	// Build the DELETE query
	deleteQuery := s.statementsFactory.CreateDeleteEntityStatement(shared.TABLE_COLLECTIONS)

	// Execute the DELETE query
	_, err := s.exec(nil, deleteQuery, id)
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
		//convert the patchedCollectionJSON back to a CollectionConfig
		var patchedCollectionConfig api.CollectionConfig
		err = json.Unmarshal([]byte(patchedCollectionJSON), &patchedCollectionConfig)
		if err != nil {
			return err
		}
		//convert the patched config back to a CollectionResource
		resource := api.Resource{
			ID:        id,
			Tenant:    persistedCollection.Resource.Tenant,
			CreatedAt: persistedCollection.Resource.CreatedAt,
			UpdatedAt: persistedCollection.Resource.UpdatedAt,
			Owner:     persistedCollection.Resource.Owner,
			ReadOnly:  persistedCollection.Resource.ReadOnly,
		}
		now := time.Now()
		if resource.CreatedAt.IsZero() {
			resource.CreatedAt = now
		}
		if resource.UpdatedAt.IsZero() {
			resource.UpdatedAt = now
		}
		persistedCollection, err = s.constructCollectionResource(&resource, &patchedCollectionConfig)
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
