package sql

import (
	"encoding/json"
	"time"

	"github.com/eval-hub/eval-hub/internal/messages"
	"github.com/eval-hub/eval-hub/internal/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func CreateProviderEntity(provider *api.ProviderResource) ([]byte, error) {
	providerJSON, err := json.Marshal(provider.ProviderConfig)
	if err != nil {
		return nil, serviceerrors.NewServiceError(messages.InternalServerError, "Error", err.Error())
	}
	return providerJSON, nil
}

func ConstructProviderResource(dbID string, createdAt time.Time, updatedAt time.Time, tenantID string, providerConfig *api.ProviderConfig) (*api.ProviderResource, error) {
	if providerConfig == nil {
		return nil, serviceerrors.NewServiceError(messages.InternalServerError, "Error", "Provider config does not exist")
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
