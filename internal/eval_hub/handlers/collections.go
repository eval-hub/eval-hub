package handlers

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/common"
	"github.com/eval-hub/eval-hub/internal/eval_hub/constants"
	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/httpwrappers"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serialization"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/internal/logging"
	"github.com/eval-hub/eval-hub/pkg/api"
)

var (
	// allowedCollectionPatches are the allowed patches for user-defined collection config fields.
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

		{Path: "/domains", Op: api.PatchOpAdd, Prefix: true},
		{Path: "/domains", Op: api.PatchOpRemove, Prefix: true},
		{Path: "/domains", Op: api.PatchOpReplace, Prefix: true},

		{Path: "/tasks", Op: api.PatchOpAdd, Prefix: true},
		{Path: "/tasks", Op: api.PatchOpRemove, Prefix: true},
		{Path: "/tasks", Op: api.PatchOpReplace, Prefix: true},

		{Path: "/modalities", Op: api.PatchOpAdd, Prefix: true},
		{Path: "/modalities", Op: api.PatchOpRemove, Prefix: true},
		{Path: "/modalities", Op: api.PatchOpReplace, Prefix: true},

		{Path: "/industries", Op: api.PatchOpAdd, Prefix: true},
		{Path: "/industries", Op: api.PatchOpRemove, Prefix: true},
		{Path: "/industries", Op: api.PatchOpReplace, Prefix: true},

		{Path: "/ai_entities", Op: api.PatchOpAdd, Prefix: true},
		{Path: "/ai_entities", Op: api.PatchOpRemove, Prefix: true},
		{Path: "/ai_entities", Op: api.PatchOpReplace, Prefix: true},

		// Tenant-controlled pin ordering (state field)
		{Path: "/state/pinned_order", Op: api.PatchOpAdd, Prefix: false},
		{Path: "/state/pinned_order", Op: api.PatchOpReplace, Prefix: false},
	}
)

// entireBenchmarkPatchPath matches JSON Patch paths that replace or add the full benchmarks array (/benchmarks)
// or a single array element (/benchmarks/<index> or /benchmarks/-). It does not match field-level paths
// (e.g. /benchmarks/0/id).
var entireBenchmarkPatchPath = regexp.MustCompile(`^/benchmarks(?:/(-|\d+))?$`)

// HandleListCollections handles GET /api/v1/evaluations/collections
func (h *Handlers) HandleListCollections(ctx *executioncontext.ExecutionContext, req httpwrappers.RequestWrapper, w httpwrappers.ResponseWrapper) {
	storage := h.storage.WithLogger(ctx.Logger).WithContext(ctx.Ctx).WithTenant(ctx.Tenant).WithOwner(ctx.User)

	var ofilter *abstractions.QueryFilter

	err := h.withSpan(
		ctx,
		func(runtimeCtx context.Context) error {
			filter, err := CommonListFilters(req, "category", "scope")

			logging.LogRequestStarted(ctx, "filter", filter)

			if err != nil {
				return err
			}

			// Translate scope=curated to an internal filter key
			if scope, ok := filter.Params["scope"]; ok && scope == "curated" {
				filter.Params["scope_curated"] = "true"
				delete(filter.Params, "scope")
			} else {
				if err = CheckScope(filter); err != nil {
					return err
				}
			}

			allowedParams := []string{"limit", "offset", "name", "category", "tags", "owner", "scope",
				"domains", "tasks", "modalities", "industries", "ai_entities", "sort_by"}
			badParams := getAllParams(req, allowedParams...)
			if len(badParams) > 0 {
				return serviceerrors.NewServiceError(messages.QueryBadParameter, "ParameterName", badParams[0], "AllowedParameters", strings.Join(allowedParams, ", "))
			}

			// Handle new array filters (use first value from each param)
			for _, key := range []string{"domains", "tasks", "modalities", "industries", "ai_entities"} {
				if vals := req.Query(key); len(vals) > 0 && vals[0] != "" {
					filter.Params[key] = vals[0]
				}
			}

			ofilter = filter
			return nil
		},
		"validation",
		"validate-collections-filter",
	)
	if err != nil {
		w.Error(err, ctx.RequestID)
		return
	}

	var count int
	var totalCount int

	_ = h.withSpan(
		ctx,
		func(runtimeCtx context.Context) error {
			scoped := storage.WithContext(runtimeCtx)
			collections, err := scoped.GetCollections(ofilter)
			if err != nil {
				w.Error(err, ctx.RequestID)
				return err
			}

			page, err := CreatePage(ctx, collections.TotalCount, ofilter.Offset, ofilter.Limit, req)
			if err != nil {
				w.Error(err, ctx.RequestID)
				return err
			}

			// Enrich benchmarks from providers and auto-populate classification fields
			ptrs := make([]*api.CollectionResource, len(collections.Items))
			for i := range collections.Items {
				ptrs[i] = &collections.Items[i]
			}
			EnrichCollectionFromProviders(scoped, ptrs...)

			result := api.CollectionResourceList{
				Page:  *page,
				Items: collections.Items,
			}

			count = len(collections.Items)
			totalCount = collections.TotalCount
			w.WriteJSON(result, 200, "count", strconv.Itoa(count), "total_count", strconv.Itoa(totalCount))
			return nil
		},
		"storage",
		"list-collections",
		"count", strconv.Itoa(count),
		"total_count", strconv.Itoa(totalCount),
	)
}

