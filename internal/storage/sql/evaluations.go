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
	Config  *api.EvaluationJobConfig  `json:"config" validate:"required"`
	Status  *api.EvaluationJobStatus  `json:"status,omitempty"`
	Results *api.EvaluationJobResults `json:"results,omitempty"`
}

//#######################################################################
// Evaluation job operations
//#######################################################################

// CreateEvaluationJob creates a new evaluation job in the database
// the evaluation job is stored in the evaluations table as a JSON string
// the evaluation job is returned as a EvaluationJobResource
func (s *SQLStorage) CreateEvaluationJob(evaluation *api.EvaluationJobConfig, mlflowExperimentID string, mlflowExperimentURL string) (*api.EvaluationJobResource, error) {
	// we have to get the evaluation job and update the status so we need a transaction
	txn, err := s.pool.BeginTx(s.ctx, nil)
	if err != nil {
		s.logger.Error("Failed to begin create evaluation job transaction", "error", err)
		return nil, serviceerrors.NewServiceError(messages.DatabaseOperationFailed, "Type", "evaluation job", "ResourceId", "<creation>", "Error", err.Error())
	}
	jobID := s.generateID()
	committed := false
	defer func() {
		if !committed {
			err := txn.Rollback()
			if err != nil {
				s.logger.Error("Failed to rollback create evaluation job transaction", "error", err, "id", jobID)
			}
		}
	}()

	tenant, err := s.getTenant()
	if err != nil {
		return nil, err
	}

	status := &api.EvaluationJobStatus{
		EvaluationJobState: api.EvaluationJobState{
			State: api.OverallStatePending,
			Message: &api.MessageInfo{
				Message:     "Evaluation job created",
				MessageCode: constants.MESSAGE_CODE_EVALUATION_JOB_CREATED,
			},
		},
	}
	results := &api.EvaluationJobResults{
		MLFlowExperimentURL: mlflowExperimentURL,
	}

	evaluationJSON, err := s.createEvaluationJobEntity(evaluation, status, results)
	if err != nil {
		return nil, err
	}
	addEntityStatement, err := createAddEntityStatement(s.sqlConfig.Driver, TABLE_EVALUATIONS)
	if err != nil {
		return nil, err
	}
	s.logger.Info("Creating evaluation job", "id", jobID, "tenant", tenant, "status", api.StatePending, "experiment_id", mlflowExperimentID)
	// (id, tenant_id, status, experiment_id, entity)
	_, err = s.exec(txn, addEntityStatement, jobID, tenant, api.StatePending, mlflowExperimentID, string(evaluationJSON))
	if err != nil {
		return nil, err
	}

	// now get the evaluation job so that we don't have to recreate it by hand
	evaluationJob, err := s.getEvaluationJobTransactional(txn, jobID)
	if err != nil {
		return nil, err
	}

	if err := txn.Commit(); err != nil {
		s.logger.Error("Failed to commit create evaluation transaction", "error", err, "id", jobID)
		return nil, serviceerrors.NewServiceError(messages.DatabaseOperationFailed, "Type", "evaluation job", "ResourceId", jobID, "Error", err.Error())
	}
	committed = true

	return evaluationJob, nil
}

func (s *SQLStorage) createEvaluationJobEntity(evaluation *api.EvaluationJobConfig, status *api.EvaluationJobStatus, results *api.EvaluationJobResults) ([]byte, error) {
	evaluationEntity := &EvaluationJobEntity{
		Config:  evaluation,
		Status:  status,
		Results: results,
	}
	evaluationJSON, err := json.Marshal(evaluationEntity)
	if err != nil {
		return nil, serviceerrors.NewServiceError(messages.InternalServerError, "Error", err.Error())
	}
	return evaluationJSON, nil
}

func (s *SQLStorage) GetEvaluationJob(id string) (*api.EvaluationJobResource, error) {
	return s.getEvaluationJobTransactional(nil, id)
}

