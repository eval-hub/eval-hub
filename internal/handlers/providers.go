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
		"validate-provider",
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
		"create-provider",
		"provider.id", id,
	)
}

// HandleListProviders handles GET /api/v1/evaluations/providers
func (h *Handlers) HandleListProviders(ctx *executioncontext.ExecutionContext, r http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {
	storage := h.storage.WithLogger(ctx.Logger).WithContext(ctx.Ctx).WithTenant(ctx.Tenant)

	logging.LogRequestStarted(ctx)

	_ = h.withSpan(
		ctx,
		func(runtimeCtx context.Context) error {
			filter, err := CommonListFilters(r)
			if err != nil {
				w.Error(err, ctx.RequestID)
				return err
			}

			benchmarksParam := r.Query("benchmarks")
			includeBenchmarks := true
			if len(benchmarksParam) > 0 {
				includeBenchmarks = benchmarksParam[0] != "false"
			}

			filter.Params["benchmarks"] = includeBenchmarks

			systemDefined := IncludeSystemDefined(r)

			ctx.Logger.Info("Include system defined providers", "system_defined", systemDefined)

			providers := []api.ProviderResource{}

			if systemDefined {
				for _, p := range h.providerConfigs {
					providers = append(providers, p)
				}
			}

			queryResults, err := storage.GetProviders(filter)
			if err != nil {
				w.Error(err, ctx.RequestID)
				return err
			}

			allItems := append(providers, queryResults.Items...)
			page := api.Page{
				TotalCount: len(providers) + queryResults.TotalStored,
			}
			if includeBenchmarks {
				w.WriteJSON(api.ProviderResourceList{Page: page, Items: allItems}, 200)
			} else {
				itemsNoBenchmarks := make([]map[string]any, 0, len(allItems))
				for i := range allItems {
					bytes, err := json.Marshal(allItems[i])
					if err != nil {
						w.Error(serviceerrors.NewServiceError(messages.InternalServerError, "Error", err.Error()), ctx.RequestID)
						return err
					}
					var m map[string]any
					if err := json.Unmarshal(bytes, &m); err != nil {
						w.Error(serviceerrors.NewServiceError(messages.InternalServerError, "Error", err.Error()), ctx.RequestID)
						return err
					}
					delete(m, "benchmarks")
					itemsNoBenchmarks = append(itemsNoBenchmarks, m)
				}
				body := map[string]any{"total_count": page.TotalCount, "limit": page.Limit, "items": itemsNoBenchmarks}
				if page.First != nil {
					body["first"] = page.First
				}
				if page.Next != nil {
					body["next"] = page.Next
				}
				w.WriteJSON(body, 200)
			}

			return nil
		},
		"storage",
		"list-providers",
	)
}

// isImmutablePatchPath returns true if the JSON Patch path targets an immutable field.
func isImmutablePatchPath(path string) bool {
	switch path {
	case "/resource", "/created_at", "/updated_at":
		return true
	}
	return strings.HasPrefix(path, "/resource/")
}

func (h *Handlers) getSystemProvider(providerId string) *api.ProviderResource {
	provider, ok := h.providerConfigs[providerId]
	if !ok {
		return nil
	}
	return &provider
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
			provider := h.getSystemProvider(providerId)
			if provider == nil {
				userProvider, err := storage.GetProvider(providerId)
				if err != nil {
					w.Error(err, ctx.RequestID)
					return err
				}
				provider = userProvider
			}

			w.WriteJSON(provider, 200)
			return nil
		},
		"storage",
		"get-provider",
		"provider.id", providerId,
	)
}