// EnrichCollectionFromProviders enriches each collection's benchmarks with Description and URL
// from the provider config, and auto-populates Domains/Tasks/Modalities on the collection
// from the union of its benchmarks' corresponding fields when not explicitly set.
func EnrichCollectionFromProviders(storage abstractions.Storage, collections ...*api.CollectionResource) {
	loaded := make(map[string]*api.ProviderResource)
	failed := make(map[string]struct{})
	for _, coll := range collections {
		if coll == nil {
			continue
		}
		domainsSet := make(map[string]struct{})
		tasksSet := make(map[string]struct{})
		modalitiesSet := make(map[string]struct{})

		for j := range coll.Benchmarks {
			b := &coll.Benchmarks[j]
			pid, bid := b.ProviderID, b.ID
			if pid == "" || bid == "" {
				continue
			}
			b.URL = ""
			if _, miss := failed[pid]; miss {
				continue
			}
			p, ok := loaded[pid]
			if !ok {
				var err error
				p, err = storage.GetProvider(pid)
				if err != nil || p == nil {
					failed[pid] = struct{}{}
					continue
				}
				loaded[pid] = p
			}
			for k := range p.Benchmarks {
				pb := &p.Benchmarks[k]
				if pb.ID != bid {
					continue
				}
				if pb.URL != "" {
					b.URL = pb.URL
				}
				for _, d := range pb.Domains {
					domainsSet[d] = struct{}{}
				}
				for _, t := range pb.Tasks {
					tasksSet[t] = struct{}{}
				}
				for _, m := range pb.Modalities {
					modalitiesSet[m] = struct{}{}
				}
				break
			}
		}

		// Auto-populate classification fields from benchmarks if not explicitly set
		if len(coll.Domains) == 0 {
			for d := range domainsSet {
				coll.Domains = append(coll.Domains, d)
			}
		}
		if len(coll.Tasks) == 0 {
			for t := range tasksSet {
				coll.Tasks = append(coll.Tasks, t)
			}
		}
		if len(coll.Modalities) == 0 {
			for m := range modalitiesSet {
				coll.Modalities = append(coll.Modalities, m)
			}
		}
	}
}

// EnrichBenchmarkURLsFromProviders clears each benchmark URL (when provider_id and id are set), then sets it
// from the provider definition when a matching benchmark with a non-empty URL exists.
func EnrichBenchmarkURLsFromProviders(storage abstractions.Storage, collections ...*api.CollectionResource) {
	loaded := make(map[string]*api.ProviderResource)
	failed := make(map[string]struct{})
	for _, coll := range collections {
		if coll == nil {
			continue
		}
		for j := range coll.Benchmarks {
			b := &coll.Benchmarks[j]
			pid, bid := b.ProviderID, b.ID
			if pid == "" || bid == "" {
				continue
			}
			b.URL = ""
			if _, miss := failed[pid]; miss {
				continue
			}
			p, ok := loaded[pid]
			if !ok {
				var err error
				p, err = storage.GetProvider(pid)
				if err != nil || p == nil {
					failed[pid] = struct{}{}
					continue
				}
				loaded[pid] = p
			}
			for k := range p.Benchmarks {
				if p.Benchmarks[k].ID == bid && p.Benchmarks[k].URL != "" {
					b.URL = p.Benchmarks[k].URL
					break
				}
			}
		}
	}
}