func (s *SQLStorage) constructEvaluationResource(statusStr string, message *api.MessageInfo, dbID string, createdAt time.Time, updatedAt time.Time, experimentID string, evaluationEntity *EvaluationJobEntity) (*api.EvaluationJobResource, error) {
	if evaluationEntity == nil {
		s.logger.Error("Failed to construct evaluation job resource", "error", "Evaluation entity does not exist", "id", dbID)
		return nil, serviceerrors.NewServiceError(messages.InternalServerError, "Error", "Evaluation entity does not exist")
	}
	if evaluationEntity.Config == nil {
		s.logger.Error("Failed to construct evaluation job resource", "error", "Evaluation config does not exist", "id", dbID)
		return nil, serviceerrors.NewServiceError(messages.InternalServerError, "Error", "Evaluation config does not exist")
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
		return nil, err
	}

	// Query the database
	var dbID string
	var createdAt, updatedAt time.Time
	var statusStr string
	var experimentID string
	var entityJSON string

	if txn != nil {
		err = txn.QueryRowContext(s.ctx, selectQuery, id).Scan(&dbID, &createdAt, &updatedAt, &statusStr, &experimentID, &entityJSON)
	} else {
		err = s.pool.QueryRowContext(s.ctx, selectQuery, id).Scan(&dbID, &createdAt, &updatedAt, &statusStr, &experimentID, &entityJSON)
	}
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, serviceerrors.NewServiceError(messages.ResourceNotFound, "Type", "evaluation job", "ResourceId", id)
		}
		// For now we differentiate between no rows found and other errors but this might be confusing
		s.logger.Error("Failed to get evaluation job", "error", err, "id", id)
		return nil, serviceerrors.NewServiceError(messages.DatabaseOperationFailed, "Type", "evaluation job", "ResourceId", id, "Error", err.Error())
	}

	// Unmarshal the entity JSON into EvaluationJobConfig
	var evaluationJobEntity EvaluationJobEntity
	err = json.Unmarshal([]byte(entityJSON), &evaluationJobEntity)
	if err != nil {
		s.logger.Error("Failed to unmarshal evaluation job entity", "error", err, "id", id)
		return nil, serviceerrors.NewServiceError(messages.JSONUnmarshalFailed, "Type", "evaluation job", "Error", err.Error())
	}

	return s.constructEvaluationResource(statusStr, nil, dbID, createdAt, updatedAt, experimentID, &evaluationJobEntity)
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
		resource, err := s.constructEvaluationResource(statusStr, nil, dbID, createdAt, updatedAt, experimentID, &evaluationJobEntity)
		if err != nil {
			s.logger.Error("Failed to construct evaluation job resource", "error", err, "id", dbID)
			return nil, serviceerrors.NewServiceError(messages.InternalServerError, "Error", err.Error())
		}

		items = append(items, *resource)
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
		return s.UpdateEvaluationJobStatus(id, api.OverallStateCancelled, &api.MessageInfo{
			Message:     "Evaluation job cancelled",
			MessageCode: constants.MESSAGE_CODE_EVALUATION_JOB_CANCELLED,
		})
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

	s.logger.Info("Deleted evaluation job", "id", id, "hardDelete", hardDelete)
	return nil
}

