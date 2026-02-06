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
	"github.com/eval-hub/eval-hub/internal/constants"
	"github.com/eval-hub/eval-hub/internal/messages"
	"github.com/eval-hub/eval-hub/internal/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
)

type EvaluationJobEntity struct {
	Config  *api.EvaluationJobConfig  `json:"config"`
	Status  *api.EvaluationJobStatus  `json:"status"`
	Results *api.EvaluationJobResults `json:"results,omitempty"`
}

//#######################################################################
// Evaluation job operations
//#######################################################################

// CreateEvaluationJob creates a new evaluation job in the database
// the evaluation job is stored in the evaluations table as a JSON string
// the evaluation job is returned as a EvaluationJobResource
// This should use transactions etc and requires cleaning up
func (s *SQLStorage) CreateEvaluationJob(evaluation *api.EvaluationJobConfig, mlflowExperimentID string) (*api.EvaluationJobResource, error) {
	tenant, err := s.getTenant()
	if err != nil {
		return nil, err
	}

	evaluationEntity := &EvaluationJobEntity{
		Config: evaluation,
		Status: &api.EvaluationJobStatus{
			EvaluationJobState: api.EvaluationJobState{
				State: api.StatePending,
				Message: &api.MessageInfo{
					Message:     "Evaluation job created",
					MessageCode: constants.MESSAGE_CODE_EVALUATION_JOB_CREATED,
				},
			},
		},
	}
	evaluationJSON, err := json.Marshal(evaluationEntity)
	if err != nil {
		return nil, err
	}
	addEntityStatement, err := createAddEntityStatement(s.sqlConfig.Driver, TABLE_EVALUATIONS)
	if err != nil {
		return nil, err
	}
	jobID := s.generateID()
	s.logger.Info("Creating evaluation job", "id", jobID, "tenant", tenant, "status", api.StatePending, "experiment_id", mlflowExperimentID)
	// (id, tenant_id, status, experiment_id, entity)
	_, err = s.exec(nil, addEntityStatement, jobID, tenant, api.StatePending, mlflowExperimentID, string(evaluationJSON))
	if err != nil {
		return nil, err
	}
	evaluationResource := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{
				ID:        jobID,
				Tenant:    api.Tenant(tenant),
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
			MLFlowExperimentID: mlflowExperimentID,
		},
		EvaluationJobConfig: *evaluation,
		Status:              evaluationEntity.Status,
		Results:             nil,
	}
	return evaluationResource, nil
}

func (s *SQLStorage) GetEvaluationJob(id string) (*api.EvaluationJobResource, error) {
	// Build the SELECT query
	selectQuery, err := createGetEntityStatement(s.sqlConfig.Driver, TABLE_EVALUATIONS)
	if err != nil {
		return nil, err
	}

	// Query the database
	var dbID string
	var createdAt, updatedAt time.Time
	var statusStr string
	var experimentID string
	var entityJSON string

	err = s.pool.QueryRowContext(s.ctx, selectQuery, id).Scan(&dbID, &createdAt, &updatedAt, &statusStr, &experimentID, &entityJSON)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, serviceerrors.NewServiceError(messages.ResourceNotFound, "Type", "evaluation job", "ResourceId", id)
		}
		// For now we differentiate between no rows found and other errors but this might be confusing
		s.logger.Error("Failed to get evaluation job", "error", err, "id", id)
		return nil, serviceerrors.NewServiceError(messages.DatabaseOperationFailed, "Type", "evaluation job", "ResourceId", id, "Error", err.Error())
	}

	// Unmarshal the entity JSON into EvaluationJobConfig
	var evaluationEntity EvaluationJobEntity
	err = json.Unmarshal([]byte(entityJSON), &evaluationEntity)
	if err != nil {
		s.logger.Error("Failed to unmarshal evaluation job entity", "error", err, "id", id)
		return nil, serviceerrors.NewServiceError(messages.JSONUnmarshalFailed, "Type", "evaluation job", "Error", err.Error())
	}

	evaluationResource := constructEvaluationResource(statusStr, dbID, createdAt, updatedAt, experimentID, evaluationEntity)

	return evaluationResource, nil
}