// enrichEntireBenchmarkPatchValues rewrites patch operation values for full benchmarks array or single-element
// add/replace ops so benchmark URLs are filled from the provider before the patch is applied in storage.
func enrichEntireBenchmarkPatchValues(storage abstractions.Storage, patches *api.Patch) error {
	for i := range *patches {
		op := &(*patches)[i]
		if op.Op != api.PatchOpReplace && op.Op != api.PatchOpAdd {
			continue
		}
		if !entireBenchmarkPatchPath.MatchString(op.Path) {
			continue
		}
		raw, err := json.Marshal(op.Value)
		if err != nil {
			return err
		}
		if op.Path == "/benchmarks" {
			var benchmarks []api.CollectionBenchmarkConfig
			if err := json.Unmarshal(raw, &benchmarks); err != nil {
				continue
			}
			tmp := &api.CollectionResource{
				CollectionConfig: api.CollectionConfig{Benchmarks: benchmarks},
			}
			EnrichBenchmarkURLsFromProviders(storage, tmp)
			enc, err := json.Marshal(tmp.Benchmarks)
			if err != nil {
				return err
			}
			var v any
			if err := json.Unmarshal(enc, &v); err != nil {
				return err
			}
			op.Value = v
			continue
		}
		var b api.CollectionBenchmarkConfig
		if err := json.Unmarshal(raw, &b); err != nil {
			continue
		}
		if b.ProviderID == "" || b.ID == "" {
			continue
		}
		tmp := &api.CollectionResource{
			CollectionConfig: api.CollectionConfig{
				Benchmarks: []api.CollectionBenchmarkConfig{b},
			},
		}
		EnrichBenchmarkURLsFromProviders(storage, tmp)
		enc, err := json.Marshal(tmp.Benchmarks[0])
		if err != nil {
			return err
		}
		var v any
		if err := json.Unmarshal(enc, &v); err != nil {
			return err
		}
		op.Value = v
	}
	return nil
}