func (s *SQLStorage) UpdateEvaluationJobStatus(id string, state api.OverallState, message *api.MessageInfo) error {
	// we have to get the evaluation job and update the status so we need a transaction
	txn, err := s.pool.BeginTx(s.ctx, nil)
	if err != nil {
		s.logger.Error("Failed to begin update evaluation job status transaction", "error", err, "id", id)
		return serviceerrors.NewServiceError(messages.DatabaseOperationFailed, "Type", "evaluation job", "ResourceId", id, "Error", err.Error())
	}
	committed := false
	defer func() {
		if !committed {
			err := txn.Rollback()
			if err != nil {
				s.logger.Error("Failed to rollback update evaluation job status transaction", "error", err, "id", id)
			}
		}
	}()

	evaluationJob, err := s.getEvaluationJobTransactional(txn, id)
	if err != nil {
		return err
	}
	evaluationJob.Status.State = state
	evaluationJob.Status.Message = message

	entity := EvaluationJobEntity{
		Config:  &evaluationJob.EvaluationJobConfig,
		Status:  evaluationJob.Status,
		Results: evaluationJob.Results,
	}

	err = s.updateEvaluationJobTransactional(txn, id, state, &entity)
	if err != nil {
		return err
	}

	if err := txn.Commit(); err != nil {
		s.logger.Error("Failed to commit update evaluation job status transaction", "error", err, "id", id)
		return serviceerrors.NewServiceError(messages.DatabaseOperationFailed, "Type", "evaluation job", "ResourceId", id, "Error", err.Error())
	}
	committed = true

	s.logger.Info("Committed update evaluation job status transaction", "id", id)
	return nil
}

func (s *SQLStorage) updateEvaluationJobTransactional(txn *sql.Tx, id string, status api.OverallState, evaluationJob *EvaluationJobEntity) error {
	entityJSON, err := json.Marshal(evaluationJob)
	if err != nil {
		// we should never get here
		return serviceerrors.NewServiceError(messages.InternalServerError, "Error", err.Error())
	}
	updateQuery, args, err := CreateUpdateEvaluationStatement(s.sqlConfig.Driver, TABLE_EVALUATIONS, id, status, string(entityJSON))
	if err != nil {
		return err
	}

	_, err = s.exec(txn, updateQuery, args...)
	if err != nil {
		s.logger.Error("Failed to update evaluation job", "error", err, "id", id, "status", status)
		return serviceerrors.NewServiceError(messages.DatabaseOperationFailed, "Type", "evaluation job", "ResourceId", id, "Error", err.Error())
	}

	s.logger.Info("Updated evaluation job", "id", id, "status", status)
	return nil
}

func (s *SQLStorage) updateBenchmarkStatus(job *api.EvaluationJobResource, runStatus *api.StatusEvent, benchmarkStatus *api.BenchmarkStatus) {
	if job.Status == nil {
		job.Status = &api.EvaluationJobStatus{
			EvaluationJobState: api.EvaluationJobState{
				State: api.OverallStatePending,
			},
		}
	}
	if job.Status.Benchmarks == nil {
		job.Status.Benchmarks = make([]api.BenchmarkStatus, 0)
	}
	for index, benchmark := range job.Status.Benchmarks {
		if benchmark.ID == runStatus.BenchmarkStatusEvent.ID {
			job.Status.Benchmarks[index] = *benchmarkStatus
			return
		}
	}
	job.Status.Benchmarks = append(job.Status.Benchmarks, *benchmarkStatus)
}

func (s *SQLStorage) updateBenchmarkResults(job *api.EvaluationJobResource, runStatus *api.StatusEvent, result *api.BenchmarkResult) {
	if job.Results == nil {
		job.Results = &api.EvaluationJobResults{}
	}
	if job.Results.Benchmarks == nil {
		job.Results.Benchmarks = make([]api.BenchmarkResult, 0)
	}

	// update the number of evaluations
	job.Results.TotalEvaluations++
	switch runStatus.BenchmarkStatusEvent.Status {
	case api.StateCompleted:
		job.Results.CompletedEvaluations++
	case api.StateFailed:
		job.Results.FailedEvaluations++
	case api.StateCancelled:
		// cancelled is amrked as failed for now
		job.Results.FailedEvaluations++
	}

	for index, benchmark := range job.Results.Benchmarks {
		if benchmark.ID == runStatus.BenchmarkStatusEvent.ID {
			job.Results.Benchmarks[index] = *result
			return
		}
	}
	job.Results.Benchmarks = append(job.Results.Benchmarks, *result)
}

