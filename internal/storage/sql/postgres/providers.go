package postgres

import (
	db "database/sql"
	"encoding/json"
	"time"

	"github.com/eval-hub/eval-hub/internal/messages"
	se "github.com/eval-hub/eval-hub/internal/serviceerrors"
	"github.com/eval-hub/eval-hub/internal/storage/sql"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func (s *PostgresStorage) CreateProvider(provider *api.ProviderResource) error {
	providerID := provider.Resource.ID
	tenant := s.tenant
	providerJSON, err := sql.CreateProviderEntity(provider)
	if err != nil {
		return se.NewServiceError(messages.InternalServerError, "Error", err)
	}
	addEntityStatement := INSERT_PROVIDER_STATEMENT

	s.logger.Info("Creating user provider", "id", providerID, "tenant", tenant)

	e := sql.SQLExecutor{
		Db:  s.pool,
		Ctx: s.ctx,
	}
	_, err = e.Exec(addEntityStatement, providerID, tenant, string(providerJSON))
	if err != nil {
		return se.NewServiceError(messages.InternalServerError, "Error", err)
	}
	return nil
}

func (s *PostgresStorage) GetProvider(id string) (*api.ProviderResource, error) {
	return s.getUserProviderTransactional(nil, id)
}

func (s *PostgresStorage) getUserProviderTransactional(txn *db.Tx, id string) (*api.ProviderResource, error) {

	selectQuery := GET_PROVIDER_STATEMENT

	var dbID string
	var createdAt, updatedAt time.Time
	var tenantID string
	var entityJSON string

	e := sql.SQLExecutor{
		Db:  s.pool,
		Ctx: s.ctx,
		Txn: txn,
	}
	err := e.QueryRow(selectQuery, id).Scan(&dbID, &createdAt, &updatedAt, &tenantID, &entityJSON)
	if err != nil {
		if err == db.ErrNoRows {
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

	return sql.ConstructProviderResource(dbID, createdAt, updatedAt, tenantID, &providerConfig)
}

func (s *PostgresStorage) DeleteProvider(id string) error {
	deleteQuery := createDeleteEntityStatement(TABLE_PROVIDERS)

	e := sql.SQLExecutor{
		Db:  s.pool,
		Ctx: s.ctx,
	}
	_, err := e.Exec(deleteQuery, id)
	if err != nil {
		s.logger.Error("Failed to delete provider", "error", err, "id", id)
		return se.NewServiceError(messages.DatabaseOperationFailed, "Type", "provider", "ResourceId", id, "Error", err.Error())
	}

	s.logger.Info("Deleted user provider", "id", id)
	return nil
}
