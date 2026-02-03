package handlers

import (
	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"github.com/eval-hub/eval-hub/internal/http_wrappers"
	"github.com/eval-hub/eval-hub/internal/messages"
)

// HandleListCollections handles GET /api/v1/evaluations/collections
func (h *Handlers) HandleListCollections(ctx *executioncontext.ExecutionContext, w http_wrappers.ResponseWrapper) {
	w.ErrorWithMessageCode(ctx.RequestID, messages.NotImplemented, "Api", "list collections")
}

// HandleCreateCollection handles POST /api/v1/evaluations/collections
func (h *Handlers) HandleCreateCollection(ctx *executioncontext.ExecutionContext, w http_wrappers.ResponseWrapper) {
	w.ErrorWithMessageCode(ctx.RequestID, messages.NotImplemented, "Api", "create collection")
}

// HandleGetCollection handles GET /api/v1/evaluations/collections/{collection_id}
func (h *Handlers) HandleGetCollection(ctx *executioncontext.ExecutionContext, w http_wrappers.ResponseWrapper) {
	w.ErrorWithMessageCode(ctx.RequestID, messages.NotImplemented, "Api", "get collection")
}

// HandleUpdateCollection handles PUT /api/v1/evaluations/collections/{collection_id}
func (h *Handlers) HandleUpdateCollection(ctx *executioncontext.ExecutionContext, w http_wrappers.ResponseWrapper) {
	w.ErrorWithMessageCode(ctx.RequestID, messages.NotImplemented, "Api", "update collection")
}

// HandlePatchCollection handles PATCH /api/v1/evaluations/collections/{collection_id}
func (h *Handlers) HandlePatchCollection(ctx *executioncontext.ExecutionContext, w http_wrappers.ResponseWrapper) {
	w.ErrorWithMessageCode(ctx.RequestID, messages.NotImplemented, "Api", "patch collection")
}

// HandleDeleteCollection handles DELETE /api/v1/evaluations/collections/{collection_id}
func (h *Handlers) HandleDeleteCollection(ctx *executioncontext.ExecutionContext, w http_wrappers.ResponseWrapper) {
	w.ErrorWithMessageCode(ctx.RequestID, messages.NotImplemented, "Api", "delete collection")
}
