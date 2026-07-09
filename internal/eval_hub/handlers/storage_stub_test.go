package handlers

import (
	"context"
	"log/slog"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// noopStorage is a minimal abstractions.Storage for handler unit tests.
type noopStorage struct{}

func (noopStorage) WithLogger(_ *slog.Logger) abstractions.Storage { return noopStorage{} }
func (noopStorage) WithContext(_ context.Context) abstractions.Storage {
	return noopStorage{}
}
func (noopStorage) WithTenant(_ api.Tenant) abstractions.Storage { return noopStorage{} }
func (noopStorage) WithOwner(_ api.User) abstractions.Storage    { return noopStorage{} }
func (noopStorage) Ping(_ time.Duration) error                   { return nil }
func (noopStorage) CreateEvaluationJob(_ *api.EvaluationJobResource) error {
	return nil
}
func (noopStorage) GetEvaluationJob(_ string) (*api.EvaluationJobResource, error) {
	return nil, nil
}
func (noopStorage) GetEvaluationJobs(_ *abstractions.QueryFilter) (*abstractions.QueryResults[api.EvaluationJobResource], error) {
	return &abstractions.QueryResults[api.EvaluationJobResource]{}, nil
}
func (noopStorage) DeleteEvaluationJob(_ string) error { return nil }
func (noopStorage) UpdateEvaluationJob(_ string, _ *api.StatusEvent) error {
	return nil
}
func (noopStorage) UpdateEvaluationJobCardURL(_ string, _ string) error { return nil }
func (noopStorage) UpdateEvaluationJobStatus(_ string, _ api.OverallState, _ *api.MessageInfo) error {
	return nil
}
func (noopStorage) CreateCollection(_ *api.CollectionResource) error { return nil }
func (noopStorage) GetCollection(_ string) (*api.CollectionResource, error) {
	return nil, nil
}
func (noopStorage) GetCollections(_ *abstractions.QueryFilter) (*abstractions.QueryResults[api.CollectionResource], error) {
	return &abstractions.QueryResults[api.CollectionResource]{}, nil
}
func (noopStorage) UpdateCollection(_ string, _ *api.CollectionConfig) (*api.CollectionResource, error) {
	return nil, nil
}
func (noopStorage) PatchCollection(_ string, _ *api.Patch) (*api.CollectionResource, error) {
	return nil, nil
}
func (noopStorage) DeleteCollection(_ string) error              { return nil }
func (noopStorage) CreateProvider(_ *api.ProviderResource) error { return nil }
func (noopStorage) GetProvider(_ string) (*api.ProviderResource, error) {
	return nil, nil
}
func (noopStorage) GetProviders(_ *abstractions.QueryFilter) (*abstractions.QueryResults[api.ProviderResource], error) {
	return &abstractions.QueryResults[api.ProviderResource]{}, nil
}
func (noopStorage) UpdateProvider(_ string, _ *api.ProviderConfig) (*api.ProviderResource, error) {
	return nil, nil
}
func (noopStorage) PatchProvider(_ string, _ *api.Patch) (*api.ProviderResource, error) {
	return nil, nil
}
func (noopStorage) DeleteProvider(_ string) error { return nil }
func (noopStorage) LoadSystemResources(_ map[string]api.CollectionResource, _ map[string]api.ProviderResource) error {
	return nil
}
func (noopStorage) Close() error { return nil }
