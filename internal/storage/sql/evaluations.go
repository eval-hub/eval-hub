package sql

import (
	"database/sql"
	"encoding/json"
	"time"

	// import the postgres driver - "pgx"

	_ "github.com/jackc/pgx/v5/stdlib"

	// import the sqlite driver - "sqlite"
	_ "modernc.org/sqlite"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/messages"
	se "github.com/eval-hub/eval-hub/internal/serviceerrors"
	commonStorage "github.com/eval-hub/eval-hub/internal/storage/common"
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
func (s *SQLStorage) CreateEvaluationJob(evaluation *api.EvaluationJobResource) error {
	jobID := evaluation.Resource.ID
	mlflowExperimentID := evaluation.Resource.MLFlowExperimentID

	err := s.withTransaction("create evaluation job", jobID, func(txn *sql.Tx) error {
		tenant, err := s.getTenant()
		if err != nil {
			return se.WithRollback(err)
		}

		evaluationJSON, err := s.createEvaluationJobEntity(evaluation)
		if err != nil {
			return se.WithRollback(err)
		}
		addEntityStatement, err := createAddEntityStatement(s.sqlConfig.Driver, TABLE_EVALUATIONS)
		if err != nil {
			return se.WithRollback(err)
		}
		s.logger.Info("Creating evaluation job", "id", jobID, "tenant", tenant, "status", api.StatePending, "experiment_id", mlflowExperimentID)
		// (id, tenant_id, status, experiment_id, entity)
		_, err = s.exec(txn, addEntityStatement, jobID, tenant, api.StatePending, mlflowExperimentID, string(evaluationJSON))
		if err != nil {
			return se.WithRollback(err)
		}

		return err
	})
	return err
}

func (s *SQLStorage) createEvaluationJobEntity(evaluation *api.EvaluationJobResource) ([]byte, error) {
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

func (s *SQLStorage) GetEvaluationJob(id string) (*api.EvaluationJobResource, error) {
	return s.getEvaluationJobTransactional(nil, id)
}

func (s *SQLStorage) constructEvaluationResource(statusStr string, message *api.MessageInfo, dbID string, createdAt time.Time, updatedAt time.Time, experimentID string, evaluationEntity *EvaluationJobEntity) (*api.EvaluationJobResource, error) {
	if evaluationEntity == nil {
		s.logger.Error("Failed to construct evaluation job resource", "error", "Evaluation entity does not exist", "id", dbID)
		// Post-read validation: no writes done, so do not request rollback.
		return nil, se.NewServiceError(messages.InternalServerError, "Error", "Evaluation entity does not exist")
	}
	if evaluationEntity.Config == nil {
		s.logger.Error("Failed to construct evaluation job resource", "error", "Evaluation config does not exist", "id", dbID)
		// Post-read validation: no writes done, so do not request rollback.
		return nil, se.NewServiceError(messages.InternalServerError, "Error", "Evaluation config does not exist")
	}
	if evaluationEntity.Status == nil {
		evaluationEntity.Status = &api.EvaluationJobStatus{}
	}

	if message == nil {
		message = evaluationEntity.Status.Message
	}
	overAllState := evaluationEntity.Status.State

	if statusStr != "" {
		if s, err := api.GetOverallState(statusStr); err == nil {
			overAllState = s
		}
	}
	status := evaluationEntity.Status
	status.State = overAllState

	evaluationResource := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{
				ID:        dbID,
				Tenant:    "TODO", // TODO: retrieve tenant from database or context
				CreatedAt: createdAt,
				UpdatedAt: updatedAt,
			},
			MLFlowExperimentID: experimentID,
			Message:            message,
		},
		Status:              status,
		EvaluationJobConfig: *evaluationEntity.Config,
		Results:             evaluationEntity.Results,
	}
	return evaluationResource, nil
}

func (s *SQLStorage) getEvaluationJobTransactional(txn *sql.Tx, id string) (*api.EvaluationJobResource, error) {
	// Build the SELECT query
	selectQuery, err := createGetEntityStatement(s.sqlConfig.Driver, TABLE_EVALUATIONS)
	if err != nil {
		return nil, se.WithRollback(err)
	}

	// Query the database
	var dbID string
	var createdAt, updatedAt time.Time
	var statusStr string
	var experimentID string
	var entityJSON string

	err = s.queryRow(txn, selectQuery, id).Scan(&dbID, &createdAt, &updatedAt, &statusStr, &experimentID, &entityJSON)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, se.NewServiceError(messages.ResourceNotFound, "Type", "evaluation job", "ResourceId", id)
		}
		// For now we differentiate between no rows found and other errors but this might be confusing
		s.logger.Error("Failed to get evaluation job", "error", err, "id", id)
		return nil, se.WithRollback(se.NewServiceError(messages.DatabaseOperationFailed, "Type", "evaluation job", "ResourceId", id, "Error", err.Error()))
	}

	// Unmarshal the entity JSON into EvaluationJobConfig
	var evaluationJobEntity EvaluationJobEntity
	err = json.Unmarshal([]byte(entityJSON), &evaluationJobEntity)
	if err != nil {
		s.logger.Error("Failed to unmarshal evaluation job entity", "error", err, "id", id)
		return nil, se.NewServiceError(messages.JSONUnmarshalFailed, "Type", "evaluation job", "Error", err.Error())
	}

	job, err := s.constructEvaluationResource(statusStr, nil, dbID, createdAt, updatedAt, experimentID, &evaluationJobEntity)
	if err != nil {
		return nil, se.WithRollback(err)
	}
	return job, nil
}

