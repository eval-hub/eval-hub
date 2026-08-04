package sql

import (
	"database/sql"
	"encoding/json"

	"github.com/eval-hub/eval-hub/internal/eval_hub/constants"
	"github.com/eval-hub/eval-hub/internal/eval_hub/handlers"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	se "github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/internal/eval_hub/storage/sql/shared"
	"github.com/eval-hub/eval-hub/pkg/api"
)

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
	return a.Message == b.Message && a.MessageCode == b.MessageCode && a.MessageOrigin == b.MessageOrigin
}

func (s *sqlStorage) UpdateEvaluationJobStatus(id string, state api.OverallState, message *api.MessageInfo) error {
	api.WithMessageOrigin(message, api.MessageOriginServer)
	// we have to get the evaluation job and update the status so we need a transaction
	s.logger.Debug("Updating evaluation job status", "id", id, "state", state, "message", message)
	err := s.withTransaction("update evaluation job status", id, func(txn *sql.Tx) error {
		// get the evaluation job
		evaluationJob, err := s.getEvaluationJobTransactionalForUpdate(txn, id)
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
func (s *sqlStorage) getOverallJobStatus(txn *sql.Tx, job *api.EvaluationJobResource) (api.OverallState, *api.MessageInfo, error) {
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
		collection, err = s.getCollectionTransactional(txn, job.Collection.ID)
		if err != nil {
			return api.OverallStatePending, api.WithMessageOrigin(&api.MessageInfo{
				Message:     "Evaluation job is pending",
				MessageCode: constants.MESSAGE_CODE_EVALUATION_JOB_UPDATED,
			}, api.MessageOriginServer), err
		}
	}
	benchmarks, err := handlers.GetJobBenchmarks(job, collection)
	total := 0
	if err != nil || len(benchmarks) == 0 {
		return api.OverallStatePending, api.WithMessageOrigin(&api.MessageInfo{
			Message:     "Evaluation job is pending",
			MessageCode: constants.MESSAGE_CODE_EVALUATION_JOB_UPDATED,
		}, api.MessageOriginServer), err
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

	return overallState, api.WithMessageOrigin(&api.MessageInfo{
		Message:     stateMessage,
		MessageCode: constants.MESSAGE_CODE_EVALUATION_JOB_UPDATED,
	}, api.MessageOriginServer), nil
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
			if api.IsBenchmarkTerminalState(benchmark.Status) && !api.IsBenchmarkTerminalState(benchmarkStatus.Status) {
				return
			}
			job.Status.Benchmarks[index] = *benchmarkStatus
			return
		}
	}
	job.Status.Benchmarks = append(job.Status.Benchmarks, *benchmarkStatus)
}

// UpdateEvaluationJobWithRunStatus runs in a transaction: fetches the job, merges RunStatusInternal into the entity, and persists.
func (s *sqlStorage) UpdateEvaluationJob(id string, runStatus *api.StatusEvent) error {
	return s.withTransaction("update evaluation job", id, func(txn *sql.Tx) error {
		s.logger.Info("Updating evaluation job", "id", id, "status", runStatus.BenchmarkStatusEvent.Status, "runStatus", runStatus)

		job, err := s.getEvaluationJobTransactionalForUpdate(txn, id)
		if err != nil {
			return err
		}
		// Test hook: no-op unless a test installs a callback (see test_hooks.go).
		invokeEvaluationJobUpdateAfterLockedReadHook(id, runStatus.BenchmarkStatusEvent.ID)

		// Guard: reject benchmark updates if job is already in a terminal state.
		// We pass OverallStateRunning as the target to leverage checkEvaluationJobState's terminal-state check.
		if _, err := s.checkEvaluationJobState(job.Resource.ID, job.Status.State, api.OverallStateRunning); err != nil {
			return err
		}

		var collection *api.CollectionResource
		if job.Collection != nil && job.Collection.ID != "" {
			collection, err = s.getCollectionTransactional(txn, job.Collection.ID)
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
			Phase:          runStatus.BenchmarkStatusEvent.Phase,
			ErrorMessage:   runStatus.BenchmarkStatusEvent.ErrorMessage,
			WarningMessage: runStatus.BenchmarkStatusEvent.WarningMessage,
			StartedAt:      runStatus.BenchmarkStatusEvent.StartedAt,
			CompletedAt:    runStatus.BenchmarkStatusEvent.CompletedAt,
			BenchmarkIndex: runStatus.BenchmarkStatusEvent.BenchmarkIndex,
		}
		s.updateBenchmarkStatus(job, runStatus, &benchmark)

		outcome := s.computeBenchmarkTestResult(txn, job, runStatus.BenchmarkStatusEvent, collection)

		// if the run status is terminal, we need to update the results
		if api.IsBenchmarkTerminalState(runStatus.BenchmarkStatusEvent.Status) {
			result := api.BenchmarkResult{
				ID:             runStatus.BenchmarkStatusEvent.ID,
				ProviderID:     runStatus.BenchmarkStatusEvent.ProviderID,
				Metrics:        runStatus.BenchmarkStatusEvent.Metrics,
				AdditionalInfo: runStatus.BenchmarkStatusEvent.AdditionalInfo,
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
		overallState, message, err := s.getOverallJobStatus(txn, job)
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

		if err := s.updateEvaluationJobTxn(txn, id, overallState, &entity); err != nil {
			return err
		}

		return nil
	})
}
