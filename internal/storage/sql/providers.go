package sql

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/eval-hub/eval-hub/internal/messages"
	se "github.com/eval-hub/eval-hub/internal/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func (s *SQLStorage) CreateUserProvider(provider *api.ProviderResource) error {
	providerID := provider.Resource.ID
	tenant := s.tenant
	providerJSON, err := s.createProviderEntity(provider)
	if err != nil {
		return se.NewServiceError(messages.InternalServerError, "Error", err)
	}
	addEntityStatement, err := createAddEntityStatement(s.sqlConfig.Driver, TABLE_PROVIDERS)
	if err != nil {
		return se.NewServiceError(messages.InternalServerError, "Error", err)
	}
	s.logger.Info("Creating user provider", "id", providerID, "tenant", tenant)
	_, err = s.exec(nil, addEntityStatement, providerID, tenant, string(providerJSON))
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

func (s *SQLStorage) GetUserProvider(id string) (*api.ProviderResource, error) {
	return s.getUserProviderTransactional(nil, id)
}

func (s *SQLStorage) getUserProviderTransactional(txn *sql.Tx, id string) (*api.ProviderResource, error) {
	selectQuery, err := createGetEntityStatement(s.sqlConfig.Driver, TABLE_PROVIDERS)
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

func (s *SQLStorage) DeleteUserProvider(id string) error {
	deleteQuery, err := createDeleteEntityStatement(s.sqlConfig.Driver, TABLE_PROVIDERS)
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
