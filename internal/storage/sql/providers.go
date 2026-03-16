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

func (s *sqlStorage) CreateProvider(provider *api.ProviderResource) error {
	if err := s.verifyTenant(); err != nil {
		return err
	}

	return s.createProviderTxn(nil, provider)
}

func (s *sqlStorage) createProviderTxn(txn *sql.Tx, provider *api.ProviderResource) error {
	// do this for now as the read_only field is not stored in the database
	s.setReadOnly(&provider.Resource)

	providerJSON, err := s.createProviderEntity(provider)
	if err != nil {
		return se.NewServiceError(messages.InternalServerError, "Error", err)
	}
	addEntityStatement, args := s.statementsFactory.CreateProviderAddEntityStatement(provider, string(providerJSON))
	_, err = s.exec(txn, addEntityStatement, args...)
	if err != nil {
		return se.NewServiceError(messages.InternalServerError, "Error", err)
	}

	s.logger.Info("Stored provider", "id", provider.Resource.ID, "resource", s.prettyPrint(provider.Resource))

	return nil
}

func (s *sqlStorage) createProviderEntity(provider *api.ProviderResource) ([]byte, error) {
	providerJSON, err := json.Marshal(provider.ProviderConfig)
	if err != nil {
		return nil, se.NewServiceError(messages.InternalServerError, "Error", err.Error())
	}
	return providerJSON, nil
}

func (s *sqlStorage) GetProvider(id string) (*api.ProviderResource, error) {
	if err := s.verifyTenant(); err != nil {
		return nil, err
	}
	return s.getUserProviderTransactional(nil, id)
}

func (s *sqlStorage) getUserProviderTransactional(txn *sql.Tx, id string) (*api.ProviderResource, error) {
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

	// do this for now as the read_only field is not stored in the database
	s.setReadOnly(&query.Resource)

	// now check that the tenant_id is allowed to see this resource
	if !s.isVisibleResource(&query.Resource) {
		return nil, se.NewServiceError(messages.ResourceNotFound, "Type", "provider", "ResourceId", id)
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

func (s *sqlStorage) deleteProviderTxn(txn *sql.Tx, id string) error {
	deleteQuery, args := s.statementsFactory.CreateDeleteEntityStatement(s.tenant, shared.TABLE_PROVIDERS, id)
	_, err := s.exec(txn, deleteQuery, args...)
	if err != nil {
		s.logger.Error("Failed to delete provider", "error", err, "id", id)
		return se.NewServiceError(messages.DatabaseOperationFailed, "Type", "provider", "ResourceId", id, "Error", err.Error())
	}

	s.logger.Debug("Deleted provider", "id", id)

	return nil
}

func (s *sqlStorage) DeleteProvider(id string) error {
	if err := s.verifyTenant(); err != nil {
		return err
	}

	return s.withTransaction("delete provider", id, func(txn *sql.Tx) error {
		persistedProvider, err := s.getUserProviderTransactional(txn, id)
		if err != nil {
			return err
		}
		if !persistedProvider.Resource.CanDelete() {
			return se.NewServiceError(
				messages.ReadOnlyProvider,
				"ProviderID", id,
			)
		}
		return s.deleteProviderTxn(txn, persistedProvider.Resource.ID)
	})
}

func (s *sqlStorage) GetProviders(filter *abstractions.QueryFilter) (*abstractions.QueryResults[api.ProviderResource], error) {
	if err := s.verifyTenant(); err != nil {
		return nil, err
	}

	var txn *sql.Tx
	return listEntities[api.ProviderResource](s, txn, shared.TABLE_PROVIDERS, filter)
}

func (s *sqlStorage) UpdateProvider(id string, providerConfig *api.ProviderConfig) (*api.ProviderResource, error) {
	if err := s.verifyTenant(); err != nil {
		return nil, err
	}

	var updated *api.ProviderResource
	err := s.withTransaction("update provider", id, func(txn *sql.Tx) error {
		persisted, err := s.getUserProviderTransactional(txn, id)
		if err != nil {
			return err
		}
		if !persisted.Resource.CanUpdate() {
			return se.NewServiceError(
				messages.ReadOnlyProvider,
				"ProviderID", id,
			)
		}
		merged := &api.ProviderResource{
			Resource:       persisted.Resource,
			ProviderConfig: *providerConfig,
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
	s.logger.Debug("Updated provider", "id", id)
	return updated, nil
}

func (s *sqlStorage) updateProviderTransactional(txn *sql.Tx, providerID string, provider *api.ProviderResource) error {
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

func (s *sqlStorage) PatchProvider(id string, patches *api.Patch) (*api.ProviderResource, error) {
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
		if !persisted.Resource.CanUpdate() {
			return se.NewServiceError(
				messages.ReadOnlyProvider,
				"ProviderID", id,
			)
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
	s.logger.Debug("Patched provider", "id", id)
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
