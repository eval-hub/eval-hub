package handlers

import (
	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/httpwrappers"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// HandleGetInfo returns build metadata and the queues available to the request tenant.
func (h *Handlers) HandleGetInfo(ctx *executioncontext.ExecutionContext, _ httpwrappers.RequestWrapper, w httpwrappers.ResponseWrapper) {
	queues := make([]api.QueueInfo, 0)
	if lister, ok := h.runtime.(abstractions.QueueLister); ok {
		listed, err := lister.ListQueues(ctx.Ctx, ctx.Tenant.String())
		if err != nil {
			w.Error(err, ctx.RequestID)
			return
		}
		if listed != nil {
			queues = listed
		}
	}

	service := h.serviceConfig.Service
	w.WriteJSON(api.InfoResponse{
		Version:   service.Version,
		Build:     service.Build,
		BuildDate: service.BuildDate,
		GitHash:   service.GitHash,
		Queues:    queues,
	}, 200)
}
