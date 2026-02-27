package sql

import (
	"database/sql"

	"github.com/eval-hub/eval-hub/internal/messages"
	se "github.com/eval-hub/eval-hub/internal/serviceerrors"
)

func (s *SQLStorage) getTotalCount(tableName string, params map[string]any) (int, error) {
	countQuery, countArgs, err := createCountEntitiesStatement(s.sqlConfig.Driver, tableName, params)
	if err != nil {
		return 0, err
	}

	var totalCount int
	if len(countArgs) > 0 {
		err = s.queryRow(nil, countQuery, countArgs...).Scan(&totalCount)
	} else {
		err = s.queryRow(nil, countQuery).Scan(&totalCount)
	}
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		s.logger.Error("Failed to count evaluation jobs", "error", err)
		return 0, se.NewServiceError(messages.QueryFailed, "Type", "evaluation jobs", "Error", err.Error())
	}
	return totalCount, nil
}
