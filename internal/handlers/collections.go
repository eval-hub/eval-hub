package handlers

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/common"
	"github.com/eval-hub/eval-hub/internal/constants"
	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"github.com/eval-hub/eval-hub/internal/http_wrappers"
	"github.com/eval-hub/eval-hub/internal/logging"
	"github.com/eval-hub/eval-hub/internal/messages"
	"github.com/eval-hub/eval-hub/internal/serialization"
	"github.com/eval-hub/eval-hub/internal/serviceerrors"
	"github.com/eval-hub/eval-hub/internal/storage/sql/shared"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func (h *Handlers) filterSystemCollections(filter map[string]any) []api.CollectionResource {
	// Filter keys relevant for system providers (owner, tenant_id, status are not applicable)
	allowedKeys := []string{"name", "tags"}
	filteredCollections := make([]api.CollectionResource, 0, len(h.collectionConfigs))

	for _, c := range h.collectionConfigs {
		if len(filter) == 0 {
			filteredCollections = append(filteredCollections, c)
			continue
		}
		matchesAll := true
		for _, key := range slices.Sorted(maps.Keys(filter)) {
			if !slices.Contains(allowedKeys, key) {
				continue
			}
			v := filter[key]
			values, operator := shared.GetValues(key, v)
			if !matchesCollectionFilterKey(c, key, values, operator) {
				matchesAll = false
				break
			}
		}
		if matchesAll {
			filteredCollections = append(filteredCollections, c)
		}
	}
	return filteredCollections
}

// matchesFilterKey returns true if the provider matches the filter key.
// values and operator come from shared.GetValues (comma=AND, pipe=OR).
func matchesCollectionFilterKey(c api.CollectionResource, key string, values []any, operator string) bool {
	getStr := func(v any) string {
		if s, ok := v.(string); ok {
			return s
		}
		return fmt.Sprintf("%v", v)
	}
	switch key {
	case "name":
		if operator == "OR" {
			for _, val := range values {
				if c.CollectionConfig.Name == getStr(val) {
					return true
				}
			}
			return false
		}
		// AND: name must equal all values (typically one value)
		for _, val := range values {
			if c.CollectionConfig.Name != getStr(val) {
				return false
			}
		}
		return true
	case "tags":
		if operator == "OR" {
			for _, val := range values {
				if slices.Contains(c.CollectionConfig.Tags, getStr(val)) {
					return true
				}
			}
			return false
		}
		// AND: provider must have all tags
		for _, val := range values {
			if !slices.Contains(c.CollectionConfig.Tags, getStr(val)) {
				return false
			}
		}
		return true
	default:
		return true
	}
}

// HandleListCollections handles GET /api/v1/evaluations/collections
func (h *Handlers) HandleListCollections(ctx *executioncontext.ExecutionContext, req http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {
	storage := h.storage.WithLogger(ctx.Logger).WithContext(ctx.Ctx).WithTenant(ctx.Tenant).WithOwner(ctx.User)

	filter, err := CommonListFilters(req)

	logging.LogRequestStarted(ctx, "filter", filter)

	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}

	allowedParams := []string{"limit", "offset", "name", "category", "tags", "system_defined", "owner"}
	badParams := getAllParams(req, allowedParams...)
	if len(badParams) > 0 {
		// just report the first bad parameter
		w.Error(serviceerrors.NewServiceError(messages.QueryBadParameter, "ParameterName", badParams[0], "AllowedParameters", strings.Join(allowedParams, ", ")), ctx.RequestID)
		return
	}

	collections := []api.CollectionResource{}

	if IncludeSystemDefined(req) {
		collections = h.filterSystemCollections(filter.ExtractQueryParams().Params)
		ctx.Logger.Info(fmt.Sprintf("Included %d system defined collections", len(collections)))
	}

	totalCount := len(collections)

	// first check to see if the system collections are enough for the paging
	if len(collections) > 0 {
		if len(collections) < (filter.Limit + filter.Offset) {
			userFilter := &abstractions.QueryFilter{
				Limit:  max(0, filter.Limit-len(collections)),
				Offset: max(0, filter.Offset-len(collections)),
				Params: filter.Params,
			}
			queryResults, err := storage.GetCollections(userFilter)
			if err != nil {
				w.Error(err, ctx.RequestID)
				return
			}
			collections = append(collections, queryResults.Items...)
			totalCount += queryResults.TotalCount
		}
	} else {
		// no system collections so normal flow for user collections
		queryResults, err := storage.GetCollections(filter)
		if err != nil {
			w.Error(err, ctx.RequestID)
			return
		}
		collections = append(collections, queryResults.Items...)
		totalCount += queryResults.TotalCount
	}

	page, err := CreatePage(ctx, totalCount, filter.Offset, filter.Limit, req)
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}

	result := api.CollectionResourceList{
		Page:  *page,
		Items: collections[:min(len(collections), filter.Limit)],
	}

	w.WriteJSON(result, 200)
}

// HandleCreateCollection handles POST /api/v1/evaluations/collections
func (h *Handlers) HandleCreateCollection(ctx *executioncontext.ExecutionContext, req http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {
	storage := h.storage.WithLogger(ctx.Logger).WithContext(ctx.Ctx).WithTenant(ctx.Tenant).WithOwner(ctx.User)

	logging.LogRequestStarted(ctx)

	// get the body bytes from the context
	bodyBytes, err := req.BodyAsBytes()
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}
	collection := &api.CollectionConfig{}
	err = serialization.Unmarshal(h.validate, ctx, bodyBytes, collection)
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}

	collectionResource := &api.CollectionResource{
		Resource: api.Resource{
			ID:        common.GUID(),
			CreatedAt: time.Now(),
			Owner:     ctx.User,
			Tenant:    ctx.Tenant,
			ReadOnly:  false,
		},
		CollectionConfig: *collection,
	}
	err = storage.CreateCollection(collectionResource)
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}
	w.WriteJSON(collectionResource, 202)
}

