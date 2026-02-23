package handlers

import (
	"encoding/json"
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

// HandleListCollections handles GET /api/v1/evaluations/collections
func (h *Handlers) HandleListCollections(ctx *executioncontext.ExecutionContext, req http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {
	storage := h.storage.WithLogger(ctx.Logger).WithContext(ctx.Ctx)

	logging.LogRequestStarted(ctx)

	limit, err := getParam(req, "limit", true, 50)
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}
	offset, err := getParam(req, "offset", true, 0)
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}

	res, err := storage.GetCollections(limit, offset)
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}
	page, err := CreatePage(res.TotalStored, offset, limit, ctx, req)
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}
	w.WriteJSON(api.CollectionResourceList{
		Page:  *page,
		Items: res.Items,
	}, 200)
}

// HandleCreateCollection handles POST /api/v1/evaluations/collections
func (h *Handlers) HandleCreateCollection(ctx *executioncontext.ExecutionContext, req http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {
	storage := h.storage.WithLogger(ctx.Logger).WithContext(ctx.Ctx)

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

	now := time.Now()
	collectionResource := &api.CollectionResource{
		Resource: api.Resource{
			ID:        common.GUID(),
			CreatedAt: &now,
		},
		Type:             "owned", //Assumption: system collections are not created through the API
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
	storage := h.storage.WithLogger(ctx.Logger).WithContext(ctx.Ctx)
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
	storage := h.storage.WithLogger(ctx.Logger).WithContext(ctx.Ctx)

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
	storage := h.storage.WithLogger(ctx.Logger).WithContext(ctx.Ctx)

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
	storage := h.storage.WithLogger(ctx.Logger).WithContext(ctx.Ctx)
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
