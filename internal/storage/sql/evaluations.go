package sql

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	evalcommon "github.com/eval-hub/eval-hub/internal/common"
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
	if err := s.verifyTenant(); err != nil {
		return err
	}

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
	if err := s.verifyTenant(); err != nil {
		return nil, err
	}
	return s.getEvaluationJobTransactional(nil, id)
}

func (s *SQLStorage) getEvaluationJobTransactional(txn *sql.Tx, id string) (*api.EvaluationJobResource, error) {
	// Build the SELECT query
	query := shared.EntityQuery{Resource: api.Resource{ID: id, Tenant: s.tenant}}
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
		return nil, se.WithRollback(se.NewServiceError(messages.JSONUnmarshalFailed, "Type", "evaluation job", "Error", err.Error()))
	}

	status := ""
	job, err := constructEvaluationResource(s.logger, &query, status, &evaluationJobEntity)
	if err != nil {
		return nil, se.WithRollback(err)
	}
	return job, nil
}

func (s *SQLStorage) GetEvaluationJobs(filter *abstractions.QueryFilter) (*abstractions.QueryResults[api.EvaluationJobResource], error) {
	if err := s.verifyTenant(); err != nil {
		return nil, err
	}

	var txn *sql.Tx
	return listEntities[api.EvaluationJobResource](s, txn, shared.TABLE_EVALUATIONS, filter)
}

func (s *SQLStorage) DeleteEvaluationJob(id string) error {
	if err := s.verifyTenant(); err != nil {
		return err
	}

	// we have to get the evaluation job and then update or delete the job so we need a transaction
	err := s.withTransaction("delete evaluation job", id, func(txn *sql.Tx) error {
		// check if the evaluation job exists, we do this otherwise we always return 204
		selectQuery, args := s.statementsFactory.CreateCheckEntityExistsStatement(s.tenant, shared.TABLE_EVALUATIONS, id)
		var dbID string
		var statusStr string
		err := s.queryRow(txn, selectQuery, args...).Scan(&dbID, &statusStr)
		if err != nil {
			if err == sql.ErrNoRows {
				return se.NewServiceError(messages.ResourceNotFound, "Type", "evaluation job", "ResourceId", id)
			}
			return se.WithRollback(se.NewServiceError(messages.DatabaseOperationFailed, "Type", "evaluation job", "ResourceId", id, "Error", err.Error()))
		}

		// Build the DELETE query
		deleteQuery, args := s.statementsFactory.CreateDeleteEntityStatement(s.tenant, shared.TABLE_EVALUATIONS, id)

		// Execute the DELETE query
		_, err = s.exec(txn, deleteQuery, args...)
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
	if err := s.verifyTenant(); err != nil {
		return err
	}

	// we have to get the evaluation job and update the status so we need a transaction
	s.logger.Debug("Updating evaluation job status", "id", id, "state", state, "message", message)
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

		benchmarks := evaluationJob.Status.Benchmarks

		// When cancelling a job, cascade cancellation to all non-terminal benchmarks
		if state == api.OverallStateCancelled {
			for i := range benchmarks {
				if !api.IsBenchmarkTerminalState(benchmarks[i].Status) {
					benchmarks[i].Status = api.StateCancelled
					benchmarks[i].ErrorMessage = message
				}
			}
		}

		entity := EvaluationJobEntity{
			Config: &evaluationJob.EvaluationJobConfig,
			Status: &api.EvaluationJobStatus{
				EvaluationJobState: api.EvaluationJobState{
					State:   state,
					Message: message,
				},
				Benchmarks: benchmarks,
			},
			Results: evaluationJob.Results,
		}

		return s.updateEvaluationJobTxn(txn, id, state, &entity)
	})
	return err
}

