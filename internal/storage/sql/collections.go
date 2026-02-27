package sql

import (
	"encoding/json"
	"time"

	"github.com/eval-hub/eval-hub/internal/messages"
	"github.com/eval-hub/eval-hub/internal/serviceerrors"

	"github.com/eval-hub/eval-hub/pkg/api"
)

func CreateCollectionEntity(collection *api.CollectionResource) ([]byte, error) {
	collectionJSON, err := json.Marshal(collection.CollectionConfig)
	if err != nil {
		return nil, serviceerrors.NewServiceError(messages.InternalServerError, "Error", err.Error())
	}
	return collectionJSON, nil
}

func ConstructCollectionResource(dbID string, createdAt time.Time, updatedAt time.Time, tenantID string, collectionConfig *api.CollectionConfig) (*api.CollectionResource, error) {
	if collectionConfig == nil {
		return nil, serviceerrors.NewServiceError(messages.InternalServerError, "Error", "Collection config does not exist")
	}
	tenant := api.Tenant(tenantID)
	return &api.CollectionResource{
		Resource: api.Resource{
			ID:        dbID,
			Tenant:    &tenant,
			CreatedAt: &createdAt,
			UpdatedAt: &updatedAt,
		},

		CollectionConfig: *collectionConfig,
	}, nil
}

func ApplyPatchCollection(id string, patches *api.Patch, e SQLExecutor, persistedCollection *api.CollectionResource) (*api.CollectionResource, error) {

	persistedCollectionJSON, err := CreateCollectionEntity(persistedCollection)
	if err != nil {
		return nil, err
	}
	//apply the patches to the persistedCollectionJSON
	patchedCollectionJSON, err := ApplyPatches(string(persistedCollectionJSON), patches)
	if err != nil {
		return nil, err
	}
	//convert the patchedCollectionJSON back to a CollectionConfig
	var patchedCollectionConfig api.CollectionConfig
	err = json.Unmarshal([]byte(patchedCollectionJSON), &patchedCollectionConfig)
	if err != nil {
		return nil, err
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
	return ConstructCollectionResource(id,
		createdAt,
		updatedAt,
		tenantID,
		&patchedCollectionConfig)

}