// UpdateEvaluationJobWithRunStatus runs in a transaction: fetches the job, merges RunStatusInternal into the entity, and persists.
func (s *SQLStorage) UpdateEvaluationJob(id string, runStatus *api.StatusEvent) error {
	txn, err := s.pool.BeginTx(s.ctx, nil)
	if err != nil {
		s.logger.Error("Failed to begin update evaluation job transaction", "error", err, "id", id)
		return serviceerrors.NewServiceError(messages.DatabaseOperationFailed, "Type", "evaluation job", "ResourceId", id, "Error", err.Error())
	}
	committed := false
	defer func() {
		if !committed {
			err := txn.Rollback()
			if err != nil {
				s.logger.Error("Failed to rollback update evaluation job transaction", "error", err, "id", id)
			}
		}
	}()

	job, err := s.getEvaluationJobTransactional(txn, id)
	if err != nil {
		return err
	}

	err = validateBenchmarkExists(job, runStatus)
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
	s.updateBenchmarkStatus(job, runStatus, &benchmark)

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
		s.updateBenchmarkResults(job, runStatus, &result)
	}

	// get the overall job status
	overallState, message := getOverallJobStatus(job)
	job.Status.State = overallState
	job.Status.Message = message

	entity := EvaluationJobEntity{
		Config:  &job.EvaluationJobConfig,
		Status:  job.Status,
		Results: job.Results,
	}

	if err := s.updateEvaluationJobTransactional(txn, id, overallState, &entity); err != nil {
		return err
	}

	if err := txn.Commit(); err != nil {
		s.logger.Error("Failed to commit update evaluation job transaction", "error", err, "id", id)
		return serviceerrors.NewServiceError(messages.DatabaseOperationFailed, "Type", "evaluation job", "ResourceId", id, "Error", err.Error())
	}
	committed = true
	return nil
}

func validateBenchmarkExists(job *api.EvaluationJobResource, runStatus *api.StatusEvent) error {
	found := false
	for _, benchmark := range job.Benchmarks {
		if benchmark.ID == runStatus.BenchmarkStatusEvent.ID {
			found = true
			break
		}
	}
	if !found {
		return serviceerrors.NewServiceError(messages.ResourceNotFound, "Type", "benchmark", "ResourceId", runStatus.BenchmarkStatusEvent.ID, "Error", "Invalid Benchmark for the evaluation job")
	}
	return nil
}

func getOverallJobStatus(job *api.EvaluationJobResource) (api.OverallState, *api.MessageInfo) {
	// group all benchmarks by state
	benchmarkStates := make(map[api.State]int)
	failureMessage := ""
	for _, benchmark := range job.Status.Benchmarks {
		benchmarkStates[benchmark.Status]++
		if benchmark.Status == api.StateFailed && benchmark.ErrorMessage != nil {
			failureMessage += "Benchmark " + benchmark.ID + " failed with message: " + benchmark.ErrorMessage.Message + "\n"
		}
	}

	// determine the overall job status
	total := len(job.Benchmarks)
	completed, failed, running := benchmarkStates[api.StateCompleted], benchmarkStates[api.StateFailed], benchmarkStates[api.StateRunning]

	var overallState api.OverallState
	var stateMessage string
	switch {
	case completed == total:
		overallState, stateMessage = api.OverallStateCompleted, "Evaluation job is completed"
	case failed == total:
		overallState, stateMessage = api.OverallStateFailed, "Evaluation job is failed. \n"+failureMessage
	case completed+failed == total:
		overallState, stateMessage = api.OverallStatePartiallyFailed, "Some of the benchmarks failed. \n"+failureMessage
	case running > 0:
		overallState, stateMessage = api.OverallStateRunning, "Evaluation job is running"
	default:
		overallState, stateMessage = api.OverallStatePending, "Evaluation job is pending"
	}

	return overallState, &api.MessageInfo{
		Message:     stateMessage,
		MessageCode: constants.MESSAGE_CODE_EVALUATION_JOB_UPDATED,
	}
}
