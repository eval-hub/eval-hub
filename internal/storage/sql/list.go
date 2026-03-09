package sql

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"slices"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/messages"
	"github.com/eval-hub/eval-hub/internal/serviceerrors"
	se "github.com/eval-hub/eval-hub/internal/serviceerrors"
	"github.com/eval-hub/eval-hub/internal/storage/sql/shared"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func getTypeFromTableName(tableName string) string {
	switch tableName {
	case shared.TABLE_EVALUATIONS:
		return "evaluation jobs"
	case shared.TABLE_PROVIDERS:
		return "providers"
	case shared.TABLE_COLLECTIONS:
		return "collections"
	}
	return "unknown"
}

func listEntities[T api.EvaluationJobResource | api.ProviderResource | api.CollectionResource](s *SQLStorage, txn *sql.Tx, tableName string, filter *abstractions.QueryFilter) (*abstractions.QueryResults[T], error) {
	filter = filter.ExtractQueryParams()
	params := filter.Params
	limit := filter.Limit
	offset := filter.Offset

	if err := shared.ValidateFilter(slices.Collect(maps.Keys(params)), s.statementsFactory.GetAllowedFilterColumns(tableName)); err != nil {
		return nil, err
	}

	typeName := getTypeFromTableName(tableName)

	// Get total count (with filter if provided)
	totalCount, err := s.getTotalCount(txn, tableName, filter.Params, typeName)
	if err != nil {
		return nil, err
	}

	// Build the list query with pagination and filters
	listQuery, listArgs := s.statementsFactory.CreateListEntitiesStatement(s.tenant, tableName, limit, offset, params)
	s.logger.Info(fmt.Sprintf("List %s query", typeName), "query", listQuery, "args", listArgs, "params", params, "limit", limit, "offset", offset)

	// Query the database
	rows, err := s.query(txn, listQuery, listArgs...)
	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to list %s", typeName), "error", err)
		return nil, serviceerrors.NewServiceError(messages.QueryFailed, "Type", typeName, "Error", err.Error())
	}
	defer rows.Close()

	// Process rows
	var items []T
	for rows.Next() {
		resource, err := scanResource[T](s, rows, tableName)
		if err != nil {
			return nil, err
		}
		if resource == nil {
			totalCount--
			continue
		}
		items = append(items, any(*resource).(T))
	}

	if err = rows.Err(); err != nil {
		s.logger.Error(fmt.Sprintf("Error iterating %s rows", typeName), "error", err)
		return nil, serviceerrors.NewServiceError(messages.QueryFailed, "Type", typeName, "Error", err.Error())
	}

	return &abstractions.QueryResults[T]{
		Items:      items,
		TotalCount: totalCount,
	}, nil
}

func scanResource[T api.EvaluationJobResource | api.ProviderResource | api.CollectionResource](s *SQLStorage, rows *sql.Rows, tableName string) (*T, error) {
	query := shared.EntityQuery{}
	err := s.statementsFactory.ScanRowForEntity(s.tenant, tableName, rows, &query)
	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to scan %s row", getTypeFromTableName(tableName)), "error", err)
		return nil, serviceerrors.NewServiceError(messages.DatabaseOperationFailed, "Type", getTypeFromTableName(tableName), "ResourceId", query.Resource.ID, "Error", err.Error())
	}

	switch tableName {
	case shared.TABLE_EVALUATIONS:
		storedEntity := EvaluationJobEntity{}
		err = json.Unmarshal([]byte(query.EntityJSON), &storedEntity)
		if err == nil {
			resource, err := constructEvaluationResource(s.logger, &query, query.Status, &storedEntity)
			var t T = any(*resource).(T)
			return &t, err
		}
	case shared.TABLE_PROVIDERS:
		storedEntity := api.ProviderConfig{}
		err = json.Unmarshal([]byte(query.EntityJSON), &storedEntity)
		if err == nil {
			resource := &api.ProviderResource{
				Resource:       query.Resource,
				ProviderConfig: storedEntity,
			}
			var t T = any(*resource).(T)
			return &t, nil
		}
	case shared.TABLE_COLLECTIONS:
		storedEntity := api.CollectionConfig{}
		err = json.Unmarshal([]byte(query.EntityJSON), &storedEntity)
		if err == nil {
			resource := &api.CollectionResource{
				Resource:         query.Resource,
				CollectionConfig: storedEntity,
			}
			var t T = any(*resource).(T)
			return &t, nil
		}
	default:
		err = se.NewServiceError(messages.InternalServerError, "Error", fmt.Sprintf("Unknown table name: %s", tableName))
	}

	return nil, err
}

func constructEvaluationResource(logger *slog.Logger, query *shared.EntityQuery, status string, evaluationEntity *EvaluationJobEntity) (*api.EvaluationJobResource, error) {
	if query == nil {
		logger.Error("Failed to construct evaluation job resource", "error", "Missing evaluation query")
		// Post-read validation: no writes done, so do not request rollback.
		return nil, se.NewServiceError(messages.InternalServerError, "Error", "Evaluation resource query does not exist")
	}
	if evaluationEntity == nil {
		logger.Error("Failed to construct evaluation job resource", "error", "Evaluation entity does not exist", "id", query.Resource.ID)
		// Post-read validation: no writes done, so do not request rollback.
		return nil, se.NewServiceError(messages.InternalServerError, "Error", "Evaluation entity does not exist")
	}
	if evaluationEntity.Config == nil {
		logger.Error("Failed to construct evaluation job resource", "error", "Evaluation config does not exist", "id", query.Resource.ID)
		// Post-read validation: no writes done, so do not request rollback.
		return nil, se.NewServiceError(messages.InternalServerError, "Error", "Evaluation config does not exist")
	}
	if evaluationEntity.Status == nil {
		evaluationEntity.Status = &api.EvaluationJobStatus{}
	}

	overAllState := evaluationEntity.Status.State
	if status != "" {
		if s, err := api.GetOverallState(status); err == nil {
			overAllState = s
		}
	}
	statusObject := evaluationEntity.Status
	statusObject.State = overAllState
	statusObject.Message = evaluationEntity.Status.Message

	return &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource:           query.Resource,
			MLFlowExperimentID: query.MLFlowExperimentID,
		},
		Status:              statusObject,
		EvaluationJobConfig: *evaluationEntity.Config,
		Results:             evaluationEntity.Results,
	}, nil
}
