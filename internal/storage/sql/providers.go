package sql

import "github.com/eval-hub/eval-hub/pkg/api"

func (s *SQLStorage) CreateUserProvider(provider *api.ProviderResource) error {
	return nil
}

func (s *SQLStorage) GetUserProvider(id string) (*api.ProviderResource, error) {
	return nil, nil
}

func (s *SQLStorage) DeleteUserProvider(id string) error {
	return nil
}
