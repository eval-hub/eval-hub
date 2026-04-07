package sql

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/constants"
	"github.com/eval-hub/eval-hub/internal/eval_hub/handlers"
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

func (s *sqlStorage) GetEvaluationJobs(filter *abstractions.QueryFilter) (*abstractions.QueryResults[api.EvaluationJobResource], error) {
	var txn *sql.Tx
	return listEntities[api.EvaluationJobResource](s, txn, shared.TABLE_EVALUATIONS, filter)
}

func (s *sqlStorage) DeleteEvaluationJob(id string) error {
	// Build the DELETE query
	deleteQuery, args := s.statementsFactory.CreateDeleteEntityStatement(s.tenant, shared.TABLE_EVALUATIONS, id)

	// Execute the DELETE query
	result, err := s.exec(nil, deleteQuery, args...)
	if err != nil {
		if err == sql.ErrNoRows {
			s.logger.Debug("Evaluation job not found", "id", id)
			return se.NewServiceError(messages.ResourceNotFound, "Type", "evaluation job", "ResourceId", id)
		}
		s.logger.Error("Failed to delete evaluation job", "error", err, "id", id)
		return se.WithRollback(se.NewServiceError(messages.DatabaseOperationFailed, "Type", "evaluation job", "ResourceId", id, "Error", err.Error()))
	}
	if rows, err := result.RowsAffected(); ((err == nil) && (rows == 0)) || (err == sql.ErrNoRows) {
		s.logger.Debug("Evaluation job not found", "id", id)
		return se.NewServiceError(messages.ResourceNotFound, "Type", "evaluation job", "ResourceId", id)
	}

	s.logger.Info("Deleted evaluation job", "id", id)

	return nil
}

func (s *sqlStorage) checkEvaluationJobState(evaluationJobID string, evaluationJobState api.OverallState, state api.OverallState) (bool, error) {
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

func messageInfosEquivalent(a, b *api.MessageInfo) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Message == b.Message && a.MessageCode == b.MessageCode
}

