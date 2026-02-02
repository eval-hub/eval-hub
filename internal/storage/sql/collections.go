package sql

import (
	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"github.com/eval-hub/eval-hub/pkg/api"
)

//#######################################################################
// Collection operations
//#######################################################################

func (s *SQLStorage) CreateCollection(ctx *executioncontext.ExecutionContext, collection *api.CollectionResource) error {
	return nil
}

func (s *SQLStorage) GetCollection(ctx *executioncontext.ExecutionContext, id string, summary bool) (*api.CollectionResource, error) {
	return nil, nil
}

func (s *SQLStorage) GetCollections(ctx *executioncontext.ExecutionContext, limit int, offset int) (*api.CollectionResourceList, error) {
	return nil, nil
}

func (s *SQLStorage) UpdateCollection(ctx *executioncontext.ExecutionContext, collection *api.CollectionResource) error {
	return nil
}

func (s *SQLStorage) DeleteCollection(ctx *executioncontext.ExecutionContext, id string) error {
	return nil
}