// HandleCreateCollection handles POST /api/v1/evaluations/collections
func (h *Handlers) HandleCreateCollection(ctx *executioncontext.ExecutionContext, req httpwrappers.RequestWrapper, w httpwrappers.ResponseWrapper) {
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
			scoped := storage.WithContext(runtimeCtx)
			collectionResource = &api.CollectionResource{
				Resource: api.Resource{
					ID:        id,
					CreatedAt: time.Now(),
					Owner:     ctx.User,
					Tenant:    ctx.Tenant,
				},
				CollectionConfig: *collection,
			}
			EnrichBenchmarkURLsFromProviders(scoped, collectionResource)
			err := scoped.CreateCollection(collectionResource)
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
func (h *Handlers) HandleGetCollection(ctx *executioncontext.ExecutionContext, req httpwrappers.RequestWrapper, w httpwrappers.ResponseWrapper) {
	storage := h.storage.WithLogger(ctx.Logger).WithContext(ctx.Ctx).WithTenant(ctx.Tenant).WithOwner(ctx.User)

	logging.LogRequestStarted(ctx)

	// Extract ID from path
	collectionID := req.PathValue(constants.PathParameterCollectionID)
	if collectionID == "" {
		w.Error(serviceerrors.NewServiceError(messages.MissingPathParameter, "ParameterName", constants.PathParameterCollectionID), ctx.RequestID)
		return
	}

	_ = h.withSpan(
		ctx,
		func(runtimeCtx context.Context) error {
			scoped := storage.WithContext(runtimeCtx)
			response, err := scoped.GetCollection(collectionID)
			if err != nil {
				w.Error(err, ctx.RequestID)
				return err
			}
			EnrichCollectionFromProviders(scoped, response)
			w.WriteJSON(response, 200)
			return nil
		},
		"storage",
		"get-collection",
		"collection.id", collectionID,
	)
}

// HandleUpdateCollection handles PUT /api/v1/evaluations/collections/{collection_id}
func (h *Handlers) HandleUpdateCollection(ctx *executioncontext.ExecutionContext, req httpwrappers.RequestWrapper, w httpwrappers.ResponseWrapper) {
	storage := h.storage.WithLogger(ctx.Logger).WithContext(ctx.Ctx).WithTenant(ctx.Tenant).WithOwner(ctx.User)

	logging.LogRequestStarted(ctx)

	// Extract ID from path
	collectionID := req.PathValue(constants.PathParameterCollectionID)
	if collectionID == "" {
		w.Error(serviceerrors.NewServiceError(messages.MissingPathParameter, "ParameterName", constants.PathParameterCollectionID), ctx.RequestID)
		return
	}

	request := &api.CollectionConfig{}

	err := h.withSpan(
		ctx,
		func(runtimeCtx context.Context) error {
			// get the body bytes from the context
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
			scoped := storage.WithContext(runtimeCtx)

			// 403 for system or curated (curation_order > 0) collections
			existing, err := scoped.GetCollection(collectionID)
			if err != nil {
				w.Error(err, ctx.RequestID)
				return err
			}
			if existing.Resource.IsSystemResource() || existing.CurationOrder > 0 {
				w.Error(serviceerrors.NewServiceError(messages.ReadOnlyCollection, "CollectionID", collectionID), ctx.RequestID)
				return nil
			}

			toUpdate := &api.CollectionResource{CollectionConfig: *request}
			EnrichCollectionFromProviders(scoped, toUpdate)
			if _, err = scoped.UpdateCollection(collectionID, &toUpdate.CollectionConfig); err != nil {
				w.Error(err, ctx.RequestID)
				return err
			}
			// Increment version counter on successful mutation
			updated, err := scoped.IncrementCollectionVersionCounter(collectionID)
			if err != nil {
				w.Error(err, ctx.RequestID)
				return err
			}
			w.WriteJSON(updated, 200)
			return nil
		},
		"storage",
		"update-collection",
		"collection.id", collectionID,
	)
}

// HandlePatchCollection handles PATCH /api/v1/evaluations/collections/{collection_id}
func (h *Handlers) HandlePatchCollection(ctx *executioncontext.ExecutionContext, req httpwrappers.RequestWrapper, w httpwrappers.ResponseWrapper) {
	storage := h.storage.WithLogger(ctx.Logger).WithContext(ctx.Ctx).WithTenant(ctx.Tenant).WithOwner(ctx.User)

	logging.LogRequestStarted(ctx)

	// Extract ID from path
	collectionID := req.PathValue(constants.PathParameterCollectionID)
	if collectionID == "" {
		w.Error(serviceerrors.NewServiceError(messages.MissingPathParameter, "ParameterName", constants.PathParameterCollectionID), ctx.RequestID)
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
			scoped := storage.WithContext(runtimeCtx)

			// 403 for system or curated (curation_order > 0) collections
			existing, err := scoped.GetCollection(collectionID)
			if err != nil {
				w.Error(err, ctx.RequestID)
				return err
			}
			if existing.Resource.IsSystemResource() || existing.CurationOrder > 0 {
				w.Error(serviceerrors.NewServiceError(messages.ReadOnlyCollection, "CollectionID", collectionID), ctx.RequestID)
				return nil
			}

			if err := enrichEntireBenchmarkPatchValues(scoped, &patches); err != nil {
				w.Error(err, ctx.RequestID)
				return err
			}
			if _, err = scoped.PatchCollection(collectionID, &patches); err != nil {
				w.Error(err, ctx.RequestID)
				return err
			}
			// Increment version counter on successful mutation
			patched, err := scoped.IncrementCollectionVersionCounter(collectionID)
			if err != nil {
				w.Error(err, ctx.RequestID)
				return err
			}
			w.WriteJSON(patched, 200)
			return nil
		},
		"storage",
		"patch-collection",
		"collection.id", collectionID,
	)
}