func (s *SQLStorage) updateEvaluationJobTxn(txn *sql.Tx, id string, status api.OverallState, evaluationJob *EvaluationJobEntity) error {
	entityJSON, err := json.Marshal(evaluationJob)
	if err != nil {
		// we should never get here
		return se.WithRollback(se.NewServiceError(messages.InternalServerError, "Error", err.Error()))
	}
	updateQuery, args := s.statementsFactory.CreateUpdateEntityStatement(s.tenant, shared.TABLE_EVALUATIONS, id, string(entityJSON), &status)

	_, err = s.exec(txn, updateQuery, args...)
	if err != nil {
		s.logger.Error("Failed to update evaluation job", "error", err, "id", id, "status", status)
		return se.WithRollback(se.NewServiceError(messages.DatabaseOperationFailed, "Type", "evaluation job", "ResourceId", id, "Error", err.Error()))
	}

	s.logger.Info("Updated evaluation job", "id", id, "status", status)

	return nil
}

// validateBenchmarkExists checks that the event's benchmark is valid for the job (in job.Benchmarks or in the job's collection).
func (s *SQLStorage) validateBenchmarkExists(job *api.EvaluationJobResource, runStatus *api.StatusEvent) error {
	event := runStatus.BenchmarkStatusEvent
	benchmarks, err := evalcommon.GetJobBenchmarks(job, s.GetCollection)
	if err != nil {
		s.logger.Error("Failed to get job benchmarks", "error", err, "job_id", job.Resource.ID)
		return err
	}
	if len(benchmarks) == 0 {
		return se.NewServiceError(messages.ResourceNotFound, "Type", "benchmark", "ResourceId", event.ID, "Error", "Invalid Benchmark for the evaluation job")
	}
	found := false
	for index, benchmark := range benchmarks {
		if benchmark.ID == event.ID &&
			benchmark.ProviderID == event.ProviderID &&
			index == event.BenchmarkIndex {
			found = true
			break
		}
	}
	if !found {
		return se.NewServiceError(messages.ResourceNotFound, "Type", "benchmark", "ResourceId", event.ID, "Error", "Invalid Benchmark for the evaluation job")
	}
	return nil
}