func constructEvaluationResource(statusStr string, dbID string, createdAt time.Time, updatedAt time.Time, experimentID string, evaluationEntity EvaluationJobEntity) *api.EvaluationJobResource {
	evaluationResource := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{
				ID:        dbID,
				Tenant:    "TODO", // TODO: retrieve tenant from database or context
				CreatedAt: createdAt,
				UpdatedAt: updatedAt,
			},
			MLFlowExperimentID: experimentID,
		},
		EvaluationJobConfig: *evaluationEntity.Config,
		Status:              evaluationEntity.Status,
		Results:             evaluationEntity.Results,
	}
	return evaluationResource
}

func (s *SQLStorage) getEvaluationJobTransactional(txn *sql.Tx, id string) (*api.EvaluationJobResource, error) {
	// Build the SELECT query
	selectQuery, err := createGetEntityStatement(s.sqlConfig.Driver, TABLE_EVALUATIONS)
	if err != nil {
		return nil, err
	}

	// Query the database
	var dbID string
	var createdAt, updatedAt time.Time
	var statusStr string
	var experimentID string
	var entityJSON string

	err = txn.QueryRowContext(s.ctx, selectQuery, id).Scan(&dbID, &createdAt, &updatedAt, &statusStr, &experimentID, &entityJSON)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, serviceerrors.NewServiceError(messages.ResourceNotFound, "Type", "evaluation job", "ResourceId", id)
		}
		// For now we differentiate between no rows found and other errors but this might be confusing
		s.logger.Error("Failed to get evaluation job", "error", err, "id", id)
		return nil, serviceerrors.NewServiceError(messages.DatabaseOperationFailed, "Type", "evaluation job", "ResourceId", id, "Error", err.Error())
	}

	// Unmarshal the entity JSON into EvaluationJobConfig
	var evaluationEntity EvaluationJobEntity
	err = json.Unmarshal([]byte(entityJSON), &evaluationEntity)
	if err != nil {
		s.logger.Error("Failed to unmarshal evaluation job entity", "error", err, "id", id)
		return nil, serviceerrors.NewServiceError(messages.JSONUnmarshalFailed, "Type", "evaluation job", "Error", err.Error())
	}

	evaluationResource := constructEvaluationResource(statusStr, dbID, createdAt, updatedAt, experimentID, evaluationEntity)

	return evaluationResource, nil
}

func (s *SQLStorage) GetEvaluationJobs(limit int, offset int, statusFilter string) (*abstractions.QueryResults[api.EvaluationJobResource], error) {
	// Get total count (with status filter if provided)
	countQuery, countArgs, err := createCountEntitiesStatement(s.sqlConfig.Driver, TABLE_EVALUATIONS, statusFilter)
	if err != nil {
		return nil, err
	}

	var totalCount int
	if len(countArgs) > 0 {
		err = s.pool.QueryRowContext(s.ctx, countQuery, countArgs...).Scan(&totalCount)
	} else {
		err = s.pool.QueryRowContext(s.ctx, countQuery).Scan(&totalCount)
	}
	if err != nil {
		s.logger.Error("Failed to count evaluation jobs", "error", err)
		return nil, serviceerrors.NewServiceError(messages.QueryFailed, "Type", "evaluation jobs", "Error", err.Error())
	}

	// Build the list query with pagination and status filter
	listQuery, listArgs, err := createListEntitiesStatement(s.sqlConfig.Driver, TABLE_EVALUATIONS, limit, offset, statusFilter)
	if err != nil {
		return nil, err
	}

	// Query the database
	rows, err := s.pool.QueryContext(s.ctx, listQuery, listArgs...)
	if err != nil {
		s.logger.Error("Failed to list evaluation jobs", "error", err)
		return nil, serviceerrors.NewServiceError(messages.QueryFailed, "Type", "evaluation jobs", "Error", err.Error())
	}
	defer rows.Close()

	// Process rows
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
			return nil, serviceerrors.NewServiceError(messages.DatabaseOperationFailed, "Type", "evaluation job", "ResourceId", dbID, "Error", err.Error())
		}

		// Unmarshal the entity JSON into EvaluationJobConfig
		var evaluationJobEntity EvaluationJobEntity
		err = json.Unmarshal([]byte(entityJSON), &evaluationJobEntity)
		if err != nil {
			s.logger.Error("Failed to unmarshal evaluation job entity", "error", err, "id", dbID)
			return nil, serviceerrors.NewServiceError(messages.JSONUnmarshalFailed, "Type", "evaluation job", "Error", err.Error())
		}

		// Construct the EvaluationJobResource
		// Note: Results and Benchmarks are initialized with defaults since they're not stored in the entity column
		resource := api.EvaluationJobResource{
			Resource: api.EvaluationResource{
				Resource: api.Resource{
					ID:        dbID,
					Tenant:    "TODO", // TODO: 	retrieve tenant from database or context
					CreatedAt: createdAt,
					UpdatedAt: updatedAt,
				},
				MLFlowExperimentID: experimentID,
			},
			EvaluationJobConfig: *evaluationJobEntity.Config,
			Status:              evaluationJobEntity.Status,
			Results:             evaluationJobEntity.Results,
		}

		items = append(items, resource)
	}

	if err = rows.Err(); err != nil {
		s.logger.Error("Error iterating evaluation job rows", "error", err)
		return nil, serviceerrors.NewServiceError(messages.QueryFailed, "Type", "evaluation jobs", "Error", err.Error())
	}

	return &abstractions.QueryResults[api.EvaluationJobResource]{
		Items:       items,
		TotalStored: totalCount,
	}, nil
}

