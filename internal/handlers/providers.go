package handlers

import (
	"context"
	"time"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/constants"
	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"github.com/eval-hub/eval-hub/internal/http_wrappers"
	"github.com/eval-hub/eval-hub/internal/logging"
	"github.com/eval-hub/eval-hub/internal/messages"
	"github.com/eval-hub/eval-hub/internal/serialization"
	"github.com/eval-hub/eval-hub/internal/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// HandleListProviders handles GET /api/v1/evaluations/providers
func (h *Handlers) HandleListProviders(ctx *executioncontext.ExecutionContext, r http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {
	providerIdParam := r.Query("id")
	benchmarksParam := r.Query("benchmarks")
	providerId := ""
	benchmarks := true

	if len(providerIdParam) > 0 {
		providerId = providerIdParam[0]
	}
	if len(benchmarksParam) > 0 {
		benchmarks = benchmarksParam[0] != "false"
	}

	providers := []api.ProviderResource{}

	for _, p := range h.providerConfigs {
		if providerId != "" && p.Resource.ID != providerId {
			continue
		}
		if !benchmarks {
			p.Benchmarks = []api.BenchmarkResource{}
		}
		providers = append(providers, p)

	}

	w.WriteJSON(api.ProviderResourceList{
		// TODO: Implement pagination
		Page: api.Page{
			TotalCount: len(providers),
		},
		Items: providers,
	}, 200)
}

func (h *Handlers) HandleCreateProvider(ctx *executioncontext.ExecutionContext, req http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {
	storage := h.storage.WithLogger(ctx.Logger).WithContext(ctx.Ctx).WithTenant(ctx.Tenant)

	logging.LogRequestStarted(ctx)

	now := time.Now()

	request := &api.ProviderRequest{}

	err := h.withSpan(
		ctx,
		func(runtimeCtx context.Context) error {
			// get the body bytes from the context
			bodyBytes, err := req.BodyAsBytes()
			if err != nil {
				return err
			}
			err = serialization.Unmarshal(h.validate, ctx.WithContext(runtimeCtx), bodyBytes, request)
			if err != nil {
				return err
			}
			if err := h.validateProvider(request, storage); err != nil {
				w.Error(err, ctx.RequestID)
				return err
			}
			return nil
		},
		"validation",
		"validate-user-provider",
	)
	if err != nil {
		return
	}

	var provider *api.ProviderResource

	_ = h.withSpan(
		ctx,
		func(runtimeCtx context.Context) error {
			provider = &api.ProviderResource{
				Resource: api.Resource{
					ID:        request.ID,
					CreatedAt: &now,
					Owner:     ctx.User,
					Tenant:    &ctx.Tenant,
					ReadOnly:  false,
				},
				ProviderConfig: api.ProviderConfig{
					Name:        request.Name,
					Description: request.Description,
					Benchmarks:  request.Benchmarks,
					Runtime:     nil,
				},
			}
			err := storage.WithContext(runtimeCtx).CreateUserProvider(provider)
			if err != nil {
				w.Error(err, ctx.RequestID)
				return err
			} else {
				w.WriteJSON(provider, 201)
				return nil
			}
		},
		"storage",
		"store-user-provider",
		"provider.id", request.ID,
	)
}

func (h *Handlers) HandleGetProvider(ctx *executioncontext.ExecutionContext, req http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {
	storage := h.storage.WithLogger(ctx.Logger).WithContext(ctx.Ctx).WithTenant(ctx.Tenant)

	logging.LogRequestStarted(ctx)

	providerId := req.PathValue(constants.PATH_PARAMETER_PROVIDER_ID)
	if providerId == "" {
		w.Error(serviceerrors.NewServiceError(messages.MissingPathParameter, "ParameterName", constants.PATH_PARAMETER_PROVIDER_ID), ctx.RequestID)
		return
	}

	_ = h.withSpan(
		ctx,
		func(runtimeCtx context.Context) error {
			provider, err := storage.GetUserProvider(providerId)
			if err != nil {
				w.Error(err, ctx.RequestID)
				return err
			}
			w.WriteJSON(provider, 200)
			return nil
		},
		"storage",
		"get-user-provider",
		"provider.id", providerId,
	)
}

func (h *Handlers) HandleUpdateProvider(ctx *executioncontext.ExecutionContext, req http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {
	providerId := req.PathValue(constants.PATH_PARAMETER_PROVIDER_ID)
	if providerId == "" {
		w.Error(serviceerrors.NewServiceError(messages.MissingPathParameter, "ParameterName", constants.PATH_PARAMETER_PROVIDER_ID), ctx.RequestID)
		return
	}
	w.Error(serviceerrors.NewServiceError(messages.NotImplemented, "APi", req.Path()), ctx.RequestID)
}

func (h *Handlers) HandlePatchProvider(ctx *executioncontext.ExecutionContext, req http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {
	providerId := req.PathValue(constants.PATH_PARAMETER_PROVIDER_ID)
	if providerId == "" {
		w.Error(serviceerrors.NewServiceError(messages.MissingPathParameter, "ParameterName", constants.PATH_PARAMETER_PROVIDER_ID), ctx.RequestID)
		return
	}
	w.Error(serviceerrors.NewServiceError(messages.NotImplemented, "APi", req.Path()), ctx.RequestID)
}

func (h *Handlers) HandleDeleteProvider(ctx *executioncontext.ExecutionContext, req http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {
	storage := h.storage.WithLogger(ctx.Logger).WithContext(ctx.Ctx).WithTenant(ctx.Tenant)

	logging.LogRequestStarted(ctx)

	providerId := req.PathValue(constants.PATH_PARAMETER_PROVIDER_ID)
	if providerId == "" {
		err := serviceerrors.NewServiceError(messages.MissingPathParameter, "ParameterName", constants.PATH_PARAMETER_PROVIDER_ID)
		w.Error(err, ctx.RequestID)
		return
	}

	_ = h.withSpan(
		ctx,
		func(runtimeCtx context.Context) error {
			err := storage.DeleteUserProvider(providerId)
			if err != nil {
				w.Error(err, ctx.RequestID)
				return err
			}
			w.WriteJSON(nil, 204)
			return nil
		},
		"storage",
		"delete-user-provider",
		"provider.id", providerId,
	)
}

func (h *Handlers) validateProvider(provider *api.ProviderRequest, storage abstractions.Storage) error {
	err := h.isUniqueId(provider.ID, storage)
	if err != nil {
		return err
	}
	return nil
}

func (h *Handlers) isUniqueId(id string, storage abstractions.Storage) error {
	if _, exists := h.providerConfigs[id]; exists {
		return serviceerrors.NewServiceError(messages.ProviderIDNotUnique, "ProviderID", "id")
	}
	if _, err := storage.GetUserProvider(id); err == nil {
		return serviceerrors.NewServiceError(messages.ProviderIDNotUnique, "ProviderID", "id")
	}
	return nil
}
