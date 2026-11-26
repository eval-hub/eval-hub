package sql

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/constants"
	"github.com/eval-hub/eval-hub/internal/eval_hub/generation"
	"github.com/eval-hub/eval-hub/internal/eval_hub/handlers"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	se "github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/internal/eval_hub/workloads"
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
					Benchmarks:                   benchmarks,
					CollectionGenerationProgress: evaluationJob.Status.CollectionGenerationProgress,
				},
				Results: evaluationJob.Results,
			}
			return s.updateEvaluationJobTxn(txn, id, evaluationJob.Status.State, &entity)
		}

		benchmarks := evaluationJob.Status.Benchmarks

		// Finalize unfinished benchmarks when the job is cancelled or cannot start.
		if state == api.OverallStateCancelled || state == api.OverallStateFailed {
			benchmarkState := api.StateCancelled
			if state == api.OverallStateFailed {
				benchmarkState = api.StateFailed
			}
			now := time.Now()
			for i := range benchmarks {
				if !api.IsBenchmarkTerminalState(benchmarks[i].Status) {
					benchmarks[i].Status = benchmarkState
					benchmarks[i].ErrorMessage = message
					setBenchmarkCompletionTime(&benchmarks[i], now)
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
				Benchmarks:                   benchmarks,
				CollectionGenerationProgress: evaluationJob.Status.CollectionGenerationProgress,
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
	var expiresAt *time.Time
	if evaluationJob.Config != nil && workloads.TypeForJob(evaluationJob.Config) == workloads.CollectionGeneration &&
		(status == api.OverallStateCompleted || status == api.OverallStateFailed || status == api.OverallStateCancelled) {
		expiry := time.Now().UTC().Add(generation.RetentionForJob(evaluationJob.Config))
		expiresAt = &expiry
	}
	updateQuery, args := s.statementsFactory.CreateUpdateEvaluationEntityStatement(s.tenant, id, string(entityJSON), status, expiresAt)

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
				MessageCode: constants.MessageCodeEvaluationJobUpdated,
			}, api.MessageOriginServer), err
		}
	}
	benchmarks, err := handlers.GetJobBenchmarks(job, collection)
	total := 0
	if err != nil || len(benchmarks) == 0 {
		return api.OverallStatePending, api.WithMessageOrigin(&api.MessageInfo{
			Message:     "Evaluation job is pending",
			MessageCode: constants.MessageCodeEvaluationJobUpdated,
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
		MessageCode: constants.MessageCodeEvaluationJobUpdated,
	}, api.MessageOriginServer), nil
}

// setBenchmarkCompletionTime records completion when a terminal event omitted it.
func setBenchmarkCompletionTime(benchmark *api.BenchmarkStatus, now time.Time) {
	if api.IsBenchmarkTerminalState(benchmark.Status) && benchmark.CompletedAt == "" {
		benchmark.CompletedAt = api.DateTimeToString(now.UTC())
	}
}

func (s *sqlStorage) updateBenchmarkStatus(job *api.EvaluationJobResource, runStatus *api.StatusEvent, benchmarkStatus *api.BenchmarkStatus, receivedAt time.Time) {
	// A pending benchmark can start on its first running event, or use an
	// explicitly supplied start when an adapter skips running and reports terminal.
	if benchmarkStatus.Status == api.StateRunning {
		if benchmarkStatus.StartedAt == "" {
			benchmarkStatus.StartedAt = api.DateTimeToString(receivedAt.UTC())
		}
	} else if !api.IsBenchmarkTerminalState(benchmarkStatus.Status) {
		benchmarkStatus.StartedAt = ""
	}
	if !api.IsBenchmarkTerminalState(benchmarkStatus.Status) {
		benchmarkStatus.CompletedAt = ""
	}
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
			// Ignore late pending events once the benchmark is already running.
			if benchmark.Status == api.StateRunning && benchmarkStatus.Status == api.StatePending {
				return
			}
			// Only a transition out of pending may establish a start. Preserve
			// even an absent start on later running or terminal status updates.
			if benchmark.StartedAt != "" || benchmark.Status != api.StatePending {
				benchmarkStatus.StartedAt = benchmark.StartedAt
			}
			if api.IsBenchmarkTerminalState(benchmark.Status) && benchmark.CompletedAt != "" {
				benchmarkStatus.CompletedAt = benchmark.CompletedAt
			}
			setBenchmarkCompletionTime(benchmarkStatus, time.Now())
			job.Status.Benchmarks[index] = *benchmarkStatus
			return
		}
	}
	setBenchmarkCompletionTime(benchmarkStatus, time.Now())
	job.Status.Benchmarks = append(job.Status.Benchmarks, *benchmarkStatus)
}

