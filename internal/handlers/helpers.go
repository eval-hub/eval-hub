package handlers

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"github.com/eval-hub/eval-hub/internal/http_wrappers"
	"github.com/eval-hub/eval-hub/internal/messages"
	"github.com/eval-hub/eval-hub/internal/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func CreatePage(total int, offset int, limit int, ctx *executioncontext.ExecutionContext, r http_wrappers.RequestWrapper) (*api.Page, error) {
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

func GetParam[T string | int | bool](r http_wrappers.RequestWrapper, name string, optional bool, defaultValue T) (T, error) {
	values := r.Query(name)
	if (len(values) == 0) || (values[0] == "") {
		if !optional {
			return defaultValue, serviceerrors.NewServiceError(messages.QueryParameterRequired, "ParameterName", name)
		}
		return defaultValue, nil
	}
	switch any(defaultValue).(type) {
	case string:
		return any(values[0]).(T), nil
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

func CommonListFilters(r http_wrappers.RequestWrapper) (*abstractions.QueryFilter, error) {
	limit, err := GetParam(r, "limit", true, 50)
	if err != nil {
		return nil, err
	}
	if limit < 0 {
		return nil, serviceerrors.NewServiceError(messages.QueryParameterInvalid, "ParameterName", "limit", "Type", "integer", "Value", strconv.Itoa(limit))
	}
	offset, err := GetParam(r, "offset", true, 0)
	if err != nil {
		return nil, err
	}
	if offset < 0 {
		return nil, serviceerrors.NewServiceError(messages.QueryParameterInvalid, "ParameterName", "offset", "Type", "integer", "Value", strconv.Itoa(offset))
	}
	status, err := GetParam(r, "status", true, "")
	if err != nil {
		return nil, err
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

	tenant, err := GetParam(r, "tenant", true, "")
	if err != nil {
		return nil, err
	}

	return &abstractions.QueryFilter{
		Limit:  limit,
		Offset: offset,
		Params: map[string]any{
			"status":    status,
			"name":      name,
			"tags":      tags,
			"owner":     owner,
			"tenant_id": tenant,
		},
	}, nil

}
