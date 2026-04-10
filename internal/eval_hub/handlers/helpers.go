package handlers

import (
	"context"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/http_wrappers"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
)

type allowedPatch struct {
	Path   string
	Op     api.PatchOp
	Prefix bool
}

func CreatePage(ctx *executioncontext.ExecutionContext, total int, offset int, limit int, r http_wrappers.RequestWrapper) (*api.Page, error) {
	// Calculate pagination info
	hasNext := offset+limit < total
	var nextHref *api.HRef
	if hasNext {
		href, err := url.Parse(r.URI())
		if err != nil {
			ctx.Logger.Error("Failed to parse request URI", "uri", r.URI(), "error", err)
			return nil, serviceerrors.NewServiceError(messages.InternalServerError, "Error", err.Error())
		}
		q := href.Query()
		if !q.Has("offset") {
			q.Add("offset", strconv.Itoa(offset+limit))
		} else {
			q.Set("offset", strconv.Itoa(offset+limit))
		}
		href.RawQuery = q.Encode()
		nextHref = &api.HRef{Href: href.String()}
	}

	return &api.Page{
		First:      &api.HRef{Href: r.URI()},
		Next:       nextHref,
		Limit:      limit,
		TotalCount: total,
	}, nil
}

func DecodeParam(v string) string {
	decoded, err := url.QueryUnescape(v)
	if err != nil {
		return v
	}
	return decoded
}

func GetParam[T string | int | bool](r http_wrappers.RequestWrapper, name string, optional bool, defaultValue T) (T, error) {
	rawValues := r.Query(name)
	// Ignore empty repeated query values before joining
	values := make([]string, 0, len(rawValues))
	for _, value := range rawValues {
		if value != "" {
			values = append(values, value)
		}
	}
	if len(values) == 0 {
		if !optional {
			return defaultValue, serviceerrors.NewServiceError(messages.QueryParameterRequired, "ParameterName", name)
		}
		return defaultValue, nil
	}
	switch any(defaultValue).(type) {
	case string:
		// we support multiple values for a single parameter by joining them with a comma
		var sb strings.Builder
		for i, value := range values {
			if i > 0 {
				sb.WriteString(",")
			}
			sb.WriteString(DecodeParam(value))
		}
		return any(sb.String()).(T), nil
	case int:
		v, err := strconv.Atoi(values[0])
		if err != nil {
			return defaultValue, serviceerrors.NewServiceError(messages.QueryParameterInvalid, "ParameterName", name, "Type", "integer", "Value", values[0])
		}
		return any(v).(T), nil
	case bool:
		v, err := strconv.ParseBool(values[0])
		if err != nil {
			return defaultValue, serviceerrors.NewServiceError(messages.QueryParameterInvalid, "ParameterName", name, "Type", "boolean", "Value", values[0])
		}
		return any(v).(T), nil
	default:
		// should never get here
		return any(fmt.Sprintf("%v", values[0])).(T), nil
	}
}

func CheckScope(filter *abstractions.QueryFilter) error {
	// owner and scope are mutually exclusive
	mismatchedParams := []string{"owner", "scope"}
	if filter.HasParams(mismatchedParams...) {
		return serviceerrors.NewServiceError(messages.QueryParameterMismatch, "ParameterNames", strings.Join(mismatchedParams, ","))
	}

	// scope matches to other fields in the filter
	// scope==system ==> owner EQ system
	// scope==tenant ==> owner NE system
	if scope, ok := filter.Params["scope"]; ok {
		switch scope {
		case abstractions.ScopeSystem, abstractions.ScopeTenant:
			return nil
		default:
			return serviceerrors.NewServiceError(messages.QueryParameterValueInvalid, "ParameterName", "scope", "AllowedValues", strings.Join([]string{abstractions.ScopeSystem, abstractions.ScopeTenant}, "|"))
		}
	}

	return nil
}

func CommonListFilters(r http_wrappers.RequestWrapper, extraParams ...string) (*abstractions.QueryFilter, error) {
	// note that a user can not search by tenant
	limit, err := GetParam(r, "limit", true, 50)
	if err != nil {
		return nil, err
	}
	if limit < 0 {
		return nil, serviceerrors.NewServiceError(messages.QueryParameterInvalid, "ParameterName", "limit", "Type", "positive integer", "Value", strconv.Itoa(limit))
	}

	offset, err := GetParam(r, "offset", true, 0)
	if err != nil {
		return nil, err
	}
	if offset < 0 {
		return nil, serviceerrors.NewServiceError(messages.QueryParameterInvalid, "ParameterName", "offset", "Type", "positive integer", "Value", strconv.Itoa(offset))
	}

	name, err := GetParam(r, "name", true, "")
	if err != nil {
		return nil, err
	}

	tags, err := GetParam(r, "tags", true, "")
	if err != nil {
		return nil, err
	}

	owner, err := GetParam(r, "owner", true, "")
	if err != nil {
		return nil, err
	}

	params := map[string]any{
		"name":  name,
		"tags":  tags,
		"owner": owner,
	}

	for _, param := range extraParams {
		value, err := GetParam(r, param, true, "")
		if err != nil {
			return nil, err
		}
		if value != "" {
			params[param] = value
		}
	}

	return &abstractions.QueryFilter{
		Limit:  limit,
		Offset: offset,
		Params: params,
	}, nil
}

