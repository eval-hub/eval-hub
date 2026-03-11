package shared

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/eval-hub/eval-hub/internal/messages"
	"github.com/eval-hub/eval-hub/internal/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func ValidateFilter(filter []string, allowedColumns []string) error {
	for _, key := range filter {
		if !slices.Contains(allowedColumns, key) {
			return serviceerrors.NewServiceError(messages.QueryBadParameter, "ParameterName", key, "AllowedParameters", strings.Join(allowedColumns, ", "))
		}
	}
	return nil
}

func getString(value any) string {
	if v, ok := value.(string); ok {
		return v
	}
	return fmt.Sprintf("%v", value)
}

func GetValues(key string, values any) ([]any, string) {
	switch key {
	case "tags":
		if strings.Contains(getString(values), ",") {
			var results []any
			for _, value := range strings.Split(getString(values), ",") {
				results = append(results, value)
			}
			return results, "AND"
		} else if strings.Contains(values.(string), "|") {
			var results []any
			for _, value := range strings.Split(getString(values), "|") {
				results = append(results, value)
			}
			return results, "OR"
		} else {
			return []any{values}, "AND"
		}
	default:
		return []any{values}, "AND"
	}
}

// createFilterStatement builds a WHERE clause and args from the filter.
// It validates each key against the table's allowlist, sorts keys deterministically,
// and returns both the clause and args in matching order. Returns an error if any
// filter key is not in the allowlist (fail closed).
func CreateFilterStatement(tenant api.Tenant, s SQLStatementsFactory, filter map[string]any, orderBy string, limit int, offset int, tableName string) (string, []any) {
	var args []any
	var sb strings.Builder

	index := 1

	haveWhere := false
	haveOperator := false

	// we must always filter by tenant_id if it exists
	if !tenant.IsEmpty() {
		sb.WriteString(" WHERE ")
		cond, condArgs := s.CreateEntityFilterCondition("tenant_id", tenant.String(), index, tableName)
		index += len(condArgs)
		sb.WriteString(cond)
		args = append(args, condArgs...)
		if len(filter) > 0 {
			sb.WriteString(" AND ")
		}
		haveWhere = true
		haveOperator = true
	}

	if len(filter) > 0 {
		allowed := s.GetAllowedFilterColumns(tableName)
		// Sort keys for deterministic query generation to avoid caching issues
		keys := slices.Sorted(maps.Keys(filter))
		for _, key := range keys {
			values := filter[key]
			if slices.Contains(allowed, key) {
				allValues, operator := GetValues(key, values)
				for _, value := range allValues {
					if !haveWhere {
						sb.WriteString(" WHERE ")
						haveWhere = true
					} else if !haveOperator {
						sb.WriteString(" ")
						sb.WriteString(operator)
						sb.WriteString(" ")
					}
					haveOperator = false
					cond, condArgs := s.CreateEntityFilterCondition(key, value, index, tableName)
					index += len(condArgs)
					sb.WriteString(cond)
					args = append(args, condArgs...)
				}
			} else {
				// should never get here as we validate the filter before calling this function
				s.GetLogger().Warn("Disallowed filter key", "key", key, "tableName", tableName)
			}
		}
	}

	if orderBy != "" {
		cond, condArgs := s.CreateEntityFilterCondition("ORDER BY", orderBy, index, tableName)
		sb.WriteString(" ")
		sb.WriteString(cond)
		// args can be empty if the condition is just the ORDER BY keyword
		if len(condArgs) > 0 {
			index += len(condArgs)
			args = append(args, condArgs...)
		}
	}
	if limit > 0 {
		cond, condArgs := s.CreateEntityFilterCondition("LIMIT", limit, index, tableName)
		index += len(condArgs)
		sb.WriteString(" ")
		sb.WriteString(cond)
		args = append(args, condArgs...)
	}
	if offset > 0 {
		cond, condArgs := s.CreateEntityFilterCondition("OFFSET", offset, index, tableName)
		index += len(condArgs)
		sb.WriteString(" ")
		sb.WriteString(cond)
		args = append(args, condArgs...)
	}

	return sb.String(), args
}