func (h *Handlers) HandleUpdateProvider(ctx *executioncontext.ExecutionContext, req http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {
	storage := h.storage.WithLogger(ctx.Logger).WithContext(ctx.Ctx).WithTenant(ctx.Tenant)

	logging.LogRequestStarted(ctx)

	providerId := req.PathValue(constants.PATH_PARAMETER_PROVIDER_ID)
	if providerId == "" {
		w.Error(serviceerrors.NewServiceError(messages.MissingPathParameter, "ParameterName", constants.PATH_PARAMETER_PROVIDER_ID), ctx.RequestID)
		return
	}

	request := &api.ProviderResource{}

	err := h.withSpan(
		ctx,
		func(runtimeCtx context.Context) error {
			if h.getSystemProvider(providerId) != nil {
				return serviceerrors.NewServiceError(messages.SystemProvider, "ProviderId", providerId)
			}

			// get the body bytes from the context
			bodyBytes, err := req.BodyAsBytes()
			if err != nil {
				return err
			}
			err = serialization.Unmarshal(h.validate, ctx.WithContext(runtimeCtx), bodyBytes, request)
			if err != nil {
				return err
			}
			return nil
		},
		"validation",
		"validate-provider-update",
		"provider.id", providerId,
	)
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}

	_ = h.withSpan(
		ctx,
		func(runtimeCtx context.Context) error {
			provider, err := storage.UpdateProvider(providerId, request)
			if err != nil {
				w.Error(err, ctx.RequestID)
				return err
			}
			w.WriteJSON(provider, 200)
			return nil
		},
		"storage",
		"update-provider",
		"provider.id", providerId,
	)
}

func (h *Handlers) HandlePatchProvider(ctx *executioncontext.ExecutionContext, req http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {
	storage := h.storage.WithLogger(ctx.Logger).WithContext(ctx.Ctx).WithTenant(ctx.Tenant)

	logging.LogRequestStarted(ctx)

	providerId := req.PathValue(constants.PATH_PARAMETER_PROVIDER_ID)
	if providerId == "" {
		w.Error(serviceerrors.NewServiceError(messages.MissingPathParameter, "ParameterName", constants.PATH_PARAMETER_PROVIDER_ID), ctx.RequestID)
		return
	}

	var patches api.Patch

	err := h.withSpan(
		ctx,
		func(runtimeCtx context.Context) error {
			if h.getSystemProvider(providerId) != nil {
				return serviceerrors.NewServiceError(messages.SystemProvider, "ProviderId", providerId)
			}

			bodyBytes, err := req.BodyAsBytes()
			if err != nil {
				return err
			}
			if err = json.Unmarshal(bodyBytes, &patches); err != nil {
				return serviceerrors.NewServiceError(messages.InvalidJSONRequest, "Error", err.Error())
			}
			for i := range patches {
				if err = h.validate.StructCtx(ctx.Ctx, &patches[i]); err != nil {
					return serviceerrors.NewServiceError(messages.RequestValidationFailed, "Error", err.Error())
				}
				if patches[i].Op != api.PatchOpReplace && patches[i].Op != api.PatchOpAdd && patches[i].Op != api.PatchOpRemove {
					return serviceerrors.NewServiceError(messages.InvalidJSONRequest, "Error", "Invalid patch operation")
				}
				if patches[i].Path == "" {
					return serviceerrors.NewServiceError(messages.InvalidJSONRequest, "Error", "Invalid patch path")
				}
				if isImmutablePatchPath(patches[i].Path) {
					return serviceerrors.NewServiceError(messages.InvalidJSONRequest, "Error",
						"Patch path '"+patches[i].Path+"' targets an immutable field and cannot be modified")
				}
			}
			return nil
		},
		"validation",
		"validate-provider-patch",
		"provider.id", providerId,
	)
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}

	_ = h.withSpan(
		ctx,
		func(runtimeCtx context.Context) error {
			provider, err := storage.PatchProvider(providerId, &patches)
			if err != nil {
				w.Error(err, ctx.RequestID)
				return err
			}
			w.WriteJSON(provider, 200)
			return nil
		},
		"storage",
		"patch-provider",
		"provider.id", providerId,
	)
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
		"delete-provider",
		"provider.id", providerId,
	)
}
