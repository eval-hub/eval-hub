package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"

	"github.com/eval-hub/eval-hub/internal/collectiondesign"
	"github.com/eval-hub/eval-hub/pkg/api"
)

const (
	catalogPath     = "/api/v1/evaluations/"
	maxCatalogReply = 4 << 20
)

var providerSegment = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)

// sidecarCatalog issues only catalog GETs. The pod namespace is read from the
// mounted service-account identity. The sidecar attaches its token.
type sidecarCatalog struct {
	client *http.Client
	base   url.URL
	tenant string
}

func (s sidecarCatalog) ListProviders(ctx context.Context, offset, limit int) (collectiondesign.Page[api.ProviderResource], error) {
	var list api.ProviderResourceList
	err := s.get(ctx, "providers", pageQuery(offset, limit), &list)
	if err != nil {
		return collectiondesign.Page[api.ProviderResource]{}, err
	}
	for _, item := range list.Items {
		if !s.visible(item.Resource) {
			return collectiondesign.Page[api.ProviderResource]{}, errors.New("provider response is outside the catalog scope")
		}
	}
	return collectiondesign.Page[api.ProviderResource]{Items: list.Items, TotalCount: list.TotalCount}, nil
}

func (s sidecarCatalog) ListCollections(ctx context.Context, offset, limit int) (collectiondesign.Page[api.CollectionResource], error) {
	var list api.CollectionResourceList
	err := s.get(ctx, "collections", pageQuery(offset, limit), &list)
	if err != nil {
		return collectiondesign.Page[api.CollectionResource]{}, err
	}
	for _, item := range list.Items {
		if !s.visible(item.Resource) {
			return collectiondesign.Page[api.CollectionResource]{}, errors.New("collection response is outside the catalog scope")
		}
	}
	return collectiondesign.Page[api.CollectionResource]{Items: list.Items, TotalCount: list.TotalCount}, nil
}

func (s sidecarCatalog) GetProvider(ctx context.Context, id string) (api.ProviderResource, error) {
	if !providerSegment.MatchString(id) {
		return api.ProviderResource{}, errors.New("invalid provider identifier")
	}
	var item api.ProviderResource
	if err := s.get(ctx, "providers/"+id, "", &item); err != nil {
		return api.ProviderResource{}, err
	}
	if item.Resource.ID != id || !s.visible(item.Resource) {
		return api.ProviderResource{}, errors.New("provider response is outside the catalog scope")
	}
	return item, nil
}

func (s sidecarCatalog) visible(resource api.Resource) bool {
	return resource.Tenant == api.Tenant(s.tenant) || (resource.Tenant.IsEmpty() && resource.IsSystemResource())
}

func (s sidecarCatalog) get(ctx context.Context, resource, query string, output any) error {
	endpoint := s.base
	endpoint.Path = catalogPath + resource
	endpoint.RawPath = ""
	endpoint.RawQuery = query
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return errors.New("create catalog request")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Tenant", s.tenant)
	res, err := s.client.Do(req)
	if err != nil {
		return errors.New("send catalog request through the local sidecar")
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("catalog endpoint returned HTTP %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxCatalogReply+1))
	if err != nil || len(body) > maxCatalogReply || json.Unmarshal(body, output) != nil {
		return errors.New("catalog response is malformed or exceeds the size limit")
	}
	return nil
}

func pageQuery(offset, limit int) string {
	return url.Values{"offset": {strconv.Itoa(offset)}, "limit": {strconv.Itoa(limit)}}.Encode()
}