func (s *SQLStorage) DeleteEvaluationJob(id string, hardDelete bool) error {
	if !hardDelete {
		statusEvent := &api.StatusEvent{
			StatusEvent: &api.EvaluationJobStatus{
				EvaluationJobState: api.EvaluationJobState{
					State: api.StateCancelled,
					Message: &api.MessageInfo{
						Message:     "Evaluation job cancelled",
						MessageCode: constants.MESSAGE_CODE_EVALUATION_JOB_CANCELLED,
					},
				},
			},
		}
		return s.UpdateEvaluationJobStatus(id, statusEvent)
	}

	// Build the DELETE query
	deleteQuery, err := createDeleteEntityStatement(s.sqlConfig.Driver, TABLE_EVALUATIONS)
	if err != nil {
		return err
	}

	// Execute the DELETE query
	_, err = s.exec(nil, deleteQuery, id)
	if err != nil {
		s.logger.Error("Failed to delete evaluation job", "error", err, "id", id)
		return serviceerrors.NewServiceError(messages.DatabaseOperationFailed, "Type", "evaluation job", "ResourceId", id, "Error", err.Error())
	}

	/* TODO: remove this code? For now we don't do this because not all drivers support RowsAffected()
	// Check if any rows were affected
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		ctx.Logger.Error("Failed to get rows affected", "error", err, "id", id)
		return NewStorageError(err, "failed to get rows affected")
	}

	if rowsAffected == 0 {
		return NewStorageError("evaluation job with ID %s not found", id)
	}
	*/

	s.logger.Info("Deleted evaluation job", "id", id, "hardDelete", hardDelete)
	return nil
}

func (s *SQLStorage) UpdateEvaluationJobStatus(id string, status *api.StatusEvent) error {
	// Build the UPDATE query
	updateQuery, err := createUpdateStatusStatement(s.sqlConfig.Driver, TABLE_EVALUATIONS)
	if err != nil {
		return err
	}

	// TODO: For now this only handles the status update

	// Execute the UPDATE query
	statusStr := string(status.StatusEvent.EvaluationJobState.State)
	_, err = s.exec(nil, updateQuery, statusStr, id)
	if err != nil {
		s.logger.Error("Failed to update evaluation job status", "error", err, "id", id, "status", statusStr)
		return serviceerrors.NewServiceError(messages.DatabaseOperationFailed, "Type", "evaluation job", "ResourceId", id, "Error", err.Error())
	}

	/* TODO: remove this code? For now we don't do this because not all drivers support RowsAffected()
	// Check if any rows were affected
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		ctx.Logger.Error("Failed to get rows affected", "error", err, "id", id)
		return NewStorageError(err, "failed to get rows affected")
	}

	if rowsAffected == 0 {
		return NewStorageError("evaluation job with ID %s not found", id)
	}
	*/

	s.logger.Info("Updated evaluation job status", "id", id, "status", statusStr)
	return nil
}

func (s *SQLStorage) updateEvaluationJobTransactional(txn *sql.Tx, id string, status *api.EvaluationJobStatus, entityJSON string) error {
	statusStr := string(status.EvaluationJobState.State)
	updateQuery, args, err := CreateUpdateEvaluationStatement(s.sqlConfig.Driver, TABLE_EVALUATIONS, id, statusStr, entityJSON)
	if err != nil {
		return err
	}

	_, err = s.exec(txn, updateQuery, args...)
	if err != nil {
		s.logger.Error("Failed to update evaluation job", "error", err, "id", id, "status", statusStr)
		return serviceerrors.NewServiceError(messages.DatabaseOperationFailed, "Type", "evaluation job", "ResourceId", id, "Error", err.Error())
	}

	s.logger.Info("Updated evaluation job", "id", id, "status", statusStr)
	return nil
}

