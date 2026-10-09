package handlers

import (
	"errors"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/httpwrappers"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// HandleGetInfo returns build metadata and the hardware profiles available to the request tenant.
func (h *Handlers) HandleGetInfo(ctx *executioncontext.ExecutionContext, _ httpwrappers.RequestWrapper, w httpwrappers.ResponseWrapper) {
	profiles := make([]api.HardwareProfileInfo, 0)
	if lister, ok := h.runtime.(abstractions.HardwareProfileLister); ok {
		listed, err := lister.ListHardwareProfiles(ctx.Ctx, ctx.Tenant.String())
		if err != nil {
			var serviceErr abstractions.ServiceError
			if errors.As(err, &serviceErr) {
				w.Error(err, ctx.RequestID)
				return
			}

			ctx.Logger.Error("failed to list tenant hardware profiles", "error", err)
			w.ErrorWithMessageCode(ctx.RequestID, messages.InternalServerError, "Error", "Failed to list tenant hardware profiles")
			return
		}
		if listed != nil {
			profiles = listed
		}
	}

	service := h.serviceConfig.Service
	w.WriteJSON(api.InfoResponse{
		Version:          service.Version,
		Build:            service.Build,
		BuildDate:        service.BuildDate,
		GitHash:          service.GitHash,
		HardwareProfiles: profiles,
	}, 200)
}
