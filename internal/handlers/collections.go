package handlers

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/eval-hub/eval-hub/internal/common"
	"github.com/eval-hub/eval-hub/internal/constants"
	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"github.com/eval-hub/eval-hub/internal/http_wrappers"
	"github.com/eval-hub/eval-hub/internal/logging"
	"github.com/eval-hub/eval-hub/internal/messages"
	"github.com/eval-hub/eval-hub/internal/serialization"
	"github.com/eval-hub/eval-hub/internal/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
)

var (
	// these are the allowed patches for the user-defined collection config
	allowedCollectionPatches = []allowedPatch{
		{Path: "/name", Op: api.PatchOpReplace, Prefix: false},

		{Path: "/description", Op: api.PatchOpAdd, Prefix: false},
		{Path: "/description", Op: api.PatchOpRemove, Prefix: false},
		{Path: "/description", Op: api.PatchOpReplace, Prefix: false},

		{Path: "/tags", Op: api.PatchOpAdd, Prefix: true},
		{Path: "/tags", Op: api.PatchOpRemove, Prefix: true},
		{Path: "/tags", Op: api.PatchOpReplace, Prefix: true},

		{Path: "/custom", Op: api.PatchOpAdd, Prefix: true},
		{Path: "/custom", Op: api.PatchOpRemove, Prefix: true},
		{Path: "/custom", Op: api.PatchOpReplace, Prefix: true},

		{Path: "/category", Op: api.PatchOpReplace, Prefix: false},

		{Path: "/benchmarks", Op: api.PatchOpReplace, Prefix: true},

		{Path: "/pass_criteria", Op: api.PatchOpAdd, Prefix: false},
		{Path: "/pass_criteria", Op: api.PatchOpRemove, Prefix: false},
		{Path: "/pass_criteria", Op: api.PatchOpReplace, Prefix: false},
	}
)

// HandleListCollections handles GET /api/v1/evaluations/collections
func (h *Handlers) HandleListCollections(ctx *executioncontext.ExecutionContext, req http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {
	storage := h.storage.WithLogger(ctx.Logger).WithContext(ctx.Ctx).WithTenant(ctx.Tenant).WithOwner(ctx.User)

	filter, err := CommonListFilters(req, "category", "scope")

	logging.LogRequestStarted(ctx, "filter", filter)

	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}

	err = CheckScope(filter)
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}

	allowedParams := []string{"limit", "offset", "name", "category", "tags", "owner", "scope"}
	badParams := getAllParams(req, allowedParams...)
	if len(badParams) > 0 {
		// just report the first bad parameter
		w.Error(serviceerrors.NewServiceError(messages.QueryBadParameter, "ParameterName", badParams[0], "AllowedParameters", strings.Join(allowedParams, ", ")), ctx.RequestID)
		return
	}

	collections, err := storage.GetCollections(filter)
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}

	page, err := CreatePage(ctx, collections.TotalCount, filter.Offset, filter.Limit, req)
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}

	result := api.CollectionResourceList{
		Page:  *page,
		Items: collections.Items,
	}

	w.WriteJSON(result, 200)
}

// HandleCreateCollection handles POST /api/v1/evaluations/collections
func (h *Handlers) HandleCreateCollection(ctx *executioncontext.ExecutionContext, req http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {
	storage := h.storage.WithLogger(ctx.Logger).WithContext(ctx.Ctx).WithTenant(ctx.Tenant).WithOwner(ctx.User)

	logging.LogRequestStarted(ctx)

	id := common.GUID()

	collection := &api.CollectionConfig{}

	err := h.withSpan(
		ctx,
		func(runtimeCtx context.Context) error {
			// get the body bytes from the context
			bodyBytes, err := req.BodyAsBytes()
			if err != nil {
				return err
			}
			return serialization.Unmarshal(h.validate, ctx.WithContext(runtimeCtx), bodyBytes, collection)
		},
		"validation",
		"validate-collection",
		"collection.id", id,
	)
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}

	var collectionResource *api.CollectionResource

	_ = h.withSpan(
		ctx,
		func(runtimeCtx context.Context) error {
			collectionResource = &api.CollectionResource{
				Resource: api.Resource{
					ID:        id,
					CreatedAt: time.Now(),
					Owner:     ctx.User,
					Tenant:    ctx.Tenant,
				},
				CollectionConfig: *collection,
			}
			err := storage.WithContext(runtimeCtx).CreateCollection(collectionResource)
			if err != nil {
				w.Error(err, ctx.RequestID)
				return err
			} else {
				w.WriteJSON(collectionResource, 201)
				return nil
			}
		},
		"storage",
		"create-collection",
		"collection.id", id,
	)
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

	_ = h.withSpan(
		ctx,
		func(runtimeCtx context.Context) error {
			response, err := storage.WithContext(runtimeCtx).GetCollection(collectionID)
			if err != nil {
				w.Error(err, ctx.RequestID)
				return err
			}
			w.WriteJSON(response, 200)
			return nil
		},
		"storage",
		"get-collection",
		"collection.id", collectionID,
	)
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

	request := &api.CollectionConfig{}

	err := h.withSpan(
		ctx,
		func(runtimeCtx context.Context) error {
	err := h.withSpan(
		ctx,
		func(runtimeCtx context.Context) error {
			// get the body bytes from the context
			bodyBytes, err := req.BodyAsBytes()
			bodyBytes, err := req.BodyAsBytes()
			if err != nil {
				return err
			}
			return serialization.Unmarshal(h.validate, ctx.WithContext(runtimeCtx), bodyBytes, request)
		},
		"validation",
		"validate-collection-update",
		"collection.id", collectionID,
	)
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}

	_ = h.withSpan(
		ctx,
		func(runtimeCtx context.Context) error {
			result, err := storage.WithContext(runtimeCtx).UpdateCollection(collectionID, request)
			if err != nil {
				w.Error(err, ctx.RequestID)
				return err
			}
			w.WriteJSON(result, 200)
			return nil
		},
		"storage",
		"update-collection",
		"collection.id", collectionID,
	)
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

	var patches api.Patch

	err := h.withSpan(
		ctx,
		func(runtimeCtx context.Context) error {
			// get the body bytes from the context
			bodyBytes, err := req.BodyAsBytes()
			if err != nil {
				return err
			}
			if err = json.Unmarshal(bodyBytes, &patches); err != nil {
				return serviceerrors.NewServiceError(messages.InvalidJSONRequest, "Error", err.Error())
			}
			if err := h.verifyPatches(runtimeCtx, patches, allowedCollectionPatches); err != nil {
				return err
			}
			return nil
		},
		"validation",
		"validate-collection-patch",
		"collection.id", collectionID,
	)
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}

	_ = h.withSpan(
		ctx,
		func(runtimeCtx context.Context) error {
			result, err := storage.WithContext(runtimeCtx).PatchCollection(collectionID, &patches)
			if err != nil {
				w.Error(err, ctx.RequestID)
				return err
			}

			w.WriteJSON(result, 200)
			return nil
		},
		"storage",
		"patch-collection",
		"collection.id", collectionID,
	)
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

	_ = h.withSpan(
		ctx,
		func(runtimeCtx context.Context) error {
			err := storage.WithContext(runtimeCtx).DeleteCollection(collectionID)
			if err != nil {
				w.Error(err, ctx.RequestID)
				return err
			}
			w.WriteJSON(nil, 204)
			return nil
		},
		"storage",
		"delete-collection",
		"collection.id", collectionID,
	)
}