// HandleCloneCollection handles POST /api/v1/evaluations/collections/{collection_id}/clones
func (h *Handlers) HandleCloneCollection(ctx *executioncontext.ExecutionContext, req httpwrappers.RequestWrapper, w httpwrappers.ResponseWrapper) {
	storage := h.storage.WithLogger(ctx.Logger).WithContext(ctx.Ctx).WithTenant(ctx.Tenant).WithOwner(ctx.User)

	logging.LogRequestStarted(ctx)

	sourceID := req.PathValue(constants.PathParameterCollectionID)
	if sourceID == "" {
		w.Error(serviceerrors.NewServiceError(messages.MissingPathParameter, "ParameterName", constants.PathParameterCollectionID), ctx.RequestID)
		return
	}

	_ = h.withSpan(
		ctx,
		func(runtimeCtx context.Context) error {
			scoped := storage.WithContext(runtimeCtx)

			// Fetch source collection
			source, err := scoped.GetCollection(sourceID)
			if err != nil {
				w.Error(err, ctx.RequestID)
				return err
			}

			// Parse optional body as overrides (fields not required — copied from source)
			overrides := &api.CollectionConfig{}
			if bodyBytes, bErr := req.BodyAsBytes(); bErr == nil && len(bodyBytes) > 0 {
				// Unmarshal without validation — required fields come from the source
				_ = json.Unmarshal(bodyBytes, overrides)
			}

			// Build the new config: start from source, apply non-zero overrides
			newConfig := source.CollectionConfig
			if overrides.Name != "" {
				newConfig.Name = overrides.Name
			}
			if overrides.Description != "" {
				newConfig.Description = overrides.Description
			}
			if overrides.Category != "" {
				newConfig.Category = overrides.Category
			}
			if len(overrides.Tags) > 0 {
				newConfig.Tags = overrides.Tags
			}
			if overrides.PassCriteria != nil {
				newConfig.PassCriteria = overrides.PassCriteria
			}
			if len(overrides.Benchmarks) > 0 {
				newConfig.Benchmarks = overrides.Benchmarks
			}
			if len(overrides.Domains) > 0 {
				newConfig.Domains = overrides.Domains
			}
			if len(overrides.Tasks) > 0 {
				newConfig.Tasks = overrides.Tasks
			}
			if len(overrides.Modalities) > 0 {
				newConfig.Modalities = overrides.Modalities
			}
			if len(overrides.Industries) > 0 {
				newConfig.Industries = overrides.Industries
			}
			if len(overrides.AIEntities) > 0 {
				newConfig.AIEntities = overrides.AIEntities
			}
			// CurationOrder is admin-only — never copied or overridden by users
			newConfig.CurationOrder = 0

			newID := common.GUID()
			now := time.Now()
			newCollection := &api.CollectionResource{
				Resource: api.Resource{
					ID:             newID,
					CreatedAt:      now,
					UpdatedAt:      now,
					Owner:          ctx.User,
					Tenant:         ctx.Tenant,
					VersionCounter: 1,
				},
				CollectionConfig: newConfig,
				State: &api.CollectionState{
					DerivedFrom: sourceID,
				},
			}

			EnrichCollectionFromProviders(scoped, newCollection)

			if err = scoped.CreateCollection(newCollection); err != nil {
				w.Error(err, ctx.RequestID)
				return err
			}

			w.WriteJSON(newCollection, 201)
			return nil
		},
		"storage",
		"clone-collection",
		"collection.source_id", sourceID,
	)
}

// HandleDeleteCollection handles DELETE /api/v1/evaluations/collections/{collection_id}
func (h *Handlers) HandleDeleteCollection(ctx *executioncontext.ExecutionContext, req httpwrappers.RequestWrapper, w httpwrappers.ResponseWrapper) {
	storage := h.storage.WithLogger(ctx.Logger).WithContext(ctx.Ctx).WithTenant(ctx.Tenant).WithOwner(ctx.User)

	logging.LogRequestStarted(ctx)

	// Extract ID from path
	collectionID := req.PathValue(constants.PathParameterCollectionID)
	if collectionID == "" {
		w.Error(serviceerrors.NewServiceError(messages.MissingPathParameter, "ParameterName", constants.PathParameterCollectionID), ctx.RequestID)
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