func (s *SQLStorage) GetEvaluationJobs(limit int, offset int, statusFilter string) (*abstractions.QueryResults[api.EvaluationJobResource], error) {
	// Get total count (with status filter if provided)
	countQuery, countArgs, err := createCountEntitiesStatement(s.sqlConfig.Driver, TABLE_EVALUATIONS, statusFilter)
	if err != nil {
		return nil, err
	}

	var totalCount int
	if len(countArgs) > 0 {
		err = s.queryRow(nil, countQuery, countArgs...).Scan(&totalCount)
	} else {
		err = s.queryRow(nil, countQuery).Scan(&totalCount)
	}
	if err != nil {
		s.logger.Error("Failed to count evaluation jobs", "error", err)
		return nil, se.NewServiceError(messages.QueryFailed, "Type", "evaluation jobs", "Error", err.Error())
	}

	// Build the list query with pagination and status filter
	listQuery, listArgs, err := createListEntitiesStatement(s.sqlConfig.Driver, TABLE_EVALUATIONS, limit, offset, statusFilter)
	if err != nil {
		return nil, err
	}

	// Query the database
	rows, err := s.query(nil, listQuery, listArgs...)
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
		var statusStr string
		var experimentID string
		var entityJSON string

		err = rows.Scan(&dbID, &createdAt, &updatedAt, &statusStr, &experimentID, &entityJSON)
		if err != nil {
			s.logger.Error("Failed to scan evaluation job row", "error", err)
			return nil, se.NewServiceError(messages.DatabaseOperationFailed, "Type", "evaluation job", "ResourceId", dbID, "Error", err.Error())
		}

		// Unmarshal the entity JSON into EvaluationJobConfig
		var evaluationJobEntity EvaluationJobEntity
		err = json.Unmarshal([]byte(entityJSON), &evaluationJobEntity)
		if err != nil {
			s.logger.Error("Failed to unmarshal evaluation job entity", "error", err, "id", dbID)
			return nil, se.NewServiceError(messages.JSONUnmarshalFailed, "Type", "evaluation job", "Error", err.Error())
		}

		// Construct the EvaluationJobResource
		resource, err := s.constructEvaluationResource(statusStr, nil, dbID, createdAt, updatedAt, experimentID, &evaluationJobEntity)
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

func (s *SQLStorage) DeleteEvaluationJob(id string) error {
	// we have to get the evaluation job and then update or delete the job so we need a transaction
	err := s.withTransaction("delete evaluation job", id, func(txn *sql.Tx) error {
		// check if the evaluation job exists, we do this otherwise we always return 204
		selectQuery, err := createCheckEntityExistsStatement(s.sqlConfig.Driver, TABLE_EVALUATIONS)
		if err != nil {
			return se.WithRollback(err)
		}
		var dbID string
		var statusStr string
		err = s.queryRow(txn, selectQuery, id).Scan(&dbID, &statusStr)
		if err != nil {
			if err == sql.ErrNoRows {
				return se.NewServiceError(messages.ResourceNotFound, "Type", "evaluation job", "ResourceId", id)
			}
			return se.WithRollback(se.NewServiceError(messages.DatabaseOperationFailed, "Type", "evaluation job", "ResourceId", id, "Error", err.Error()))
		}

		// Build the DELETE query
		deleteQuery, err := createDeleteEntityStatement(s.sqlConfig.Driver, TABLE_EVALUATIONS)
		if err != nil {
			return se.WithRollback(err)
		}

		// Execute the DELETE query
		_, err = s.exec(txn, deleteQuery, id)
		if err != nil {
			s.logger.Error("Failed to delete evaluation job", "error", err, "id", id)
			return se.WithRollback(se.NewServiceError(messages.DatabaseOperationFailed, "Type", "evaluation job", "ResourceId", id, "Error", err.Error()))
		}

		s.logger.Info("Deleted evaluation job", "id", id)

		return nil
	})
	return err
}

func (s *SQLStorage) UpdateEvaluationJobStatus(id string, state api.OverallState, message *api.MessageInfo) error {
	// we have to get the evaluation job and update the status so we need a transaction
	err := s.withTransaction("update evaluation job status", id, func(txn *sql.Tx) error {
		// get the evaluation job
		evaluationJob, err := s.getEvaluationJobTransactional(txn, id)
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
		if err := s.updateEvaluationJobStatusTxn(txn, id, state, message); err != nil {
			return err
		}
		s.logger.Info("Updated evaluation job status", "id", id, "overall_state", state, "message", message)
		return nil
	})
	return err
}

func (s *SQLStorage) updateEvaluationJobStatusTxn(txn *sql.Tx, id string, overallState api.OverallState, message *api.MessageInfo) error {
	evaluationJob, err := s.getEvaluationJobTransactional(txn, id)
	if err != nil {
		return err
	}
	evaluationJob.Status.State = overallState
	evaluationJob.Status.Message = message

	entity := EvaluationJobEntity{
		Config:  &evaluationJob.EvaluationJobConfig,
		Status:  evaluationJob.Status,
		Results: evaluationJob.Results,
	}

	return s.updateEvaluationJobTxn(txn, id, overallState, &entity)
}

func (s *SQLStorage) updateEvaluationJobTxn(txn *sql.Tx, id string, status api.OverallState, evaluationJob *EvaluationJobEntity) error {
	entityJSON, err := json.Marshal(evaluationJob)
	if err != nil {
		// we should never get here
		return se.WithRollback(se.NewServiceError(messages.InternalServerError, "Error", err.Error()))
	}
	updateQuery, args, err := CreateUpdateEvaluationStatement(s.sqlConfig.Driver, TABLE_EVALUATIONS, id, status, string(entityJSON))
	if err != nil {
		return se.WithRollback(err)
	}

	_, err = s.exec(txn, updateQuery, args...)
	if err != nil {
		s.logger.Error("Failed to update evaluation job", "error", err, "id", id, "status", status)
		return se.WithRollback(se.NewServiceError(messages.DatabaseOperationFailed, "Type", "evaluation job", "ResourceId", id, "Error", err.Error()))
	}

	s.logger.Info("Updated evaluation job", "id", id, "status", status)

	return nil
}

// UpdateEvaluationJobWithRunStatus runs in a transaction: fetches the job, merges RunStatusInternal into the entity, and persists.
func (s *SQLStorage) UpdateEvaluationJob(id string, runStatus *api.StatusEvent) error {
	err := s.withTransaction("update evaluation job", id, func(txn *sql.Tx) error {
		job, err := s.getEvaluationJobTransactional(txn, id)
		if err != nil {
			return err
		}

		err = commonStorage.ValidateBenchmarkExists(job, runStatus)
		if err != nil {
			return err
		}

		// first we store the benchmark status
		benchmark := api.BenchmarkStatus{
			ProviderID:   runStatus.BenchmarkStatusEvent.ProviderID,
			ID:           runStatus.BenchmarkStatusEvent.ID,
			Status:       runStatus.BenchmarkStatusEvent.Status,
			ErrorMessage: runStatus.BenchmarkStatusEvent.ErrorMessage,
			StartedAt:    runStatus.BenchmarkStatusEvent.StartedAt,
			CompletedAt:  runStatus.BenchmarkStatusEvent.CompletedAt,
		}
		commonStorage.UpdateBenchmarkStatus(job, runStatus, &benchmark)

		// if the run status is completed, failed, or cancelled, we need to update the results
		if runStatus.BenchmarkStatusEvent.Status == api.StateCompleted || runStatus.BenchmarkStatusEvent.Status == api.StateFailed || runStatus.BenchmarkStatusEvent.Status == api.StateCancelled {
			result := api.BenchmarkResult{
				ID:          runStatus.BenchmarkStatusEvent.ID,
				ProviderID:  runStatus.BenchmarkStatusEvent.ProviderID,
				Metrics:     runStatus.BenchmarkStatusEvent.Metrics,
				Artifacts:   runStatus.BenchmarkStatusEvent.Artifacts,
				MLFlowRunID: runStatus.BenchmarkStatusEvent.MLFlowRunID,
				LogsPath:    runStatus.BenchmarkStatusEvent.LogsPath,
			}
			err := commonStorage.UpdateBenchmarkResults(job, runStatus, &result)
			if err != nil {
				return err
			}
		}

		// get the overall job status
		overallState, message := commonStorage.GetOverallJobStatus(job)
		job.Status.State = overallState
		job.Status.Message = message

		entity := EvaluationJobEntity{
			Config:  &job.EvaluationJobConfig,
			Status:  job.Status,
			Results: job.Results,
		}

		return s.updateEvaluationJobTxn(txn, id, overallState, &entity)
	})

	return err
}
