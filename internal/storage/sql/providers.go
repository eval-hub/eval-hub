package sql

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/messages"
	se "github.com/eval-hub/eval-hub/internal/serviceerrors"
	"github.com/eval-hub/eval-hub/internal/storage/sql/shared"
	"github.com/eval-hub/eval-hub/pkg/api"
	jsonpatch "gopkg.in/evanphx/json-patch.v4"
)

func (s *SQLStorage) CreateProvider(provider *api.ProviderResource) error {
	providerID := provider.Resource.ID
	providerJSON, err := s.createProviderEntity(provider)
	if err != nil {
		return se.NewServiceError(messages.InternalServerError, "Error", err)
	}
	addEntityStatement, args := s.statementsFactory.CreateProviderAddEntityStatement(provider, string(providerJSON))
	s.logger.Info("Creating user provider", "id", providerID, "args", args)
	_, err = s.exec(nil, addEntityStatement, args...)
	if err != nil {
		return se.NewServiceError(messages.InternalServerError, "Error", err)
	}
	return nil
}

func (s *SQLStorage) createProviderEntity(provider *api.ProviderResource) ([]byte, error) {
	providerJSON, err := json.Marshal(provider.ProviderConfig)
	if err != nil {
		return nil, se.NewServiceError(messages.InternalServerError, "Error", err.Error())
	}
	return providerJSON, nil
}

func (s *SQLStorage) GetProvider(id string) (*api.ProviderResource, error) {
	return s.getUserProviderTransactional(nil, id)
}

func (s *SQLStorage) getUserProviderTransactional(txn *sql.Tx, id string) (*api.ProviderResource, error) {
	selectQuery, err := createGetEntityStatement(s.sqlConfig.Driver, shared.TABLE_PROVIDERS)
	if err != nil {
		return nil, se.NewServiceError(messages.InternalServerError, "Error", err.Error())
	}

	var dbID string
	var createdAt, updatedAt time.Time
	var tenantID string
	var entityJSON string

	err = s.queryRow(txn, selectQuery, id).Scan(&dbID, &createdAt, &updatedAt, &tenantID, &entityJSON)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, se.NewServiceError(messages.ResourceNotFound, "Type", "provider", "ResourceId", id)
		}
		s.logger.Error("Failed to get provider", "error", err, "id", id)
		return nil, se.NewServiceError(messages.DatabaseOperationFailed, "Type", "provider", "ResourceId", id, "Error", err.Error())
	}

	var providerConfig api.ProviderConfig
	err = json.Unmarshal([]byte(entityJSON), &providerConfig)
	if err != nil {
		s.logger.Error("Failed to unmarshal provider config", "error", err, "id", id)
		return nil, se.NewServiceError(messages.JSONUnmarshalFailed, "Type", "provider", "Error", err.Error())
	}

	return s.constructProviderResource(dbID, createdAt, updatedAt, tenantID, &providerConfig)
}

func (s *SQLStorage) constructProviderResource(dbID string, createdAt time.Time, updatedAt time.Time, tenantID string, providerConfig *api.ProviderConfig) (*api.ProviderResource, error) {
	if providerConfig == nil {
		s.logger.Error("Failed to construct provider resource", "error", "Provider config does not exist", "id", dbID)
		return nil, se.NewServiceError(messages.InternalServerError, "Error", "Provider config does not exist")
	}
	tenant := api.Tenant(tenantID)
	return &api.ProviderResource{
		Resource: api.Resource{
			ID:        dbID,
			Tenant:    &tenant,
			CreatedAt: &createdAt,
			UpdatedAt: &updatedAt,
		},
		ProviderConfig: *providerConfig,
	}, nil
}

func (s *SQLStorage) DeleteProvider(id string) error {
	deleteQuery, err := createDeleteEntityStatement(s.sqlConfig.Driver, shared.TABLE_PROVIDERS)
	if err != nil {
		return se.NewServiceError(messages.InternalServerError, "Error", "Error while building delete provider query")
	}

	_, err = s.exec(nil, deleteQuery, id)
	if err != nil {
		s.logger.Error("Failed to delete provider", "error", err, "id", id)
		return se.NewServiceError(messages.DatabaseOperationFailed, "Type", "provider", "ResourceId", id, "Error", err.Error())
	}

	s.logger.Info("Deleted user provider", "id", id)
	return nil
}