// HandleGetCollection handles GET /api/v1/evaluations/collections/{collection_id}
func (h *Handlers) HandleGetCollection(ctx *executioncontext.ExecutionContext, req http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {
	storage := h.storage.WithLogger(ctx.Logger).WithContext(ctx.Ctx).WithTenant(ctx.Tenant).WithOwner(ctx.User)

	logging.LogRequestStarted(ctx)

	// Extract ID from path
	collectionID := req.PathValue(constants.PATH_PARAMETER_COLLECTION_ID)
	if collectionID == "" {
		w.Error(serviceerrors.NewServiceError(messages.MissingPathParameter, "ParameterName", constants.PATH_PARAMETER_COLLECTION_ID), ctx.RequestID)
		return
	}

	response, err := storage.GetCollection(collectionID)
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}

	w.WriteJSON(response, 200)
}

// HandleUpdateCollection handles PUT /api/v1/evaluations/collections/{collection_id}
func (h *Handlers) HandleUpdateCollection(ctx *executioncontext.ExecutionContext, req http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {
	storage := h.storage.WithLogger(ctx.Logger).WithContext(ctx.Ctx).WithTenant(ctx.Tenant).WithOwner(ctx.User)

	logging.LogRequestStarted(ctx)

	// Extract ID from path
	collectionID := req.PathValue(constants.PATH_PARAMETER_COLLECTION_ID)
	if collectionID == "" {
		w.Error(serviceerrors.NewServiceError(messages.MissingPathParameter, "ParameterName", constants.PATH_PARAMETER_COLLECTION_ID), ctx.RequestID)
		return
	}

	// get the body bytes from the context
	bodyBytes, err := req.BodyAsBytes()
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}
	collection := &api.CollectionConfig{}
	err = serialization.Unmarshal(h.validate, ctx, bodyBytes, collection)
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}

	collectionResource := &api.CollectionResource{
		Resource: api.Resource{
			ID: collectionID,
		},
		CollectionConfig: *collection,
	}
	err = storage.UpdateCollection(collectionResource)
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}
	w.WriteJSON(collectionResource, 200)
}

// HandlePatchCollection handles PATCH /api/v1/evaluations/collections/{collection_id}
func (h *Handlers) HandlePatchCollection(ctx *executioncontext.ExecutionContext, req http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {
	storage := h.storage.WithLogger(ctx.Logger).WithContext(ctx.Ctx).WithTenant(ctx.Tenant).WithOwner(ctx.User)

	logging.LogRequestStarted(ctx)

	// Extract ID from path
	collectionID := req.PathValue(constants.PATH_PARAMETER_COLLECTION_ID)
	if collectionID == "" {
		w.Error(serviceerrors.NewServiceError(messages.MissingPathParameter, "ParameterName", constants.PATH_PARAMETER_COLLECTION_ID), ctx.RequestID)
		return
	}

	// get the body bytes from the context
	bodyBytes, err := req.BodyAsBytes()
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}
	var patches api.Patch
	if err = json.Unmarshal(bodyBytes, &patches); err != nil {
		w.Error(serviceerrors.NewServiceError(messages.InvalidJSONRequest, "Error", err.Error()), ctx.RequestID)
		return
	}
	for i := range patches {
		if err = h.validate.StructCtx(ctx.Ctx, &patches[i]); err != nil {
			w.Error(serviceerrors.NewServiceError(messages.RequestValidationFailed, "Error", err.Error()), ctx.RequestID)
			return
		}
		//validate that the op is valid as per RFC 6902
		if patches[i].Op != api.PatchOpReplace && patches[i].Op != api.PatchOpAdd && patches[i].Op != api.PatchOpRemove {
			w.Error(serviceerrors.NewServiceError(messages.InvalidJSONRequest, "Error", "Invalid patch operation"), ctx.RequestID)
			return
		}
		//validate that the path is valid as per RFC 6902
		if patches[i].Path == "" {
			w.Error(serviceerrors.NewServiceError(messages.InvalidJSONRequest, "Error", "Invalid patch path"), ctx.RequestID)
			return
		}
	}

	err = storage.PatchCollection(collectionID, &patches)
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}
	w.WriteJSON(nil, 200)
}

// HandleDeleteCollection handles DELETE /api/v1/evaluations/collections/{collection_id}
func (h *Handlers) HandleDeleteCollection(ctx *executioncontext.ExecutionContext, req http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {
	storage := h.storage.WithLogger(ctx.Logger).WithContext(ctx.Ctx).WithTenant(ctx.Tenant).WithOwner(ctx.User)

	logging.LogRequestStarted(ctx)

	// Extract ID from path
	collectionID := req.PathValue(constants.PATH_PARAMETER_COLLECTION_ID)
	if collectionID == "" {
		w.Error(serviceerrors.NewServiceError(messages.MissingPathParameter, "ParameterName", constants.PATH_PARAMETER_COLLECTION_ID), ctx.RequestID)
		return
	}

	err := storage.DeleteCollection(collectionID)
	if err != nil {
		ctx.Logger.Info("Failed to delete collection", "error", err.Error(), "id", collectionID)
		w.Error(err, ctx.RequestID)
		return
	}
	w.WriteJSON(nil, 204)
}
