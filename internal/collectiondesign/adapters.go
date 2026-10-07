package collectiondesign

import (
	"context"
	"fmt"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/evalhubclient"
)

// ClientReader is the small part of evalhubclient needed by the MCP adapter.
type ClientReader interface {
	ListProviders(opts ...evalhubclient.ListOption) (*api.ProviderResourceList, error)
	ListCollections(opts ...evalhubclient.ListOption) (*api.CollectionResourceList, error)
}

// ClientSource wraps a client already scoped to the MCP request. For a real
// evalhubclient.Client, scope it with WithContext before constructing this source
// so cancellation also interrupts an in-flight HTTP call.
type ClientSource struct{ client ClientReader }

func NewClientSource(scopedClient ClientReader) *ClientSource {
	return &ClientSource{client: scopedClient}
}

func (s *ClientSource) ListProviders(ctx context.Context, offset, limit int) (Page[api.ProviderResource], error) {
	if err := ctx.Err(); err != nil {
		return Page[api.ProviderResource]{}, err
	}
	list, err := s.client.ListProviders(evalhubclient.WithLimit(limit), evalhubclient.WithOffset(offset))
	if err != nil {
		return Page[api.ProviderResource]{}, err
	}
	if list == nil {
		return Page[api.ProviderResource]{}, fmt.Errorf("provider list is nil")
	}
	return Page[api.ProviderResource]{Items: list.Items, TotalCount: list.TotalCount}, nil
}

func (s *ClientSource) ListCollections(ctx context.Context, offset, limit int) (Page[api.CollectionResource], error) {
	if err := ctx.Err(); err != nil {
		return Page[api.CollectionResource]{}, err
	}
	list, err := s.client.ListCollections(evalhubclient.WithLimit(limit), evalhubclient.WithOffset(offset))
	if err != nil {
		return Page[api.CollectionResource]{}, err
	}
	if list == nil {
		return Page[api.CollectionResource]{}, fmt.Errorf("collection list is nil")
	}
	return Page[api.CollectionResource]{Items: list.Items, TotalCount: list.TotalCount}, nil
}

// StorageSource wraps API-service storage already scoped with WithTenant and
// WithOwner. WithContext only replaces its request context; it retains identity.
type StorageSource struct{ storage abstractions.Storage }

func NewStorageSource(scopedStorage abstractions.Storage) *StorageSource {
	return &StorageSource{storage: scopedStorage}
}

func (s *StorageSource) ListProviders(ctx context.Context, offset, limit int) (Page[api.ProviderResource], error) {
	if err := ctx.Err(); err != nil {
		return Page[api.ProviderResource]{}, err
	}
	result, err := s.storage.WithContext(ctx).GetProviders(&abstractions.QueryFilter{Limit: limit, Offset: offset})
	if err != nil {
		return Page[api.ProviderResource]{}, err
	}
	if result == nil {
		return Page[api.ProviderResource]{}, fmt.Errorf("provider query result is nil")
	}
	return Page[api.ProviderResource]{Items: result.Items, TotalCount: result.TotalCount}, nil
}

func (s *StorageSource) ListCollections(ctx context.Context, offset, limit int) (Page[api.CollectionResource], error) {
	if err := ctx.Err(); err != nil {
		return Page[api.CollectionResource]{}, err
	}
	result, err := s.storage.WithContext(ctx).GetCollections(&abstractions.QueryFilter{Limit: limit, Offset: offset})
	if err != nil {
		return Page[api.CollectionResource]{}, err
	}
	if result == nil {
		return Page[api.CollectionResource]{}, fmt.Errorf("collection query result is nil")
	}
	return Page[api.CollectionResource]{Items: result.Items, TotalCount: result.TotalCount}, nil
}
