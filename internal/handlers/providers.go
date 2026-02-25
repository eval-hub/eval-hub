package handlers

import (
	"context"
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

// HandleListProviders handles GET /api/v1/evaluations/providers
func (h *Handlers) HandleListProviders(ctx *executioncontext.ExecutionContext, r http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {

	benchmarksParam := r.Query("benchmarks")
	providerId := ""
	benchmarks := true

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
	id := common.GUID()

	request := &api.ProviderConfig{}

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
			// TODO: do we need any extra validation for the provider config?
			return nil
		},
		"validation",
		"validate-user-provider",
		"provider.id", id,
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
					ID:        id,
					CreatedAt: &now,
					Owner:     ctx.User,
					Tenant:    &ctx.Tenant,
					ReadOnly:  false,
				},
				ProviderConfig: *request,
			}
			err := storage.WithContext(runtimeCtx).CreateProvider(provider)
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
		"provider.id", id,
	)
}

func (h *Handlers) getSystemProvider(providerId string) (*api.ProviderResource, error) {
	provider, ok := h.providerConfigs[providerId]
	if !ok {
		return nil, serviceerrors.NewServiceError(messages.ResourceNotFound, "provider", providerId)
	}
	return &provider, nil
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

			provider, err := h.getSystemProvider(providerId)
			if err != nil {

				ctx.Logger.Warn("System provider not found", "provider_id", providerId)
				provider, err = storage.GetProvider(providerId)
				if err != nil {
					ctx.Logger.Error("User provider not found", "provider_id", providerId)
					err = serviceerrors.NewServiceError(messages.ResourceNotFound, "Type", "provider", "ResourceId", providerId)
					w.Error(err, ctx.RequestID)
					return err
				}
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
			err := storage.DeleteProvider(providerId)
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
