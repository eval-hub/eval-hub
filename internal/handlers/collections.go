package handlers

import (
	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"github.com/eval-hub/eval-hub/internal/http_wrappers"
)

// HandleListCollections handles GET /api/v1/evaluations/collections
func (h *Handlers) HandleListCollections(ctx *executioncontext.ExecutionContext, w http_wrappers.ResponseWrapper) {

	w.Error("Not implemented", 501, ctx.RequestID)

}

// HandleCreateCollection handles POST /api/v1/evaluations/collections
func (h *Handlers) HandleCreateCollection(ctx *executioncontext.ExecutionContext, w http_wrappers.ResponseWrapper) {

	w.Error("Not implemented", 501, ctx.RequestID)

}

// HandleGetCollection handles GET /api/v1/evaluations/collections/{collection_id}
func (h *Handlers) HandleGetCollection(ctx *executioncontext.ExecutionContext, w http_wrappers.ResponseWrapper) {

	// Extract collection_id from path
	//pathParts := strings.Split(ctx.Request.URI(), "/")
	//collectionID := pathParts[len(pathParts)-1]

	w.Error("Not implemented", 501, ctx.RequestID)

}

// HandleUpdateCollection handles PUT /api/v1/evaluations/collections/{collection_id}
func (h *Handlers) HandleUpdateCollection(ctx *executioncontext.ExecutionContext, w http_wrappers.ResponseWrapper) {

	w.Error("Not implemented", 501, ctx.RequestID)

}

// HandlePatchCollection handles PATCH /api/v1/evaluations/collections/{collection_id}
func (h *Handlers) HandlePatchCollection(ctx *executioncontext.ExecutionContext, w http_wrappers.ResponseWrapper) {

	w.Error("Not implemented", 501, ctx.RequestID)
}

// HandleDeleteCollection handles DELETE /api/v1/evaluations/collections/{collection_id}
func (h *Handlers) HandleDeleteCollection(ctx *executioncontext.ExecutionContext, w http_wrappers.ResponseWrapper) {

	w.Error("Not implemented", 501, ctx.RequestID)

}
