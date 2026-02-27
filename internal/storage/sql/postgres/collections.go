package postgres

import (
	db "database/sql"
	"encoding/json"
	"time"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/messages"
	"github.com/eval-hub/eval-hub/internal/serviceerrors"
	se "github.com/eval-hub/eval-hub/internal/serviceerrors"
	"github.com/eval-hub/eval-hub/internal/storage/sql"
	"github.com/eval-hub/eval-hub/pkg/api"
)

//#######################################################################
// Collection operations
//#######################################################################

func (s *PostgresStorage) CreateCollection(collection *api.CollectionResource) error {
	collectionId := collection.Resource.ID
	tenant := s.tenant
	collectionJSON, err := sql.CreateCollectionEntity(collection)
	if err != nil {
		return serviceerrors.NewServiceError(messages.InternalServerError, "Error", err)
	}
	addEntityStatement := INSERT_COLLECTION_STATEMENT

	e := sql.SQLExecutor{
		Db:  s.pool,
		Ctx: s.ctx,
	}
	_, err = e.Exec(addEntityStatement, collectionId, tenant, string(collectionJSON))
	if err != nil {
		return serviceerrors.NewServiceError(messages.InternalServerError, "Error", err)
	}
	return nil
}

func (s *PostgresStorage) GetCollection(id string) (*api.CollectionResource, error) {
	e := sql.SQLExecutor{
		Db:  s.pool,
		Ctx: s.ctx,
	}
	return getCollectionTransactional(id, GET_COLLECTION_STATEMENT, e)
}

func (s *PostgresStorage) GetCollections(filter abstractions.QueryFilter) (*abstractions.QueryResults[api.CollectionResource], error) {

	filter = extractQueryParams(filter)
	params := filter.Params
	limit := filter.Limit
	offset := filter.Offset

	// Get total count (there are no filters for collections)
	countQuery, _, err := createCountEntitiesStatement(TABLE_COLLECTIONS, params)
	if err != nil {
		return nil, err
	}

	e := sql.SQLExecutor{
		Db:  s.pool,
		Ctx: s.ctx,
	}

	var totalCount int
	err = e.QueryRow(countQuery).Scan(&totalCount)
	if err != nil {
		s.logger.Error("Failed to count collections", "error", err)
		return nil, se.NewServiceError(messages.QueryFailed, "Type", "collections", "Error", err.Error())
	}

	// Build the list query with pagination and status filter
	listQuery, listArgs, err := createListEntitiesStatement(TABLE_COLLECTIONS, limit, offset, nil)
	if err != nil {
		return nil, err
	}

	// Query the database
	rows, err := e.Query(listQuery, listArgs...)
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
		var entityJSON string

		err = rows.Scan(&dbID, &createdAt, &updatedAt, &tenantID, &entityJSON)
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
		resource, err := sql.ConstructCollectionResource(dbID, createdAt, updatedAt, tenantID, &collectionConfig)
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

func (s *PostgresStorage) UpdateCollection(collection *api.CollectionResource) error {
	err := sql.WithTransaction(s.pool, s.ctx, s.logger, "update collection", collection.Resource.ID, func(txn *db.Tx) error {

		e := sql.SQLExecutor{
			Db:  s.pool,
			Ctx: s.ctx,
			Txn: txn,
		}

		persistedCollection, err := getCollectionTransactional(collection.Resource.ID, GET_COLLECTION_STATEMENT, e)
		if err != nil {
			return err
		}
		if persistedCollection.Resource.Owner == "system" {
			return se.NewServiceError(messages.BadRequest, "Type", "collection", "ResourceId", collection.Resource.ID, "Error", "System collections cannot be updated")
		}
		persistedCollection.CollectionConfig = collection.CollectionConfig
		return updateCollectionTransactional(persistedCollection, e)
	})
	return err
}

func (s *PostgresStorage) DeleteCollection(id string) error {
	// Build the DELETE query
	deleteQuery := createDeleteEntityStatement(TABLE_COLLECTIONS)

	e := sql.SQLExecutor{
		Db:  s.pool,
		Ctx: s.ctx,
	}
	// Execute the DELETE query
	_, err := e.Exec(deleteQuery, id)
	if err != nil {
		s.logger.Error("Failed to delete collection", "error", err, "id", id)
		return se.NewServiceError(messages.DatabaseOperationFailed, "Type", "collection", "ResourceId", id, "Error", err.Error())
	}

	s.logger.Info("Deleted collection", "id", id)

	return nil
}

func (s *PostgresStorage) PatchCollection(id string, patches *api.Patch) error {
	err := sql.WithTransaction(s.pool, s.ctx, s.logger, "patch collection", id, func(txn *db.Tx) error {

		e := sql.SQLExecutor{
			Db:  s.pool,
			Ctx: s.ctx,
			Txn: txn,
		}
		persistedCollection, err := getCollectionTransactional(id, GET_COLLECTION_STATEMENT, e)
		if err != nil {
			return err
		}
		if persistedCollection.Resource.Owner == "system" {
			return se.NewServiceError(messages.BadRequest, "Type", "collection", "ResourceId", id, "Error", "System collections cannot be patched")
		}

		persistedCollection, err = sql.ApplyPatchCollection(id, patches, e, persistedCollection)
		if err != nil {
			return err
		}

		return updateCollectionTransactional(persistedCollection, e)
	})
	return err
}

func getCollectionTransactional(id string, selectQuery string, e sql.SQLExecutor) (*api.CollectionResource, error) {

	// Query the database
	var dbID string
	var createdAt, updatedAt time.Time
	var tenantID string
	var collectionsJSON string

	err := e.QueryRow(selectQuery, id).Scan(&dbID, &createdAt, &updatedAt, &tenantID, &collectionsJSON)
	if err != nil {
		if err == db.ErrNoRows {
			return nil, serviceerrors.NewServiceError(messages.ResourceNotFound, "Type", "collection", "ResourceId", id)
		}
		// For now we differentiate between no rows found and other errors but this might be confusing
		return nil, serviceerrors.NewServiceError(messages.DatabaseOperationFailed, "Type", "collection", "ResourceId", id, "Error", err.Error())
	}

	// Unmarshal the entity JSON into EvaluationJobConfig
	var collectionConfig api.CollectionConfig
	err = json.Unmarshal([]byte(collectionsJSON), &collectionConfig)
	if err != nil {
		return nil, serviceerrors.NewServiceError(messages.JSONUnmarshalFailed, "Type", "collection", "Error", err.Error())
	}

	collectionResource, err := sql.ConstructCollectionResource(dbID, createdAt, updatedAt, tenantID, &collectionConfig)
	if err != nil {
		return nil, serviceerrors.WithRollback(err)
	}
	return collectionResource, nil

}

func updateCollectionTransactional(collection *api.CollectionResource, e sql.SQLExecutor) error {
	collectionJSON, err := sql.CreateCollectionEntity(collection)
	if err != nil {
		return serviceerrors.NewServiceError(messages.InternalServerError, "Error", err)
	}
	updateCollectionStatement, args := createUpdateCollectionStatement(collection.Resource.ID, string(collectionJSON))
	if err != nil {
		return serviceerrors.NewServiceError(messages.InternalServerError, "Error", err)
	}

	_, err = e.Exec(updateCollectionStatement, args...)
	if err != nil {
		return serviceerrors.WithRollback(err)
	}
	return nil
}