// UpdateEvaluationJobWithRunStatus runs in a transaction: fetches the job, merges RunStatusInternal into the entity, and persists.
func (s *SQLStorage) UpdateEvaluationJob(id string, runStatus *api.RunStatusInternal) error {
	txn, err := s.pool.BeginTx(s.ctx, nil)
	if err != nil {
		s.logger.Error("Failed to begin transaction", "error", err, "id", id)
		return serviceerrors.NewServiceError(messages.DatabaseOperationFailed, "Type", "evaluation job", "ResourceId", id, "Error", err.Error())
	}
	defer func() { _ = txn.Rollback() }()

	job, err := s.getEvaluationJobTransactional(txn, id)
	if err != nil {
		return err
	}

	err = validateBenchmarkExists(job, runStatus)
	if err != nil {
		return err
	}

	updateBenchMarkProgress(job, runStatus)

	updateOverallJobStatus(job)

	updatedEntityJSON, err := json.Marshal(&EvaluationJobEntity{
		Config:  &job.EvaluationJobConfig,
		Status:  job.Status,
		Results: job.Results,
	})
	if err != nil {
		s.logger.Error("Failed to marshal updated job resource", "error", err, "id", id)
		return serviceerrors.NewServiceError(messages.DatabaseOperationFailed, "Type", "evaluation job", "ResourceId", id, "Error", err.Error())
	}
	if err := s.updateEvaluationJobTransactional(txn, id, job.Status, string(updatedEntityJSON)); err != nil {
		return err
	}

	if err := txn.Commit(); err != nil {
		s.logger.Error("Failed to commit transaction", "error", err, "id", id)
		return serviceerrors.NewServiceError(messages.DatabaseOperationFailed, "Type", "evaluation job", "ResourceId", id, "Error", err.Error())
	}
	return nil
}

func validateBenchmarkExists(job *api.EvaluationJobResource, runStatus *api.RunStatusInternal) error {
	found := false
	for _, benchmark := range job.Benchmarks {
		if benchmark.ID == runStatus.StatusEvent.BenchmarkID {
			found = true
			break
		}
	}
	if !found {
		return serviceerrors.NewServiceError(messages.ResourceNotFound, "Type", "benchmark", "ResourceId", runStatus.StatusEvent.BenchmarkID, "Error", "Invalid Benchmark for the evaluation job")
	}
	return nil
}

func updateOverallJobStatus(job *api.EvaluationJobResource) {
	// group all benchmarks by state
	benchmarkStates := make(map[api.State]int)
	failureMessage := ""
	for _, benchmark := range job.Status.Benchmarks {
		benchmarkStates[benchmark.State]++
		if benchmark.State == api.StateFailed && benchmark.Message != nil {
			failureMessage += "Benchmark " + benchmark.ID + " failed with message: " + benchmark.Message.Message + "\n"
		}
	}

	// determine the overall job status
	total := len(job.Benchmarks)
	completed, failed, running := benchmarkStates[api.StateCompleted], benchmarkStates[api.StateFailed], benchmarkStates[api.StateRunning]

	var overallState api.State
	var stateMessage string
	switch {
	case completed == total:
		overallState, stateMessage = api.StateCompleted, "Evaluation job is completed"
	case failed == total:
		overallState, stateMessage = api.StateFailed, "Evaluation job is failed. \n"+failureMessage
	case completed+failed == total:
		overallState, stateMessage = api.StatePartiallyFailed, "Some of the benchmarks failed. \n"+failureMessage
	case running > 0:
		overallState, stateMessage = api.StateRunning, "Evaluation job is running"
	default:
		overallState, stateMessage = api.StatePending, "Evaluation job is pending"
	}

	newStatus := overallState
	statusUpdate := &api.EvaluationJobStatus{
		EvaluationJobState: api.EvaluationJobState{
			State: newStatus,
			Message: &api.MessageInfo{
				Message: stateMessage,
			},
		},
		Benchmarks: job.Status.Benchmarks,
	}

	job.Status = statusUpdate
}

