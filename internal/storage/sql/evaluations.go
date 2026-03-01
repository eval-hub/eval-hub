package sql

import (
	"database/sql"
	"encoding/json"
	"maps"
	"slices"
	"time"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/messages"
	se "github.com/eval-hub/eval-hub/internal/serviceerrors"
	commonStorage "github.com/eval-hub/eval-hub/internal/storage/common"
	"github.com/eval-hub/eval-hub/internal/storage/sql/shared"
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

func (s *SQLStorage) constructEvaluationResource(tenantID string, statusStr string, message *api.MessageInfo, dbID string, createdAt time.Time, updatedAt time.Time, experimentID string, evaluationEntity *EvaluationJobEntity) (*api.EvaluationJobResource, error) {
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
	status.Message = message

	tenant := api.Tenant(tenantID)
	evaluationResource := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{
				ID:        dbID,
				Tenant:    &tenant,
				CreatedAt: &createdAt,
				UpdatedAt: &updatedAt,
			},
			MLFlowExperimentID: experimentID,
		},
		Status:              status,
		EvaluationJobConfig: *evaluationEntity.Config,
		Results:             evaluationEntity.Results,
	}
	return evaluationResource, nil
}

func (s *SQLStorage) getEvaluationJobTransactional(txn *sql.Tx, id string) (*api.EvaluationJobResource, error) {
	// Build the SELECT query
	query := shared.EvaluationJobQuery{ID: id}
	selectQuery, selectArgs, queryArgs := s.statementsFactory.CreateEvaluationGetEntityStatement(&query)

	// Query the database
	err := s.queryRow(txn, selectQuery, selectArgs...).Scan(queryArgs...)
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
	err = json.Unmarshal([]byte(query.EntityJSON), &evaluationJobEntity)
	if err != nil {
		s.logger.Error("Failed to unmarshal evaluation job entity", "error", err, "id", id)
		return nil, se.NewServiceError(messages.JSONUnmarshalFailed, "Type", "evaluation job", "Error", err.Error())
	}

	job, err := s.constructEvaluationResource(query.Tenant, query.Status, nil, query.ID, query.CreatedAt, query.UpdatedAt, query.ExperimentID, &evaluationJobEntity)
	if err != nil {
		return nil, se.WithRollback(err)
	}
	return job, nil
}

