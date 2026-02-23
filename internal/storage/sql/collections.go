package sql

import (
	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/pkg/api"
)

//#######################################################################
// Collection operations
//#######################################################################

func (s *SQLStorage) CreateCollection(collection *api.CollectionResource) error {
	return nil
}

func (s *SQLStorage) GetCollection(id string) (*api.CollectionResource, error) {
	return nil, nil
}

func (s *SQLStorage) GetCollections(limit int, offset int, filter abstractions.QueryFilter) (*abstractions.QueryResults[api.CollectionResource], error) {
	return nil, nil
}

func (s *SQLStorage) UpdateCollection(collection *api.CollectionResource) error {
	return nil
}

func (s *SQLStorage) DeleteCollection(id string) error {
	return nil
}
