package sql

import (
	"database/sql"
	"encoding/json"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	se "github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/internal/eval_hub/storage/sql/shared"
	"github.com/eval-hub/eval-hub/pkg/api"
)

type EvaluationJobEntity struct {
	Config  *api.EvaluationJobConfig  `json:"config" validate:"required"`
	Status  *api.EvaluationJobStatus  `json:"status,omitempty"`
	Results *api.EvaluationJobResults `json:"results,omitempty"`
}

// #######################################################################
// Evaluation job operations
// #######################################################################
func (s *sqlStorage) CreateEvaluationJob(evaluation *api.EvaluationJobResource) error {
	return s.withTransaction("create evaluation job", evaluation.Resource.ID, func(txn *sql.Tx) error {
		evaluationJSON, err := s.createEvaluationJobEntity(evaluation)
		if err != nil {
			return se.WithRollback(err)
		}
		addEntityStatement, args := s.statementsFactory.CreateEvaluationAddEntityStatement(evaluation, string(evaluationJSON))
		_, err = s.exec(txn, addEntityStatement, args...)
		if err != nil {
			return se.WithRollback(err)
		}
		s.logger.Info("Created evaluation job", "id", evaluation.Resource.ID, "addEntityStatement", addEntityStatement)
		return nil
	})
}

func (s *sqlStorage) createEvaluationJobEntity(evaluation *api.EvaluationJobResource) ([]byte, error) {
	evaluationEntity := &EvaluationJobEntity{
		Config:  &evaluation.EvaluationJobConfig,
		Status:  evaluation.Status,
		Results: evaluation.Results,
	}
	evaluationJSON, err := json.Marshal(evaluationEntity)
	if err != nil {
		return nil, se.NewServiceError(messages.InternalServerError, "Error", err.Error())
	}
	return evaluationJSON, nil
}

func (s *sqlStorage) GetEvaluationJob(id string) (*api.EvaluationJobResource, error) {
	return s.getEvaluationJobTransactional(nil, id)
}

func (s *sqlStorage) getEvaluationJobTransactional(txn *sql.Tx, id string) (*api.EvaluationJobResource, error) {
	return s.scanEvaluationJobTransactional(txn, id, false)
}

func (s *sqlStorage) getEvaluationJobTransactionalForUpdate(txn *sql.Tx, id string) (*api.EvaluationJobResource, error) {
	return s.scanEvaluationJobTransactional(txn, id, true)
}

func (s *sqlStorage) scanEvaluationJobTransactional(txn *sql.Tx, id string, forUpdate bool) (*api.EvaluationJobResource, error) {
	query := shared.EntityQuery{Resource: api.Resource{ID: id, Tenant: s.tenant}}
	var selectQuery string
	var selectArgs, queryArgs []any
	if forUpdate {
		selectQuery, selectArgs, queryArgs = s.statementsFactory.CreateEvaluationGetEntityForUpdateStatement(&query)
	} else {
		selectQuery, selectArgs, queryArgs = s.statementsFactory.CreateEvaluationGetEntityStatement(&query)
	}

	err := s.queryRow(txn, selectQuery, selectArgs...).Scan(queryArgs...)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, se.NewServiceError(messages.ResourceNotFound, "Type", "evaluation job", "ResourceId", id)
		}
		s.logger.Error("Failed to get evaluation job", "error", err, "id", id)
		return nil, se.WithRollback(se.NewServiceError(messages.DatabaseOperationFailed, "Type", "evaluation job", "ResourceId", id, "Error", err.Error()))
	}

	var evaluationJobEntity EvaluationJobEntity
	err = json.Unmarshal([]byte(query.EntityJSON), &evaluationJobEntity)
	if err != nil {
		s.logger.Error("Failed to unmarshal evaluation job entity", "error", err, "id", id)
		return nil, se.WithRollback(se.NewServiceError(messages.JSONUnmarshalFailed, "Type", "evaluation job", "Error", err.Error()))
	}

	status := ""
	job, err := constructEvaluationResource(s.logger, &query, status, &evaluationJobEntity)
	if err != nil {
		return nil, se.WithRollback(err)
	}
	return job, nil
}

func (s *sqlStorage) GetEvaluationJobs(filter *abstractions.QueryFilter) (*abstractions.QueryResults[api.EvaluationJobResource], error) {
	var txn *sql.Tx
	return listEntities[api.EvaluationJobResource](s, txn, shared.TableEvaluations, filter)
}

func (s *sqlStorage) DeleteEvaluationJob(id string) error {
	// Build the DELETE query
	deleteQuery, args := s.statementsFactory.CreateDeleteEntityStatement(s.tenant, shared.TableEvaluations, id)

	// Execute the DELETE query
	result, err := s.exec(nil, deleteQuery, args...)
	if err != nil {
		s.logger.Error("Failed to delete evaluation job", "error", err, "id", id)
		return se.WithRollback(se.NewServiceError(messages.DatabaseOperationFailed, "Type", "evaluation job", "ResourceId", id, "Error", err.Error()))
	}
	rows, rowsErr := result.RowsAffected()
	if rowsErr != nil {
		s.logger.Error("Failed to determine rows affected", "error", rowsErr, "id", id)
		return se.WithRollback(se.NewServiceError(messages.DatabaseOperationFailed, "Type", "evaluation job", "ResourceId", id, "Error", rowsErr.Error()))
	}
	if rows == 0 {
		s.logger.Debug("Evaluation job not found", "id", id)
		return se.NewServiceError(messages.ResourceNotFound, "Type", "evaluation job", "ResourceId", id)
	}
	s.logger.Info("Deleted evaluation job", "id", id)

	return nil
}