func getAllParams(r http_wrappers.RequestWrapper, allowedParams ...string) []string {
	uri, err := url.Parse(r.URI())
	if err != nil {
		return []string{}
	}
	params := slices.Collect(maps.Keys(uri.Query()))
	return slices.DeleteFunc(params, func(p string) bool {
		return slices.Contains(allowedParams, p)
	})
}

// isAllowedPatch returns true if the JSON Patch path targets a valid field.
func isAllowedPatch(patches []allowedPatch, operation api.PatchOp, path string) bool {
	// test exact matches first
	for _, patch := range patches {
		if patch.Path == path && patch.Op == operation {
			return true
		}
	}
	// test prefix matches next
	for _, patch := range patches {
		if (patch.Prefix && patch.Op == operation) && strings.HasPrefix(path, patch.Path+"/") {
			return true
		}
	}
	return false
}

func (h *Handlers) verifyPatches(ctx context.Context, patches api.Patch, allowedPatches []allowedPatch) error {
	for i := range patches {
		if err := h.validate.StructCtx(ctx, &patches[i]); err != nil {
			return serviceerrors.NewServiceError(messages.RequestValidationFailed, "Error", err.Error())
		}
		if patches[i].Op != api.PatchOpReplace && patches[i].Op != api.PatchOpAdd && patches[i].Op != api.PatchOpRemove {
			return serviceerrors.NewServiceError(messages.InvalidPatchOperation, "Operation", string(patches[i].Op), "AllowedOperations", strings.Join([]string{string(api.PatchOpReplace), string(api.PatchOpAdd), string(api.PatchOpRemove)}, ", "))
		}
		if patches[i].Path == "" {
			return serviceerrors.NewServiceError(messages.InvalidJSONRequest, "Error", "Invalid patch path")
		}
		if !isAllowedPatch(allowedPatches, patches[i].Op, patches[i].Path) {
			return serviceerrors.NewServiceError(messages.UnallowedPatch, "Operation", patches[i].Op, "Path", patches[i].Path)
		}
	}
	return nil
}

// GetJobBenchmarks returns the effective benchmark list for a job:
// from the job's collection when set, otherwise from job.Benchmarks.
func GetJobBenchmarks(job *api.EvaluationJobResource, collection *api.CollectionResource) ([]api.EvaluationBenchmarkConfig, error) {
	if job.Collection != nil && job.Collection.ID != "" {
		if collection == nil {
			return nil, serviceerrors.NewServiceError(
				messages.InternalServerError,
				"Error", fmt.Sprintf("Failed to resolve collection %s", job.Collection.ID),
			)
		}
		if len(collection.Benchmarks) == 0 {
			return nil, serviceerrors.NewServiceError(
				messages.CollectionEmpty,
				"CollectionID", job.Collection.ID,
			)
		}
		var mergedBenchmarks []api.EvaluationBenchmarkConfig
		for _, benchmark := range collection.Benchmarks {
			benchmark := mergeBenchmarkParameters(benchmark, job.Collection.Benchmarks)
			mergedBenchmarks = append(mergedBenchmarks, benchmark)
		}
		return mergedBenchmarks, nil
	}
	if len(job.Benchmarks) == 0 {
		return nil, serviceerrors.NewServiceError(
			messages.EvaluationJobEmpty,
			"EvaluationJobID", job.Resource.ID,
		)
	}
	return job.Benchmarks, nil
}

func mergeBenchmarkParameters(benchmark api.CollectionBenchmarkConfig, jobBenchmarks []api.EvaluationBenchmarkConfig) api.EvaluationBenchmarkConfig {
	parameters := map[string]any{}
	for _, jobBenchmark := range jobBenchmarks {
		if jobBenchmark.ProviderID == benchmark.ProviderID {
			maps.Copy(parameters, jobBenchmark.Parameters)
		}
	}
	for key, value := range benchmark.Parameters {
		if isEmpty(value) {
			delete(parameters, key)
		} else {
			parameters[key] = value
		}
	}
	return api.EvaluationBenchmarkConfig{
		Ref:          benchmark.Ref,
		ProviderID:   benchmark.ProviderID,
		Weight:       benchmark.Weight,
		PrimaryScore: benchmark.PrimaryScore,
		PassCriteria: benchmark.PassCriteria,
		TestDataRef:  benchmark.TestDataRef,
		Parameters:   parameters,
	}
}

// isEmpty returns true if the value is nil or empty so that it can be removed from the list of parameters.
func isEmpty(value any) bool {
	if value == nil {
		return true
	}
	switch v := value.(type) {
	case string:
		return v == ""
	}
	return false
}