func (s *sqlStorage) UpdateEvaluationJobStatus(id string, state api.OverallState, message *api.MessageInfo) error {
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
			if message == nil || messageInfosEquivalent(evaluationJob.Status.Message, message) {
				return nil
			}
			benchmarks := evaluationJob.Status.Benchmarks
			entity := EvaluationJobEntity{
				Config: &evaluationJob.EvaluationJobConfig,
				Status: &api.EvaluationJobStatus{
					EvaluationJobState: api.EvaluationJobState{
						State:   evaluationJob.Status.State,
						Message: message,
					},
					Benchmarks: benchmarks,
				},
				Results: evaluationJob.Results,
			}
			return s.updateEvaluationJobTxn(txn, id, evaluationJob.Status.State, &entity)
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

func (s *sqlStorage) updateEvaluationJobTxn(txn *sql.Tx, id string, status api.OverallState, evaluationJob *EvaluationJobEntity) error {
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
func (s *sqlStorage) validateBenchmarkExists(job *api.EvaluationJobResource, runStatus *api.StatusEvent, collection *api.CollectionResource) error {
	event := runStatus.BenchmarkStatusEvent
	benchmarks, err := handlers.GetJobBenchmarks(job, collection)
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

// GetOverallJobStatus returns overall state and message. getCollection is used to resolve job benchmark count when job has only a collection reference.
func (s *sqlStorage) getOverallJobStatus(job *api.EvaluationJobResource) (api.OverallState, *api.MessageInfo, error) {
	// to be safe - do an initial check to see if the job is finished
	if job.Status.State.IsTerminalState() {
		return job.Status.State, job.Status.Message, nil
	}

	// group all benchmarks by state
	benchmarkStates := make(map[api.State]int)
	failureMessage := ""
	for _, benchmark := range job.Status.Benchmarks {
		benchmarkStates[benchmark.Status]++
		if benchmark.Status == api.StateFailed && benchmark.ErrorMessage != nil {
			failureMessage += "Benchmark " + benchmark.ID + " failed with message: " + benchmark.ErrorMessage.Message + "\n"
		}
	}

	// determine the overall job status (use resolved benchmark count for collection-only jobs)
	var collection *api.CollectionResource
	var err error
	if job.Collection != nil && job.Collection.ID != "" {
		collection, err = s.GetCollection(job.Collection.ID)
		if err != nil {
			return api.OverallStatePending, &api.MessageInfo{
				Message:     "Evaluation job is pending",
				MessageCode: constants.MESSAGE_CODE_EVALUATION_JOB_UPDATED,
			}, err
		}
	}
	benchmarks, err := handlers.GetJobBenchmarks(job, collection)
	total := 0
	if err != nil || len(benchmarks) == 0 {
		return api.OverallStatePending, &api.MessageInfo{
			Message:     "Evaluation job is pending",
			MessageCode: constants.MESSAGE_CODE_EVALUATION_JOB_UPDATED,
		}, err
	}
	total = len(benchmarks)
	completed, failed, running, cancelled := benchmarkStates[api.StateCompleted], benchmarkStates[api.StateFailed], benchmarkStates[api.StateRunning], benchmarkStates[api.StateCancelled]

	var overallState api.OverallState
	var stateMessage string
	switch {
	case completed == total:
		overallState, stateMessage = api.OverallStateCompleted, "Evaluation job is completed"
	case failed == total:
		overallState, stateMessage = api.OverallStateFailed, "Evaluation job is failed. \n"+failureMessage
	case completed+failed == total:
		overallState, stateMessage = api.OverallStatePartiallyFailed, "Some of the benchmarks failed. \n"+failureMessage
	case cancelled == total:
		overallState, stateMessage = api.OverallStateCancelled, "Evaluation job is cancelled"
	case completed+failed+cancelled == total:
		overallState, stateMessage = api.OverallStatePartiallyFailed, "Some of the benchmarks failed or cancelled. \n"+failureMessage
	case running > 0, completed > 0, failed > 0, cancelled > 0: // if at least one benchmark has reported a state then the job is running
		overallState, stateMessage = api.OverallStateRunning, "Evaluation job is running"
	default:
		overallState, stateMessage = api.OverallStatePending, "Evaluation job is pending"
	}

	s.logger.Debug("Overall job state", "state", overallState, "completed", completed, "failed", failed, "running", running, "cancelled", cancelled, "total", total)

	return overallState, &api.MessageInfo{
		Message:     stateMessage,
		MessageCode: constants.MESSAGE_CODE_EVALUATION_JOB_UPDATED,
	}, nil
}

func (s *sqlStorage) updateBenchmarkStatus(job *api.EvaluationJobResource, runStatus *api.StatusEvent, benchmarkStatus *api.BenchmarkStatus) {
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
		if benchmark.ID == runStatus.BenchmarkStatusEvent.ID &&
			benchmark.ProviderID == runStatus.BenchmarkStatusEvent.ProviderID &&
			benchmark.BenchmarkIndex == runStatus.BenchmarkStatusEvent.BenchmarkIndex {
			job.Status.Benchmarks[index] = *benchmarkStatus
			return
		}
	}
	job.Status.Benchmarks = append(job.Status.Benchmarks, *benchmarkStatus)
}

func (s *sqlStorage) updateBenchmarkResults(job *api.EvaluationJobResource, runStatus *api.StatusEvent, result *api.BenchmarkResult) error {
	if job.Results == nil {
		job.Results = &api.EvaluationJobResults{}
	}
	if job.Results.Benchmarks == nil {
		job.Results.Benchmarks = make([]api.BenchmarkResult, 0)
	}

	for i, benchmark := range job.Results.Benchmarks {
		if benchmark.ID == runStatus.BenchmarkStatusEvent.ID &&
			benchmark.ProviderID == runStatus.BenchmarkStatusEvent.ProviderID &&
			benchmark.BenchmarkIndex == runStatus.BenchmarkStatusEvent.BenchmarkIndex {
			if reflect.DeepEqual(benchmark, *result) {
				return nil
			}
			job.Results.Benchmarks[i] = *result
			return nil
		}
	}

	job.Results.Benchmarks = append(job.Results.Benchmarks, *result)

	return nil
}

// UpdateEvaluationJobWithRunStatus runs in a transaction: fetches the job, merges RunStatusInternal into the entity, and persists.
func (s *sqlStorage) UpdateEvaluationJob(id string, runStatus *api.StatusEvent) error {
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

		var collection *api.CollectionResource
		if job.Collection != nil && job.Collection.ID != "" {
			collection, err = s.GetCollection(job.Collection.ID)
			if err != nil {
				return err
			}
		}
		err = s.validateBenchmarkExists(job, runStatus, collection)
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
		s.updateBenchmarkStatus(job, runStatus, &benchmark)

		outcome := s.computeBenchmarkTestResult(job, runStatus.BenchmarkStatusEvent, collection)

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
			err := s.updateBenchmarkResults(job, runStatus, &result)
			if err != nil {
				return err
			}
		}

		// get the overall job status
		overallState, message, err := s.getOverallJobStatus(job)
		if err != nil {
			return err
		}
		job.Status.State = overallState
		job.Status.Message = message

		s.logger.Info("Calculated overall job status", "id", id, "overall_state", overallState, "status", runStatus.BenchmarkStatusEvent.Status)

		// compute the job test result only if the job is completed
		if overallState == api.OverallStateCompleted {
			s.computeJobTestResult(job, collection)
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

func (s *sqlStorage) computeJobTestResult(job *api.EvaluationJobResource, collection *api.CollectionResource) {
	if job.Results == nil || job.Results.Benchmarks == nil || len(job.Results.Benchmarks) == 0 {
		return
	}
	var sumOfWeightedScores float32 = 0.0
	var sumOfWeights float32 = 0.0
	resolvedJobBenchmarks, err := handlers.GetJobBenchmarks(job, collection)
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

	threshold := s.getPassCriteriaThreshold(job, collection)
	jobTest = &api.EvaluationTest{
		Score:     weightedAvgJobScore,
		Threshold: threshold,
		Pass:      weightedAvgJobScore >= threshold,
	}

	job.Results.Test = jobTest
}

func (s *sqlStorage) getPassCriteriaThreshold(job *api.EvaluationJobResource, collection *api.CollectionResource) float32 {
	if job.EvaluationJobConfig.PassCriteria != nil {
		return job.EvaluationJobConfig.PassCriteria.Threshold
	}
	if collection != nil && collection.PassCriteria != nil {
		return collection.PassCriteria.Threshold
	}
	// this is the hard-coded default pass criteria threshold
	return 0.5
}

func (s *sqlStorage) computeBenchmarkTestResult(job *api.EvaluationJobResource, benchmarkStatusEvent *api.BenchmarkStatusEvent, collection *api.CollectionResource) *api.BenchmarkTest {
	// job could have benchmarks array or it could have collection. If it has collection, we need to get the benchmarks from the collection
	benchmarks, err := handlers.GetJobBenchmarks(job, collection)
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