func (s *SQLStorage) GetEvaluationJobs(filter *abstractions.QueryFilter) (*abstractions.QueryResults[api.EvaluationJobResource], error) {
	filter = shared.ExtractQueryParams(filter)
	params := filter.Params
	limit := filter.Limit
	offset := filter.Offset

	if err := shared.ValidateFilter(slices.Collect(maps.Keys(params)), []string{"tenant_id", "status", "experiment_id"}); err != nil {
		return nil, err
	}

	// Get total count (with filter if provided)
	countQuery, countArgs := s.statementsFactory.CreateCountEntitiesStatement(shared.TABLE_EVALUATIONS, filter.Params)

	var totalCount int
	var err error
	if len(countArgs) > 0 {
		err = s.queryRow(nil, countQuery, countArgs...).Scan(&totalCount)
	} else {
		err = s.queryRow(nil, countQuery).Scan(&totalCount)
	}
	if err != nil {
		if err == sql.ErrNoRows {
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
	listQuery, listArgs := s.statementsFactory.CreateListEntitiesStatement(shared.TABLE_EVALUATIONS, limit, offset, params)
	s.logger.Info("List evaluations query", "query", listQuery, "args", listArgs, "params", params, "limit", limit, "offset", offset)

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
		var evaluationJobEntity EvaluationJobEntity
		err = json.Unmarshal([]byte(entityJSON), &evaluationJobEntity)
		if err != nil {
			s.logger.Error("Failed to unmarshal evaluation job entity", "error", err, "id", dbID)
			return nil, se.NewServiceError(messages.JSONUnmarshalFailed, "Type", "evaluation job", "Error", err.Error())
		}

		// Construct the EvaluationJobResource
		resource, err := s.constructEvaluationResource(tenantID, statusStr, nil, dbID, createdAt, updatedAt, experimentID, &evaluationJobEntity)
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
		selectQuery := s.statementsFactory.CreateCheckEntityExistsStatement(shared.TABLE_EVALUATIONS)
		var dbID string
		var statusStr string
		err := s.queryRow(txn, selectQuery, id).Scan(&dbID, &statusStr)
		if err != nil {
			if err == sql.ErrNoRows {
				return se.NewServiceError(messages.ResourceNotFound, "Type", "evaluation job", "ResourceId", id)
			}
			return se.WithRollback(se.NewServiceError(messages.DatabaseOperationFailed, "Type", "evaluation job", "ResourceId", id, "Error", err.Error()))
		}

		// Build the DELETE query
		deleteQuery := s.statementsFactory.CreateDeleteEntityStatement(shared.TABLE_EVALUATIONS)

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

func (s *SQLStorage) checkEvaluationJobState(evaluationJobID string, evaluationJobState api.OverallState, state api.OverallState) (bool, error) {
	// check if the state is unchanged
	if state == evaluationJobState {
		// if the state is the same as the current state then we don't need to update the status
		// we don't treat this as an error for now, we just return 204
		return true, nil
	}

	// check if the job is in a final state
	switch evaluationJobState {
	case api.OverallStateCancelled, api.OverallStateCompleted, api.OverallStateFailed, api.OverallStatePartiallyFailed:
		// the job is already in a final state, so we can't update the status
		return false, se.NewServiceError(messages.JobCanNotBeUpdated, "Id", evaluationJobID, "NewStatus", state, "Status", evaluationJobState)
	}

	return false, nil
}

func (s *SQLStorage) UpdateEvaluationJobStatus(id string, state api.OverallState, message *api.MessageInfo) error {
	// we have to get the evaluation job and update the status so we need a transaction
	err := s.withTransaction("update evaluation job status", id, func(txn *sql.Tx) error {
		// get the evaluation job
		evaluationJob, err := s.getEvaluationJobTransactional(txn, id)
		if err != nil {
			return err
		}

		// check the state
		sameState, err := s.checkEvaluationJobState(evaluationJob.Resource.ID, evaluationJob.Status.State, state)
		if err != nil {
			return err
		}
		if sameState {
			// if the state is the same as the current state then we don't need to update the status
			// we don't treat this as an error for now, we just return 204
			return nil
		}

		entity := EvaluationJobEntity{
			Config: &evaluationJob.EvaluationJobConfig,
			Status: &api.EvaluationJobStatus{
				EvaluationJobState: api.EvaluationJobState{
					State:   state,
					Message: message,
				},
				Benchmarks: evaluationJob.Status.Benchmarks,
			},
			Results: evaluationJob.Results,
		}

		if err := s.updateEvaluationJobTxn(txn, id, state, &entity); err != nil {
			return err
		}
		s.logger.Info("Updated evaluation job status", "id", id, "overall_state", state, "message", message)
		return nil
	})
	return err
}

func (s *SQLStorage) updateEvaluationJobTxn(txn *sql.Tx, id string, status api.OverallState, evaluationJob *EvaluationJobEntity) error {
	entityJSON, err := json.Marshal(evaluationJob)
	if err != nil {
		// we should never get here
		return se.WithRollback(se.NewServiceError(messages.InternalServerError, "Error", err.Error()))
	}
	updateQuery, args := s.statementsFactory.CreateUpdateEntityStatement(shared.TABLE_EVALUATIONS, id, string(entityJSON), &status)

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

		// Guard: reject benchmark updates if job is already in a terminal state.
		// We pass OverallStateRunning as the target to leverage checkEvaluationJobState's terminal-state check.
		if _, err := s.checkEvaluationJobState(job.Resource.ID, job.Status.State, api.OverallStateRunning); err != nil {
			return err
		}

		err = commonStorage.ValidateBenchmarkExists(job, runStatus)
		if err != nil {
			return err
		}

		// first we store the benchmark status
		benchmark := api.BenchmarkStatus{
			ProviderID:     runStatus.BenchmarkStatusEvent.ProviderID,
			ID:             runStatus.BenchmarkStatusEvent.ID,
			Status:         runStatus.BenchmarkStatusEvent.Status,
			ErrorMessage:   runStatus.BenchmarkStatusEvent.ErrorMessage,
			StartedAt:      runStatus.BenchmarkStatusEvent.StartedAt,
			CompletedAt:    runStatus.BenchmarkStatusEvent.CompletedAt,
			BenchmarkIndex: runStatus.BenchmarkStatusEvent.BenchmarkIndex,
		}
		commonStorage.UpdateBenchmarkStatus(job, runStatus, &benchmark)

		outcome := computeBenchmarkTestResult(job, runStatus.BenchmarkStatusEvent)

		// if the run status is completed, failed, or cancelled, we need to update the results
		if runStatus.BenchmarkStatusEvent.Status == api.StateCompleted || runStatus.BenchmarkStatusEvent.Status == api.StateFailed || runStatus.BenchmarkStatusEvent.Status == api.StateCancelled {
			result := api.BenchmarkResult{
				ID:             runStatus.BenchmarkStatusEvent.ID,
				ProviderID:     runStatus.BenchmarkStatusEvent.ProviderID,
				Metrics:        runStatus.BenchmarkStatusEvent.Metrics,
				Artifacts:      runStatus.BenchmarkStatusEvent.Artifacts,
				MLFlowRunID:    runStatus.BenchmarkStatusEvent.MLFlowRunID,
				LogsPath:       runStatus.BenchmarkStatusEvent.LogsPath,
				BenchmarkIndex: runStatus.BenchmarkStatusEvent.BenchmarkIndex,
				Test:           outcome,
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

		// compute the job test result only if the job is completed
		if overallState == api.OverallStateCompleted {
			s.computeJobTestResult(job)
		}

		entity := EvaluationJobEntity{
			Config:  &job.EvaluationJobConfig,
			Status:  job.Status,
			Results: job.Results,
		}

		return s.updateEvaluationJobTxn(txn, id, overallState, &entity)
	})

	return err
}

func (s *SQLStorage) computeJobTestResult(job *api.EvaluationJobResource) {
	if job.Results == nil || job.Results.Benchmarks == nil || len(job.Results.Benchmarks) == 0 {
		return
	}
	var sumOfWeightedScores float32 = 0.0
	var sumOfWeights float32 = 0.0
	for _, benchmark := range job.Results.Benchmarks {
		if benchmark.Test == nil {
			// if the benchmark test result is not defined, we skip it
			// This should never happen, since this method is called only when the overall job status is 'completed'
			s.logger.Info("Benchmark test result is not defined for benchmark", "benchmark_id", benchmark.ID, "benchmark_index", benchmark.BenchmarkIndex)
			continue
		}
		benchmarkWeight := job.Benchmarks[benchmark.BenchmarkIndex].Weight
		if benchmarkWeight == 0 {
			// if the benchmark weight is not defined, we set it to 1
			benchmarkWeight = 1
		}
		weightedScore := benchmarkWeight * benchmark.Test.PrimaryScore
		if job.Benchmarks[benchmark.BenchmarkIndex].PrimaryScore.LowerIsBetter {
			weightedScore = benchmarkWeight * (1 - benchmark.Test.PrimaryScore)
		}
		sumOfWeightedScores += weightedScore
		sumOfWeights += benchmarkWeight
		s.logger.Info("Benchmark test result", "benchmark_id", benchmark.ID, "benchmark_index", benchmark.BenchmarkIndex, "primary_score", benchmark.Test.PrimaryScore, "weighted_score", weightedScore, "benchmark_weight", benchmarkWeight, "sum_of_weighted_scores", sumOfWeightedScores, "sum_of_weights", sumOfWeights)
	}
	if sumOfWeights == 0 {
		s.logger.Warn("No benchmark weights accumulated; cannot compute job score")
		return
	}
	weightedAvgJobScore := sumOfWeightedScores / sumOfWeights
	s.logger.Info("Weighted average job score", "weighted_avg_job_score", weightedAvgJobScore, "sum_of_weighted_scores", sumOfWeightedScores, "sum_of_weights", sumOfWeights)
	var jobTest *api.EvaluationTest = nil
	// We set 'test' on the evaluation job only if the pass criteria is defined
	if job.EvaluationJobConfig.PassCriteria != nil {
		jobTest = &api.EvaluationTest{
			Score:     weightedAvgJobScore,
			Threshold: job.EvaluationJobConfig.PassCriteria.Threshold,
			Pass:      weightedAvgJobScore >= job.EvaluationJobConfig.PassCriteria.Threshold,
		}
	}

	job.Results.Test = jobTest
}

func computeBenchmarkTestResult(job *api.EvaluationJobResource, benchmarkStatusEvent *api.BenchmarkStatusEvent) *api.BenchmarkTest {
	for _, benchmark := range job.Benchmarks {
		if benchmark.ID == benchmarkStatusEvent.ID && benchmark.ProviderID == benchmarkStatusEvent.ProviderID {
			//TODO: If primary score is not defined in the API request, the default primary score for the benchmark should be read from the provider.
			//TBD after the code to access providers from 'internal' package is implemented.
			if benchmark.PrimaryScore != nil && benchmark.PrimaryScore.Metric != "" {
				primaryMetric := benchmark.PrimaryScore.Metric
				if primaryMetricValue, ok := benchmarkStatusEvent.Metrics[primaryMetric]; ok {
					primaryMetricValueFloat := float32(primaryMetricValue.(float64))
					passCriteria := benchmark.PassCriteria.Threshold
					pass := primaryMetricValueFloat >= passCriteria
					if benchmark.PrimaryScore.LowerIsBetter {
						pass = primaryMetricValueFloat <= passCriteria
					}
					return &api.BenchmarkTest{
						PrimaryScore: primaryMetricValueFloat,
						Threshold:    benchmark.PassCriteria.Threshold,
						Pass:         pass,
					}
				}
			}
		}
	}
	return nil
}