// UpdateEvaluationJobWithRunStatus runs in a transaction: fetches the job, merges RunStatusInternal into the entity, and persists.
func (s *SQLStorage) UpdateEvaluationJob(id string, runStatus *api.StatusEvent) error {
	if err := s.verifyTenant(); err != nil {
		return err
	}

	err := s.withTransaction("update evaluation job", id, func(txn *sql.Tx) error {
		s.logger.Info("Updating evaluation job", "id", id, "status", runStatus.BenchmarkStatusEvent.Status, "runStatus", runStatus)

		job, err := s.getEvaluationJobTransactional(txn, id)
		if err != nil {
			return err
		}

		// Guard: reject benchmark updates if job is already in a terminal state.
		// We pass OverallStateRunning as the target to leverage checkEvaluationJobState's terminal-state check.
		if _, err := s.checkEvaluationJobState(job.Resource.ID, job.Status.State, api.OverallStateRunning); err != nil {
			return err
		}

		err = s.validateBenchmarkExists(job, runStatus)
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

		outcome := s.computeBenchmarkTestResult(job, runStatus.BenchmarkStatusEvent)

		// if the run status is terminal, we need to update the results
		if api.IsBenchmarkTerminalState(runStatus.BenchmarkStatusEvent.Status) {
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
		overallState, message := commonStorage.GetOverallJobStatus(s.logger, job)
		job.Status.State = overallState
		job.Status.Message = message

		s.logger.Info("Calculated overall job status", "id", id, "overall_state", overallState, "status", runStatus.BenchmarkStatusEvent.Status)

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
	resolvedJobBenchmarks, err := evalcommon.GetJobBenchmarks(job, s.GetCollection)
	if err != nil {
		s.logger.Error("Failed to get job benchmarks", "error", err, "job_id", job.Resource.ID)
		return
	}
	for _, benchmark := range job.Results.Benchmarks {
		if benchmark.Test == nil {
			// if the benchmark test result is not defined, we skip it
			// This should never happen, since this method is called only when the overall job status is 'completed'
			s.logger.Info("Benchmark test result is not defined for benchmark", "benchmark_id", benchmark.ID, "benchmark_index", benchmark.BenchmarkIndex)
			continue
		}
		if benchmark.BenchmarkIndex < 0 || benchmark.BenchmarkIndex >= len(resolvedJobBenchmarks) {
			s.logger.Warn(
				"benchmark index out of range for resolved benchmarks",
				"benchmark_id", benchmark.ID,
				"benchmark_index", benchmark.BenchmarkIndex,
				"resolved_count", len(resolvedJobBenchmarks),
			)
			continue
		}
		benchmarkWeight := resolvedJobBenchmarks[benchmark.BenchmarkIndex].Weight
		if benchmarkWeight == 0 {
			// if the benchmark weight is not defined, we set it to 1
			benchmarkWeight = 1
		}
		weightedScore := benchmarkWeight * benchmark.Test.PrimaryScore
		if primaryScore := resolvedJobBenchmarks[benchmark.BenchmarkIndex].PrimaryScore; primaryScore != nil && primaryScore.LowerIsBetter {
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

func (s *SQLStorage) computeBenchmarkTestResult(job *api.EvaluationJobResource, benchmarkStatusEvent *api.BenchmarkStatusEvent) *api.BenchmarkTest {
	// job could have benchmarks array or it could have collection. If it has collection, we need to get the benchmarks from the collection
	benchmarks, err := evalcommon.GetJobBenchmarks(job, s.GetCollection)
	if err != nil {
		s.logger.Error("Failed to get job benchmarks", "error", err, "job_id", job.Resource.ID)
		return nil
	}
	if len(benchmarks) == 0 {
		return nil
	}
	for _, benchmark := range benchmarks {
		if benchmark.ID != benchmarkStatusEvent.ID || benchmark.ProviderID != benchmarkStatusEvent.ProviderID {
			continue
		}
		primaryScore := benchmark.PrimaryScore
		var providerBench *api.BenchmarkResource
		// if the primary score is not defined, we need to get the primary score from the provider
		if (primaryScore == nil || primaryScore.Metric == "") && benchmark.ProviderID != "" {
			provider, err := s.GetProvider(benchmark.ProviderID)
			if err == nil && provider != nil {
				for i := range provider.Benchmarks {
					if provider.Benchmarks[i].ID == benchmark.ID {
						providerBench = &provider.Benchmarks[i]
						break
					}
				}
			}
			if providerBench != nil && providerBench.PrimaryScore != nil && providerBench.PrimaryScore.Metric != "" {
				primaryScore = providerBench.PrimaryScore
			}
		}
		if primaryScore != nil && primaryScore.Metric != "" {
			primaryMetric := primaryScore.Metric
			if primaryMetricValue, ok := benchmarkStatusEvent.Metrics[primaryMetric]; ok {
				primaryMetricValueFloat, err := castAnyToFloat32(primaryMetricValue)
				if err != nil {
					s.logger.Error("Failed to cast primary metric value to float32", "error", err, "primary_metric", primaryMetric, "primary_metric_value", primaryMetricValue)
					return nil
				}
				var threshold float32
				if benchmark.PassCriteria != nil {
					threshold = benchmark.PassCriteria.Threshold
				} else if providerBench != nil && providerBench.PassCriteria != nil {
					threshold = providerBench.PassCriteria.Threshold
				} else {
					return nil
				}
				pass := primaryMetricValueFloat >= threshold
				if primaryScore.LowerIsBetter {
					pass = primaryMetricValueFloat <= threshold
				}
				return &api.BenchmarkTest{
					PrimaryScore: primaryMetricValueFloat,
					Threshold:    threshold,
					Pass:         pass,
				}
			}
		}
	}
	return nil
}

func castAnyToFloat32(primaryMetricValue any) (float32, error) {
	var primaryMetricValueFloat float32
	switch v := primaryMetricValue.(type) {
	case float64:
		primaryMetricValueFloat = float32(v)
	case float32:
		primaryMetricValueFloat = v
	case int:
		primaryMetricValueFloat = float32(v)
	case int32:
		primaryMetricValueFloat = float32(v)
	case int64:
		primaryMetricValueFloat = float32(v)
	default:
		return 0, fmt.Errorf("unsupported type: %T for primary metric value", primaryMetricValue)
	}
	return primaryMetricValueFloat, nil
}