func updateBenchMarkProgress(jobResource *api.EvaluationJobResource, runStatus *api.RunStatusInternal) {
	jobResource.Status.Benchmarks = findAndUpdateBenchmarkStatus(jobResource.Status.Benchmarks, runStatus)
	if jobResource.Results == nil {
		jobResource.Results = &api.EvaluationJobResults{}
	}
	findAndUpdateBenchmarkResults(jobResource.Results, runStatus)
}

func findAndUpdateBenchmarkStatus(benchmarkStatus []api.BenchmarkStatus, runStatus *api.RunStatusInternal) []api.BenchmarkStatus {
	found := false
	for i := range benchmarkStatus {
		status := &benchmarkStatus[i]
		if status.ID == runStatus.StatusEvent.BenchmarkID {
			prevState := status.State
			status.State = runStatus.StatusEvent.Status
			if runStatus.StatusEvent.Artifacts != nil {
				if logsPath, ok := runStatus.StatusEvent.Artifacts["logs"].(string); ok && logsPath != "" {
					status.Logs = &api.BenchmarkStatusLogs{Path: logsPath}
				}
			}
			if prevState == api.StatePending && runStatus.StatusEvent.Status == api.StateRunning {
				status.StartedAt = runStatus.StatusEvent.StartedAt
			}
			if runStatus.StatusEvent.Status == api.StateCompleted {
				status.CompletedAt = runStatus.StatusEvent.CompletedAt
			}
			if runStatus.StatusEvent.Status == api.StateFailed && runStatus.StatusEvent.ErrorMessage != nil {
				status.Message = &api.MessageInfo{
					Message:     runStatus.StatusEvent.ErrorMessage.Message,
					MessageCode: runStatus.StatusEvent.ErrorMessage.MessageCode,
				}
			}
			found = true
			break
		}
	}
	if !found {
		// if the benchmark is not found, create a new benchmark status
		newBenchmarkStatus := api.BenchmarkStatus{
			ID:    runStatus.StatusEvent.BenchmarkID,
			State: runStatus.StatusEvent.Status,
		}
		if runStatus.StatusEvent.Artifacts != nil {
			if logsPath, ok := runStatus.StatusEvent.Artifacts["logs"].(string); ok && logsPath != "" {
				newBenchmarkStatus.Logs = &api.BenchmarkStatusLogs{Path: logsPath}
			}
		}
		if runStatus.StatusEvent.Status == api.StateRunning {
			newBenchmarkStatus.StartedAt = runStatus.StatusEvent.StartedAt
		}
		if runStatus.StatusEvent.Status == api.StateCompleted || runStatus.StatusEvent.Status == api.StateFailed {
			newBenchmarkStatus.CompletedAt = runStatus.StatusEvent.CompletedAt
		}
		if runStatus.StatusEvent.Status == api.StateFailed && runStatus.StatusEvent.ErrorMessage != nil {
			newBenchmarkStatus.Message = &api.MessageInfo{
				Message:     runStatus.StatusEvent.ErrorMessage.Message,
				MessageCode: runStatus.StatusEvent.ErrorMessage.MessageCode,
			}
		}
		benchmarkStatus = append(benchmarkStatus, newBenchmarkStatus)
	}
	return benchmarkStatus
}

func findAndUpdateBenchmarkResults(benchmarkResults *api.EvaluationJobResults, runStatus *api.RunStatusInternal) {
	if benchmarkResults == nil {
		return
	}
	found := false
	for i := range benchmarkResults.Benchmarks {
		result := &benchmarkResults.Benchmarks[i]
		if result.ID == runStatus.StatusEvent.BenchmarkID {
			if runStatus.StatusEvent.Status == api.StateCompleted {
				result.Metrics = runStatus.StatusEvent.Metrics
				result.Artifacts = runStatus.StatusEvent.Artifacts
			}
			found = true
			break
		}
	}
	if !found {
		if runStatus.StatusEvent.Status == api.StateCompleted {
			newBenchmarkResult := api.EvaluationJobBenchmarkResult{
				ID:        runStatus.StatusEvent.BenchmarkID,
				State:     runStatus.StatusEvent.Status,
				Metrics:   runStatus.StatusEvent.Metrics,
				Artifacts: runStatus.StatusEvent.Artifacts,
			}
			benchmarkResults.Benchmarks = append(benchmarkResults.Benchmarks, newBenchmarkResult)
		}

	}
}
