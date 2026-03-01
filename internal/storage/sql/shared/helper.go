package shared

import (
	"maps"
	"slices"
	"strings"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/messages"
	"github.com/eval-hub/eval-hub/internal/serviceerrors"
)

func ValidateFilter(filter []string, allowedColumns []string) error {
	for _, key := range filter {
		if !slices.Contains(allowedColumns, key) {
			return serviceerrors.NewServiceError(messages.QueryBadParameter, "ParameterName", key, "AllowedParameters", strings.Join(allowedColumns, ", "))
		}
	}
	return nil
}

func getParams(params *abstractions.QueryFilter) map[string]any {
	filter := maps.Clone(params.Params)
	maps.DeleteFunc(filter, func(k string, v any) bool {
		return v == "" // delete empty values
	})
	return filter
}

// Returns the limit, offset, and filtered params
func ExtractQueryParams(filter *abstractions.QueryFilter) *abstractions.QueryFilter {
	params := getParams(filter)
	// TODO - remove this delete after adding owner in storage layer
	delete(params, "owner")
	return &abstractions.QueryFilter{
		Limit:  filter.Limit,
		Offset: filter.Offset,
		Params: params,
	}
}