func (s *SQLStorage) GetProviders(filter *abstractions.QueryFilter) (*abstractions.QueryResults[api.ProviderResource], error) {
	filter = extractQueryParams(filter)
	params := filter.Params
	limit := filter.Limit
	offset := filter.Offset

	// TODO: why is this here?
	delete(params, "benchmarks")

	selectQuery, args, err := createListEntitiesStatement(s.sqlConfig.Driver, shared.TABLE_PROVIDERS, limit, offset, params)
	if err != nil {
		return nil, se.NewServiceError(messages.InternalServerError, "Error", err.Error())
	}

	rows, err := s.query(nil, selectQuery, args...)
	if err != nil {
		return nil, se.NewServiceError(messages.InternalServerError, "Error", err.Error())
	}
	defer rows.Close()

	items := []api.ProviderResource{}
	for rows.Next() {
		var dbID string
		var createdAt, updatedAt time.Time
		var tenantID string
		var entityJSON string
		err = rows.Scan(&dbID, &createdAt, &updatedAt, &tenantID, &entityJSON)
		if err != nil {
			return nil, se.NewServiceError(messages.InternalServerError, "Error", err.Error())
		}
		var providerConfig api.ProviderConfig
		err = json.Unmarshal([]byte(entityJSON), &providerConfig)
		if err != nil {
			return nil, se.NewServiceError(messages.JSONUnmarshalFailed, "Type", "provider", "Error", err.Error())
		}
		resource, err := s.constructProviderResource(dbID, createdAt, updatedAt, tenantID, &providerConfig)
		if err != nil {
			return nil, se.NewServiceError(messages.InternalServerError, "Error", err.Error())
		}
		items = append(items, *resource)
	}
	if err = rows.Err(); err != nil {
		return nil, se.NewServiceError(messages.InternalServerError, "Error", err.Error())
	}
	return &abstractions.QueryResults[api.ProviderResource]{
		Items:       items,
		TotalStored: len(items),
		Errors:      nil,
	}, nil
}

func (s *SQLStorage) UpdateProvider(id string, provider *api.ProviderResource) (*api.ProviderResource, error) {
	var updated *api.ProviderResource
	err := s.withTransaction("update provider", id, func(txn *sql.Tx) error {
		persisted, err := s.getUserProviderTransactional(txn, id)
		if err != nil {
			return err
		}
		merged := &api.ProviderResource{
			Resource:       persisted.Resource,
			ProviderConfig: provider.ProviderConfig,
		}
		if err := s.updateProviderTransactional(txn, id, merged); err != nil {
			return err
		}
		updated, err = s.getUserProviderTransactional(txn, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.logger.Info("Updated provider", "id", id)
	return updated, nil
}

func (s *SQLStorage) updateProviderTransactional(txn *sql.Tx, providerID string, provider *api.ProviderResource) error {
	providerJSON, err := s.createProviderEntity(provider)
	if err != nil {
		return se.NewServiceError(messages.InternalServerError, "Error", err)
	}
	updateStmt, args, err := CreateUpdateCollectionStatement(s.sqlConfig.Driver, shared.TABLE_PROVIDERS, providerID, string(providerJSON))
	if err != nil {
		return se.NewServiceError(messages.InternalServerError, "Error", err)
	}
	_, err = s.exec(txn, updateStmt, args...)
	if err != nil {
		s.logger.Error("Failed to update provider", "error", err, "id", providerID)
		return se.WithRollback(se.NewServiceError(messages.DatabaseOperationFailed, "Type", "provider", "ResourceId", providerID, "Error", err.Error()))
	}
	return nil
}

func (s *SQLStorage) PatchProvider(id string, patches *api.Patch) (*api.ProviderResource, error) {
	var updated *api.ProviderResource
	err := s.withTransaction("patch provider", id, func(txn *sql.Tx) error {
		// TODO: verify the patches and return a validation error if they are on invalid fields
		//for _, patch := range *patches {
		//if isImmutablePatchPath(patch.Path) {
		//	return se.NewServiceError(messages.InvalidJSONRequest, "Type", "provider", "Error", "Invalid patch path")
		//}
		//}

		persisted, err := s.getUserProviderTransactional(txn, id)
		if err != nil {
			return err
		}
		persistedJSON, err := s.createProviderEntity(persisted)
		if err != nil {
			return err
		}
		patchedJSON, err := applyProviderPatches(string(persistedJSON), patches)
		if err != nil {
			return err
		}
		var patchedConfig api.ProviderConfig
		if err := json.Unmarshal([]byte(patchedJSON), &patchedConfig); err != nil {
			return se.NewServiceError(messages.JSONUnmarshalFailed, "Type", "provider", "Error", err.Error())
		}
		merged := &api.ProviderResource{
			Resource:       persisted.Resource,
			ProviderConfig: patchedConfig,
		}
		if err := s.updateProviderTransactional(txn, id, merged); err != nil {
			return err
		}
		updated, err = s.getUserProviderTransactional(txn, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.logger.Info("Patched provider", "id", id)
	return updated, nil
}

func applyProviderPatches(doc string, patches *api.Patch) (string, error) {
	if patches == nil || len(*patches) == 0 {
		return doc, nil
	}
	patchesJSON, err := json.Marshal(patches)
	if err != nil {
		return "", err
	}
	patch, err := jsonpatch.DecodePatch(patchesJSON)
	if err != nil {
		return "", err
	}
	result, err := patch.Apply([]byte(doc))
	if err != nil {
		return "", err
	}
	return string(result), nil
}
