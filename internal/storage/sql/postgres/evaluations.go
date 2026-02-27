package postgres

import (
	db "database/sql"
	"encoding/json"
	"time"

	// import the postgres driver - "pgx"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/messages"
	se "github.com/eval-hub/eval-hub/internal/serviceerrors"

	"github.com/eval-hub/eval-hub/internal/storage/sql"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// #######################################################################
// Evaluation job operations
// #######################################################################
func (s *PostgresStorage) CreateEvaluationJob(evaluation *api.EvaluationJobResource) error {
	jobID := evaluation.Resource.ID
	mlflowExperimentID := evaluation.Resource.MLFlowExperimentID

	err := sql.WithTransaction(s.pool, s.ctx, s.logger, "create evaluation job", jobID, func(txn *db.Tx) error {
		tenant := s.tenant

		evaluationJSON, err := sql.CreateEvaluationJobEntity(evaluation)
		if err != nil {
			return se.WithRollback(err)
		}
		addEntityStatement := INSERT_EVALUATION_STATEMENT

		s.logger.Info("Creating evaluation job", "id", jobID, "tenant", tenant, "status", api.StatePending, "experiment_id", mlflowExperimentID)
		// (id, tenant_id, status, experiment_id, entity)

		e := sql.SQLExecutor{
			Db:  s.pool,
			Ctx: s.ctx,
			Txn: txn,
		}
		_, err = e.Exec(addEntityStatement, jobID, tenant, api.StatePending, mlflowExperimentID, string(evaluationJSON))
		if err != nil {
			return se.WithRollback(err)
		}

		return err
	})
	return err
}

func (s *PostgresStorage) GetEvaluationJob(id string) (*api.EvaluationJobResource, error) {
	e := sql.SQLExecutor{
		Db:  s.pool,
		Ctx: s.ctx,
	}
	return getEvaluationJobTransactional(id, e)
}

func (s *PostgresStorage) GetEvaluationJobs(filter abstractions.QueryFilter) (*abstractions.QueryResults[api.EvaluationJobResource], error) {

	filter = extractQueryParams(filter)
	params := filter.Params
	limit := filter.Limit
	offset := filter.Offset

	// Get total count (with filter if provided)
	countQuery, countArgs, err := createCountEntitiesStatement(TABLE_EVALUATIONS, filter.Params)
	if err != nil {
		return nil, err
	}

	e := sql.SQLExecutor{
		Db:  s.pool,
		Ctx: s.ctx,
	}

	var totalCount int
	err = e.QueryRow(countQuery, countArgs...).Scan(&totalCount)

	if err != nil {
		if err == db.ErrNoRows {
			return &abstractions.QueryResults[api.EvaluationJobResource]{
				Items:       make([]api.EvaluationJobResource, 0),
				TotalStored: 0,
				Errors:      nil,
			}, nil
		}
		s.logger.Error("Failed to count evaluation jobs", "error", err)
		return nil, se.NewServiceError(messages.QueryFailed, "Type", "evaluation jobs", "Error", err.Error())
	}

	// Build the list query with pagination and filters
	listQuery, listArgs, err := createListEntitiesStatement(TABLE_EVALUATIONS, limit, offset, params)
	if err != nil {
		return nil, err
	}
	s.logger.Info("List evaluations query", "query", listQuery, "args", listArgs, "params", params, "limit", limit, "offset", offset)

	// Query the database
	rows, err := e.Query(listQuery, listArgs...)
	if err != nil {
		s.logger.Error("Failed to list evaluation jobs", "error", err)
		return nil, se.NewServiceError(messages.QueryFailed, "Type", "evaluation jobs", "Error", err.Error())
	}
	defer rows.Close()

	// Process rows
	var constructErrs []string
	var items []api.EvaluationJobResource
	for rows.Next() {
		var dbID string
		var createdAt, updatedAt time.Time
		var tenantID string
		var statusStr string
		var experimentID string
		var entityJSON string

		err = rows.Scan(&dbID, &createdAt, &updatedAt, &tenantID, &statusStr, &experimentID, &entityJSON)
		if err != nil {
			s.logger.Error("Failed to scan evaluation job row", "error", err)
			return nil, se.NewServiceError(messages.DatabaseOperationFailed, "Type", "evaluation job", "ResourceId", dbID, "Error", err.Error())
		}

		// Unmarshal the entity JSON into EvaluationJobConfig
		var evaluationJobEntity sql.EvaluationJobEntity
		err = json.Unmarshal([]byte(entityJSON), &evaluationJobEntity)
		if err != nil {
			s.logger.Error("Failed to unmarshal evaluation job entity", "error", err, "id", dbID)
			return nil, se.NewServiceError(messages.JSONUnmarshalFailed, "Type", "evaluation job", "Error", err.Error())
		}

		// Construct the EvaluationJobResource
		resource, err := sql.ConstructEvaluationResource(tenantID, statusStr, nil, dbID, createdAt, updatedAt, experimentID, &evaluationJobEntity)
		if err != nil {
			constructErrs = append(constructErrs, err.Error())
			totalCount--
			continue
		}

		items = append(items, *resource)
	}

	if err = rows.Err(); err != nil {
		s.logger.Error("Error iterating evaluation job rows", "error", err)
		return nil, se.NewServiceError(messages.QueryFailed, "Type", "evaluation jobs", "Error", err.Error())
	}

	return &abstractions.QueryResults[api.EvaluationJobResource]{
		Items:       items,
		TotalStored: totalCount,
		Errors:      constructErrs,
	}, nil
}

func (s *PostgresStorage) DeleteEvaluationJob(id string) error {
	// we have to get the evaluation job and then update or delete the job so we need a transaction
	err := sql.WithTransaction(s.pool, s.ctx, s.logger, "delete evaluation job", id, func(txn *db.Tx) error {
		// check if the evaluation job exists, we do this otherwise we always return 204
		selectQuery := createCheckEntityExistsStatement(TABLE_EVALUATIONS)

		e := sql.SQLExecutor{
			Db:  s.pool,
			Ctx: s.ctx,
			Txn: txn,
		}
		var dbID string
		err := e.QueryRow(selectQuery, id).Scan(&dbID)
		if err != nil {
			if err == db.ErrNoRows {
				return se.NewServiceError(messages.ResourceNotFound, "Type", "evaluation job", "ResourceId", id)
			}
			return se.WithRollback(se.NewServiceError(messages.DatabaseOperationFailed, "Type", "evaluation job", "ResourceId", id, "Error", err.Error()))
		}

		// Build the DELETE query
		deleteQuery := createDeleteEntityStatement(TABLE_EVALUATIONS)

		// Execute the DELETE query
		_, err = e.Exec(deleteQuery, id)
		if err != nil {
			s.logger.Error("Failed to delete evaluation job", "error", err, "id", id)
			return se.WithRollback(se.NewServiceError(messages.DatabaseOperationFailed, "Type", "evaluation job", "ResourceId", id, "Error", err.Error()))
		}

		s.logger.Info("Deleted evaluation job", "id", id)

		return nil
	})
	return err
}

func (s *PostgresStorage) UpdateEvaluationJobStatus(id string, state api.OverallState, message *api.MessageInfo) error {
	// we have to get the evaluation job and update the status so we need a transaction
	err := sql.WithTransaction(s.pool, s.ctx, s.logger, "update evaluation job status", id, func(txn *db.Tx) error {
		// get the evaluation job
		e := sql.SQLExecutor{
			Db:  s.pool,
			Ctx: s.ctx,
			Txn: txn,
		}
		evaluationJob, err := getEvaluationJobTransactional(id, e)
		if err != nil {
			return err
		}
		switch evaluationJob.Status.State {
		case api.OverallStateCancelled:
			// if the job is already cancelled then we don't need to update the status
			// we don't treat this as an error for now, we just return 204
			return nil
		case api.OverallStateCompleted, api.OverallStateFailed:
			return se.NewServiceError(messages.JobCanNotBeCancelled, "Id", id, "Status", evaluationJob.Status.State)
		}
		if err := updateEvaluationJobStatusTxn(evaluationJob, state, message, e); err != nil {
			return err
		}
		s.logger.Info("Updated evaluation job status", "id", id, "overall_state", state, "message", message)
		return nil
	})
	return err
}

// UpdateEvaluationJobWithRunStatus runs in a transaction: fetches the job, merges RunStatusInternal into the entity, and persists.
func (s *PostgresStorage) UpdateEvaluationJob(id string, runStatus *api.StatusEvent) error {
	err := sql.WithTransaction(s.pool, s.ctx, s.logger, "update evaluation job", id, func(txn *db.Tx) error {

		e := sql.SQLExecutor{
			Db:  s.pool,
			Ctx: s.ctx,
			Txn: txn,
		}
		job, err := getEvaluationJobTransactional(id, e)
		if err != nil {
			return err
		}

		entity, err := sql.ApplyStatusEventToEvaluationJob(job, runStatus, e)
		if err != nil {
			return err
		}

		return updateEvaluationJobTxn(id, entity.Status.State, entity, e)
	})

	return err
}

func getEvaluationJobTransactional(id string, e sql.SQLExecutor) (*api.EvaluationJobResource, error) {

	selectQuery := GET_EVALUATION_STATEMENT
	// Query the database
	var dbID string
	var createdAt, updatedAt time.Time
	var tenantID string
	var statusStr string
	var experimentID string
	var entityJSON string

	err := e.QueryRow(selectQuery, id).Scan(&dbID, &createdAt, &updatedAt, &tenantID, &statusStr, &experimentID, &entityJSON)
	if err != nil {
		if err == db.ErrNoRows {
			return nil, se.NewServiceError(messages.ResourceNotFound, "Type", "evaluation job", "ResourceId", id)
		}
		// For now we differentiate between no rows found and other errors but this might be confusing
		return nil, se.WithRollback(se.NewServiceError(messages.DatabaseOperationFailed, "Type", "evaluation job", "ResourceId", id, "Error", err.Error()))
	}

	// Unmarshal the entity JSON into EvaluationJobConfig
	var evaluationJobEntity sql.EvaluationJobEntity
	err = json.Unmarshal([]byte(entityJSON), &evaluationJobEntity)
	if err != nil {
		return nil, se.NewServiceError(messages.JSONUnmarshalFailed, "Type", "evaluation job", "Error", err.Error())
	}

	job, err := sql.ConstructEvaluationResource(tenantID, statusStr, nil, dbID, createdAt, updatedAt, experimentID, &evaluationJobEntity)
	if err != nil {
		return nil, se.WithRollback(err)
	}
	return job, nil
}

func updateEvaluationJobTxn(id string, status api.OverallState, evaluationJob *sql.EvaluationJobEntity, e sql.SQLExecutor) error {
	entityJSON, err := json.Marshal(evaluationJob)
	if err != nil {
		// we should never get here
		return se.WithRollback(se.NewServiceError(messages.InternalServerError, "Error", err.Error()))
	}
	updateQuery, args := createUpdateEvaluationStatement(id, status, string(entityJSON))
	if err != nil {
		return se.WithRollback(err)
	}

	_, err = e.Exec(updateQuery, args...)
	if err != nil {
		return se.WithRollback(se.NewServiceError(messages.DatabaseOperationFailed, "Type", "evaluation job", "ResourceId", id, "Error", err.Error()))
	}

	return nil
}

func updateEvaluationJobStatusTxn(evaluationJob *api.EvaluationJobResource, overallState api.OverallState, message *api.MessageInfo, e sql.SQLExecutor) error {

	status := evaluationJob.Status
	status.State = overallState
	status.Message = message

	entity := sql.EvaluationJobEntity{
		Config:  &evaluationJob.EvaluationJobConfig,
		Status:  status,
		Results: evaluationJob.Results,
	}

	return updateEvaluationJobTxn(evaluationJob.Resource.ID, overallState, &entity, e)
}
