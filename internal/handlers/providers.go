package handlers

import (
	"context"
	"encoding/json"
	"fmt"
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
	"github.com/eval-hub/eval-hub/pkg/api"
)

var (
	// these are the allowed patches for the user-defined provider config
	allowedPatches = []allowedPatch{
		{Path: "/name", Op: api.PatchOpReplace, Prefix: false},

		{Path: "/title", Op: api.PatchOpAdd, Prefix: false},
		{Path: "/title", Op: api.PatchOpRemove, Prefix: false},
		{Path: "/title", Op: api.PatchOpReplace, Prefix: false},

		{Path: "/description", Op: api.PatchOpAdd, Prefix: false},
		{Path: "/description", Op: api.PatchOpRemove, Prefix: false},
		{Path: "/description", Op: api.PatchOpReplace, Prefix: false},

		{Path: "/tags", Op: api.PatchOpAdd, Prefix: true},
		{Path: "/tags", Op: api.PatchOpRemove, Prefix: true},
		{Path: "/tags", Op: api.PatchOpReplace, Prefix: true},

		{Path: "/custom", Op: api.PatchOpAdd, Prefix: true},
		{Path: "/custom", Op: api.PatchOpRemove, Prefix: true},
		{Path: "/custom", Op: api.PatchOpReplace, Prefix: true},

		{Path: "/runtime", Op: api.PatchOpReplace, Prefix: true},

		{Path: "/benchmarks", Op: api.PatchOpReplace, Prefix: true},
	}
)

type allowedPatch struct {
	Path   string
	Op     api.PatchOp
	Prefix bool
}

func (h *Handlers) HandleCreateProvider(ctx *executioncontext.ExecutionContext, req http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {
	storage := h.storage.WithLogger(ctx.Logger).WithContext(ctx.Ctx).WithTenant(ctx.Tenant).WithOwner(ctx.User)

	logging.LogRequestStarted(ctx)

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
					CreatedAt: time.Now(),
					Owner:     ctx.User,
					Tenant:    ctx.Tenant,
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

func (h *Handlers) filterSystemProviders(filter map[string]any) []api.ProviderResource {
	filteredProviders := []api.ProviderResource{}
	for _, p := range h.providerConfigs {
		if len(filter) == 0 {
			filteredProviders = append(filteredProviders, p)
			continue
		}
		for k, v := range filter {
			pass := true
			// we don't query by owner, tenant, or status as they are not relevant for system providers
			switch k {
			case "name":
				if p.ProviderConfig.Name != v {
					pass = false
				}
			case "tags":
				if !slices.Contains(p.ProviderConfig.Tags, v.(string)) {
					pass = false
				}
			}
			if pass {
				filteredProviders = append(filteredProviders, p)
			}
		}
	}
	return filteredProviders
}

// HandleListProviders handles GET /api/v1/evaluations/providers
func (h *Handlers) HandleListProviders(ctx *executioncontext.ExecutionContext, req http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {
	storage := h.storage.WithLogger(ctx.Logger).WithContext(ctx.Ctx).WithTenant(ctx.Tenant).WithOwner(ctx.User)

	logging.LogRequestStarted(ctx)

	allowedParams := []string{"limit", "offset", "benchmarks", "name", "tags", "system_defined", "benchmarks", "owner"}
	badParams := getAllParams(req, allowedParams...)
	if len(badParams) > 0 {
		// just report the first bad parameter
		w.Error(serviceerrors.NewServiceError(messages.QueryBadParameter, "ParameterName", badParams[0], "AllowedParameters", strings.Join(allowedParams, ", ")), ctx.RequestID)
		return
	}

	filter, err := CommonListFilters(req)
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}

	providers := []api.ProviderResource{}

	if IncludeSystemDefined(req) {
		providers = h.filterSystemProviders(filter.ExtractQueryParams().Params)
		ctx.Logger.Info(fmt.Sprintf("Included %d system defined providers", len(providers)))
	}

	totalCount := len(providers)

	// first check to see if the system providers are enough for the paging
	if len(providers) > 0 {
		if len(providers) < (filter.Limit + filter.Offset) {
			userFilter := &abstractions.QueryFilter{
				Limit:  max(0, filter.Limit-len(providers)),
				Offset: max(0, filter.Offset-len(providers)),
				Params: filter.Params,
			}
			queryResults, err := storage.GetProviders(userFilter)
			if err != nil {
				w.Error(err, ctx.RequestID)
				return
			}
			providers = append(providers, queryResults.Items...)
			totalCount += queryResults.TotalCount
		}
	} else {
		// no system providers so normal flow for user providers
		queryResults, err := storage.GetProviders(filter)
		if err != nil {
			w.Error(err, ctx.RequestID)
			return
		}
		providers = append(providers, queryResults.Items...)
		totalCount += queryResults.TotalCount
	}

	// remove the benchmarks if requested
	benchmarks, err := GetParam(req, "benchmarks", true, true)
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}
	if !benchmarks {
		for i := range providers {
			providers[i].Benchmarks = []api.BenchmarkResource{}
		}
	}

	page, err := CreatePage(ctx, totalCount, filter.Offset, filter.Limit, req)
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}

	result := api.ProviderResourceList{
		Page:  *page,
		Items: providers[:min(len(providers), filter.Limit)],
	}

	w.WriteJSON(result, 200)
}

// isAllowedPatch returns true if the JSON Patch path targets a valid field.
func isAllowedPatch(operation api.PatchOp, path string) bool {
	// test exact matches first
	for _, patch := range allowedPatches {
		if patch.Path == path && patch.Op == operation {
			return true
		}
	}
	// test prefix matches next
	for _, patch := range allowedPatches {
		if (patch.Prefix && patch.Op == operation) && strings.HasPrefix(path, patch.Path+"/") {
			return true
		}
	}
	return false
}

func (h *Handlers) getSystemProvider(providerId string) *api.ProviderResource {
	provider, ok := h.providerConfigs[providerId]
	if !ok {
		return nil
	}
	return &provider
}

func (h *Handlers) HandleGetProvider(ctx *executioncontext.ExecutionContext, req http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {
	storage := h.storage.WithLogger(ctx.Logger).WithContext(ctx.Ctx).WithTenant(ctx.Tenant).WithOwner(ctx.User)

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
	storage := h.storage.WithLogger(ctx.Logger).WithContext(ctx.Ctx).WithTenant(ctx.Tenant).WithOwner(ctx.User)

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
	storage := h.storage.WithLogger(ctx.Logger).WithContext(ctx.Ctx).WithTenant(ctx.Tenant).WithOwner(ctx.User)

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
					return serviceerrors.NewServiceError(messages.InvalidPatchOperation, "Operation", string(patches[i].Op), "AllowedOperations", strings.Join([]string{string(api.PatchOpReplace), string(api.PatchOpAdd), string(api.PatchOpRemove)}, ", "))
				}
				if patches[i].Path == "" {
					return serviceerrors.NewServiceError(messages.InvalidJSONRequest, "Error", "Invalid patch path")
				}
				if !isAllowedPatch(patches[i].Op, patches[i].Path) {
					return serviceerrors.NewServiceError(messages.UnallowedPatch, "Operation", patches[i].Op, "Path", patches[i].Path)
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
	storage := h.storage.WithLogger(ctx.Logger).WithContext(ctx.Ctx).WithTenant(ctx.Tenant).WithOwner(ctx.User)

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
