package shared

import (
	"slices"
	"strings"

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
