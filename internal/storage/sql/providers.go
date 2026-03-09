package sql

import (
	"database/sql"
	"encoding/json"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/messages"
	se "github.com/eval-hub/eval-hub/internal/serviceerrors"
	"github.com/eval-hub/eval-hub/internal/storage/sql/shared"
	"github.com/eval-hub/eval-hub/pkg/api"
	jsonpatch "gopkg.in/evanphx/json-patch.v4"
)

func (s *SQLStorage) CreateProvider(provider *api.ProviderResource) error {
	if err := s.verifyTenant(); err != nil {
		return err
	}

	providerID := provider.Resource.ID
	providerJSON, err := s.createProviderEntity(provider)
	if err != nil {
		return se.NewServiceError(messages.InternalServerError, "Error", err)
	}
	addEntityStatement, args := s.statementsFactory.CreateProviderAddEntityStatement(provider, string(providerJSON))
	s.logger.Info("Creating user provider", "id", providerID)
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
	if err := s.verifyTenant(); err != nil {
		return nil, err
	}
	return s.getUserProviderTransactional(nil, id)
}

func (s *SQLStorage) getUserProviderTransactional(txn *sql.Tx, id string) (*api.ProviderResource, error) {
	// Build the SELECT query
	query := shared.EntityQuery{Resource: api.Resource{ID: id, Tenant: s.tenant}}
	selectQuery, selectArgs, queryArgs := s.statementsFactory.CreateProviderGetEntityStatement(&query)

	// Query the database
	err := s.queryRow(txn, selectQuery, selectArgs...).Scan(queryArgs...)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, se.NewServiceError(messages.ResourceNotFound, "Type", "provider", "ResourceId", id)
		}
		s.logger.Error("Failed to get provider", "error", err, "id", id)
		return nil, se.NewServiceError(messages.DatabaseOperationFailed, "Type", "provider", "ResourceId", id, "Error", err.Error())
	}

	var providerConfig api.ProviderConfig
	err = json.Unmarshal([]byte(query.EntityJSON), &providerConfig)
	if err != nil {
		s.logger.Error("Failed to unmarshal provider config", "error", err, "id", id)
		return nil, se.NewServiceError(messages.JSONUnmarshalFailed, "Type", "provider", "Error", err.Error())
	}

	resource := api.ProviderResource{
		Resource:       query.Resource,
		ProviderConfig: providerConfig,
	}

	return &resource, nil
}

func (s *SQLStorage) DeleteProvider(id string) error {
	if err := s.verifyTenant(); err != nil {
		return err
	}

	deleteQuery, args := s.statementsFactory.CreateDeleteEntityStatement(s.tenant, shared.TABLE_PROVIDERS, id)
	_, err := s.exec(nil, deleteQuery, args...)
	if err != nil {
		s.logger.Error("Failed to delete provider", "error", err, "id", id)
		return se.NewServiceError(messages.DatabaseOperationFailed, "Type", "provider", "ResourceId", id, "Error", err.Error())
	}

	s.logger.Info("Deleted user provider", "id", id)
	return nil
}

func (s *SQLStorage) GetProviders(filter *abstractions.QueryFilter) (*abstractions.QueryResults[api.ProviderResource], error) {
	if err := s.verifyTenant(); err != nil {
		return nil, err
	}

	var txn *sql.Tx
	return listEntities[api.ProviderResource](s, txn, shared.TABLE_PROVIDERS, filter)
}

func (s *SQLStorage) UpdateProvider(id string, provider *api.ProviderResource) (*api.ProviderResource, error) {
	if err := s.verifyTenant(); err != nil {
		return nil, err
	}

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
	updateStmt, args := s.statementsFactory.CreateUpdateEntityStatement(s.tenant, shared.TABLE_PROVIDERS, providerID, string(providerJSON), nil)
	_, err = s.exec(txn, updateStmt, args...)
	if err != nil {
		s.logger.Error("Failed to update provider", "error", err, "id", providerID)
		return se.WithRollback(se.NewServiceError(messages.DatabaseOperationFailed, "Type", "provider", "ResourceId", providerID, "Error", err.Error()))
	}
	return nil
}

func (s *SQLStorage) PatchProvider(id string, patches *api.Patch) (*api.ProviderResource, error) {
	if err := s.verifyTenant(); err != nil {
		return nil, err
	}

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