// UpdateEvaluationJobWithRunStatus runs in a transaction: fetches the job, merges RunStatusInternal into the entity, and persists.
func (s *sqlStorage) UpdateEvaluationJob(id string, runStatus *api.StatusEvent) error {
	if runStatus == nil || runStatus.BenchmarkStatusEvent == nil {
		return se.NewServiceError(messages.RequestValidationFailed, "Error", "benchmark status event is required")
	}
	// Capture once for internal runtime callers that bypass the events handler.
	receivedAt := time.Now()
	var committedGenerationState api.State
	err := s.withTransaction("update evaluation job", id, func(txn *sql.Tx) error {
		s.logger.Info("Updating evaluation job", "id", id, "status", runStatus.BenchmarkStatusEvent.Status)

		job, err := s.getEvaluationJobTransactionalForUpdate(txn, id)
		if err != nil {
			return err
		}
		// Test hook: no-op unless a test installs a callback (see test_hooks.go).
		invokeEvaluationJobUpdateAfterLockedReadHook(id, runStatus.BenchmarkStatusEvent.ID)
		if workloads.TypeForJob(&job.EvaluationJobConfig) == workloads.CollectionGeneration {
			// Normalization may turn an invalid completion into a failure. Each
			// transaction retry must evaluate the original proposal again.
			event := *runStatus.BenchmarkStatusEvent
			err := s.updateCollectionGenerationJob(txn, job, &api.StatusEvent{BenchmarkStatusEvent: &event}, receivedAt)
			committedGenerationState = event.Status
			return err
		}
		if runStatus.BenchmarkStatusEvent.CollectionGenerationProgress != nil {
			return se.NewServiceError(messages.RequestValidationFailed, "Error", "collection-generation progress is only valid for generation jobs")
		}

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
		s.updateBenchmarkStatus(job, runStatus, &benchmark, receivedAt)

		outcome := s.computeBenchmarkTestResult(txn, job, runStatus.BenchmarkStatusEvent, collection)

		// if the run status is terminal, we need to update the results
		if api.IsBenchmarkTerminalState(runStatus.BenchmarkStatusEvent.Status) {
			result := api.BenchmarkResult{
				ID:             runStatus.BenchmarkStatusEvent.ID,
				ProviderID:     runStatus.BenchmarkStatusEvent.ProviderID,
				Metrics:        runStatus.BenchmarkStatusEvent.Metrics,
				MetricsSchema:  runStatus.BenchmarkStatusEvent.MetricsSchema,
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
		if overallState == api.OverallStateCompleted {
			if err := s.validatePostProcessingSourceCompletion(txn, job); err != nil {
				return se.WithRollback(err)
			}
		}

		return nil
	})
	if err == nil && committedGenerationState != "" {
		// The caller uses the persisted state for runtime phase notifications.
		runStatus.BenchmarkStatusEvent.Status = committedGenerationState
	}
	return err
}

// updateCollectionGenerationJob persists the typed generation result/progress without
// entering evaluation score, collection, or MLflow processing paths.
func (s *sqlStorage) updateCollectionGenerationJob(txn *sql.Tx, job *api.EvaluationJobResource, runStatus *api.StatusEvent, receivedAt time.Time) error {
	event := runStatus.BenchmarkStatusEvent
	if job.Status == nil {
		return se.NewServiceError(messages.RequestValidationFailed, "Error", "collection-generation job has no status")
	}
	if job.Status.State.IsTerminalState() {
		return se.NewServiceError(messages.JobCanNotBeUpdated, "Id", job.Resource.ID, "NewStatus", event.Status, "Status", job.Status.State)
	}
	if _, err := s.checkEvaluationJobState(job.Resource.ID, job.Status.State, api.OverallStateRunning); err != nil {
		return err
	}
	if err := s.validateBenchmarkExists(job, runStatus, nil); err != nil {
		return err
	}
	if err := normalizeCollectionGenerationEvent(job, event, receivedAt); err != nil {
		return se.NewServiceError(messages.RequestValidationFailed, "Error", err.Error())
	}

	if event.Status == api.StateCompleted {
		result, ok := event.AdditionalInfo["collection_generation_result"].(api.CollectionGenerationResult)
		if !ok {
			return errors.New("missing validated collection-generation result")
		}
		if err := s.validateCollectionGenerationCatalog(txn, result); err != nil {
			if !errors.Is(err, errGenerationBenchmarkNotVisible) {
				return se.WithRollback(err)
			}
			setGenerationValidationFailure(event)
		}
	}

	benchmark := api.BenchmarkStatus{
		ProviderID: event.ProviderID, ID: event.ID, Status: event.Status,
		ErrorMessage: event.ErrorMessage, BenchmarkIndex: event.BenchmarkIndex,
	}
	s.updateBenchmarkStatus(job, runStatus, &benchmark, receivedAt)

	var state api.OverallState
	var message *api.MessageInfo
	switch event.Status {
	case api.StateRunning:
		state = api.OverallStateRunning
		message = api.WithMessageOrigin(&api.MessageInfo{Message: "Collection generation is running", MessageCode: "collection_generation_updated"}, api.MessageOriginServer)
	case api.StateCompleted:
		state = api.OverallStateCompleted
		message = api.WithMessageOrigin(&api.MessageInfo{Message: "Collection generation is completed", MessageCode: "collection_generation_completed"}, api.MessageOriginServer)
	case api.StateFailed:
		state = api.OverallStateFailed
		if event.ErrorMessage != nil {
			message = api.WithMessageOrigin(&api.MessageInfo{Message: event.ErrorMessage.Message, MessageCode: event.ErrorMessage.MessageCode}, api.MessageOriginServer)
		}
	}
	job.Status.State = state
	job.Status.Message = message
	if api.IsBenchmarkTerminalState(event.Status) {
		result := api.BenchmarkResult{
			ID: event.ID, ProviderID: event.ProviderID, AdditionalInfo: event.AdditionalInfo,
			BenchmarkIndex: event.BenchmarkIndex,
		}
		if err := s.updateBenchmarkResults(job, runStatus, &result); err != nil {
			return err
		}
	}
	entity := EvaluationJobEntity{Config: &job.EvaluationJobConfig, Status: job.Status, Results: job.Results}
	return s.updateEvaluationJobTxn(txn, job.Resource.ID, state, &entity)
}

var errGenerationBenchmarkNotVisible = errors.New("generation result references a benchmark outside the caller-visible catalog")

func (s *sqlStorage) validateCollectionGenerationCatalog(txn *sql.Tx, result api.CollectionGenerationResult) error {
	benchmarks := make([]api.CollectionBenchmarkConfig, 0, len(result.Collection.Benchmarks)+len(result.Explanation.RejectedBenchmarks))
	benchmarks = append(benchmarks, result.Collection.Benchmarks...)
	for _, rejected := range result.Explanation.RejectedBenchmarks {
		benchmarks = append(benchmarks, api.CollectionBenchmarkConfig{ProviderID: rejected.ProviderID, Ref: api.Ref{ID: rejected.ID}})
	}
	for _, benchmark := range benchmarks {
		provider, err := s.getUserProviderTransactional(txn, benchmark.ProviderID)
		if err != nil {
			var serviceErr *se.ServiceError
			if errors.As(err, &serviceErr) && serviceErr.MessageCode() == messages.ResourceNotFound {
				return errGenerationBenchmarkNotVisible
			}
			return err
		}
		found := false
		for _, catalogBenchmark := range provider.Benchmarks {
			if catalogBenchmark.ID == benchmark.ID {
				found = true
				break
			}
		}
		if !found {
			return errGenerationBenchmarkNotVisible
		}
	}
	return nil
}

func normalizeCollectionGenerationEvent(job *api.EvaluationJobResource, event *api.BenchmarkStatusEvent, receivedAt time.Time) error {
	if event.Metrics != nil || event.MetricsSchema != nil || event.PrimaryScore != nil || event.Artifacts != nil || event.MLFlowRunID != "" || event.LogsPath != "" {
		return fmt.Errorf("collection-generation jobs cannot report evaluation metrics or artifacts")
	}
	if event.WarningMessage != nil || event.Phase != "" || event.StartedAt != "" || event.CompletedAt != "" {
		return fmt.Errorf("generation warnings, evaluation phase, and timestamps are server-managed")
	}
	if event.CollectionGenerationProgress != nil {
		if event.Status != api.StateRunning || event.AdditionalInfo != nil || event.ErrorMessage != nil {
			return fmt.Errorf("generation progress must be a running-only status event")
		}
		previous := job.Status.CollectionGenerationProgress
		if err := generation.ValidateProgressEvent(*event.CollectionGenerationProgress, previous); err != nil {
			return err
		}
		job.Status.CollectionGenerationProgress = ptr(generation.ProgressFromEvent(*event.CollectionGenerationProgress, previous, receivedAt))
		return nil
	}
	if event.Status == api.StateRunning {
		if event.AdditionalInfo != nil || event.ErrorMessage != nil {
			return fmt.Errorf("running generation events cannot include results or errors")
		}
		return nil
	}
	if event.Status != api.StateCompleted && event.Status != api.StateFailed {
		return fmt.Errorf("unsupported collection-generation event state")
	}
	config, err := generation.ConfigFromJob(&job.EvaluationJobConfig)
	if err != nil {
		return fmt.Errorf("invalid collection-generation job snapshot")
	}
	if event.Status == api.StateCompleted {
		if event.ErrorMessage != nil {
			return fmt.Errorf("completed generation events cannot include an error")
		}
		result, err := generation.ResultFromAdditionalInfo(config.Request, event.AdditionalInfo)
		if err != nil {
			setGenerationValidationFailure(event)
			return nil
		}
		event.AdditionalInfo = map[string]any{"collection_generation_result": result}
		return nil
	}
	if _, hasTypedFailure := event.AdditionalInfo["collection_generation_error"]; !hasTypedFailure && event.ErrorMessage != nil {
		// The operator's pod and queue controllers report these failures using
		// the generic status event. Discard their message before storing it.
		switch event.ErrorMessage.MessageCode {
		case "RUNTIME_FAILURE", "queue_error", "gpu_unavailable":
			event.AdditionalInfo = map[string]any{
				"collection_generation_error": api.CollectionGenerationError{Code: "workload_failed"},
			}
		}
	}
	failure, err := generation.FailureFromAdditionalInfo(event.AdditionalInfo)
	if err != nil {
		return fmt.Errorf("invalid generation failure: %w", err)
	}
	event.AdditionalInfo = map[string]any{"collection_generation_error": failure}
	event.ErrorMessage = api.WithMessageOrigin(&api.MessageInfo{
		Message: failure.Message, MessageCode: "collection_generation_" + failure.Code,
	}, api.MessageOriginServer)
	return nil
}

func setGenerationValidationFailure(event *api.BenchmarkStatusEvent) {
	failure, _ := generation.SafeFailure("validation_failed", "")
	event.Status = api.StateFailed
	event.AdditionalInfo = map[string]any{"collection_generation_error": failure}
	event.ErrorMessage = api.WithMessageOrigin(&api.MessageInfo{
		Message: failure.Message, MessageCode: "collection_generation_" + failure.Code,
	}, api.MessageOriginServer)
}

func ptr[T any](value T) *T { return &value }

// UpdateEvaluationJobResolvedSHA records the resolved test-data identity on the benchmark at
// benchmarkIndex as TestDataRef.ResolvedSHA. It stamps the effective benchmark list from
// GetJobBenchmarks (collection definition merged with job overrides, or job.Benchmarks for
// direct jobs). For collection jobs the stamped list is written back to job.Collection.Benchmarks
// so the SHA persists on the job entity. Idempotent: if the SHA is already set the call is a no-op.
func (s *sqlStorage) UpdateEvaluationJobResolvedSHA(id string, benchmarkIndex int, sha string) error {
	if sha == "" {
		return nil
	}
	if err := api.ValidateResolvedSHA(sha); err != nil {
		return err
	}
	return s.withTransaction("update evaluation job resolved sha", id, func(txn *sql.Tx) error {
		job, err := s.getEvaluationJobTransactionalForUpdate(txn, id)
		if err != nil {
			return err
		}

		stamp := func(benchmarks []api.EvaluationBenchmarkConfig, sliceName string) bool {
			if benchmarkIndex < 0 || benchmarkIndex >= len(benchmarks) {
				return false
			}
			b := &benchmarks[benchmarkIndex]
			if b.TestDataRef == nil {
				return false
			}
			if b.TestDataRef.ResolvedSHA != "" {
				s.logger.Info("Resolved SHA already set on benchmark; skipping", "id", id, "slice", sliceName, "benchmark_index", benchmarkIndex)
				return false
			}
			b.TestDataRef.ResolvedSHA = sha
			return true
		}

		var collection *api.CollectionResource
		if job.Collection != nil && job.Collection.ID != "" {
			collection, err = s.getCollectionTransactional(txn, job.Collection.ID)
			if err != nil {
				return err
			}
		}
		benchmarks, err := handlers.GetJobBenchmarks(job, collection)
		if err != nil {
			return err
		}
		updated := stamp(benchmarks, "effective")
		if !updated {
			s.logger.Info("Resolved SHA already set or benchmark has no test_data_ref; skipping update", "id", id, "benchmark_index", benchmarkIndex)
			return nil
		}
		// Materialize the effective list onto the job so collection-local TestDataRef
		// (including ResolvedSHA) is stored with the job, not only on the shared collection.
		if job.Collection != nil {
			job.Collection.Benchmarks = benchmarks
		}
		entity := EvaluationJobEntity{
			Config:  &job.EvaluationJobConfig,
			Status:  job.Status,
			Results: job.Results,
		}
		return s.updateEvaluationJobTxn(txn, id, job.Status.State, &entity)
	})
}
