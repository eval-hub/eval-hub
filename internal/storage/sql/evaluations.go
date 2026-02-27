package sql

import (
	"encoding/json"
	"fmt"
	"time"

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

func CreateEvaluationJobEntity(evaluation *api.EvaluationJobResource) ([]byte, error) {
	evaluationEntity := &EvaluationJobEntity{
		Config:  &evaluation.EvaluationJobConfig,
		Status:  evaluation.Status,
		Results: evaluation.Results,
	}
	evaluationJSON, err := json.Marshal(evaluationEntity)
	if err != nil {
		return nil, serviceerrors.NewServiceError(messages.InternalServerError, "Error", err.Error())
	}
	return evaluationJSON, nil
}

func ConstructEvaluationResource(tenantID string,
	statusStr string,
	message *api.MessageInfo,
	dbID string,
	createdAt time.Time,
	updatedAt time.Time,
	experimentID string,
	evaluationEntity *EvaluationJobEntity) (*api.EvaluationJobResource, error) {
	if evaluationEntity == nil {
		// Post-read validation: no writes done, so do not request rollback.
		return nil, serviceerrors.NewServiceError(messages.InternalServerError, "Error", "Evaluation entity does not exist")
	}
	if evaluationEntity.Config == nil {
		// Post-read validation: no writes done, so do not request rollback.
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

func ApplyStatusEventToEvaluationJob(job *api.EvaluationJobResource, runStatus *api.StatusEvent, e SQLExecutor) (*EvaluationJobEntity, error) {

	err := ValidateBenchmarkExists(job, runStatus)
	if err != nil {
		return nil, err
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
	UpdateBenchmarkStatus(job, runStatus, &benchmark)

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
		err := UpdateBenchmarkResults(job, runStatus, &result)
		if err != nil {
			return nil, err
		}
	}

	// get the overall job status
	overallState, message := GetOverallJobStatus(job)
	job.Status.State = overallState
	job.Status.Message = message

	entity := EvaluationJobEntity{
		Config:  &job.EvaluationJobConfig,
		Status:  job.Status,
		Results: job.Results,
	}

	return &entity, nil

}

func GetOverallJobStatus(job *api.EvaluationJobResource) (api.OverallState, *api.MessageInfo) {
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

func ValidateBenchmarkExists(job *api.EvaluationJobResource, runStatus *api.StatusEvent) error {
	found := false
	for index, benchmark := range job.Benchmarks {
		if benchmark.ID == runStatus.BenchmarkStatusEvent.ID &&
			benchmark.ProviderID == runStatus.BenchmarkStatusEvent.ProviderID &&
			index == runStatus.BenchmarkStatusEvent.BenchmarkIndex {
			found = true
			break
		}
	}
	if !found {
		return serviceerrors.NewServiceError(messages.ResourceNotFound, "Type", "benchmark", "ResourceId", runStatus.BenchmarkStatusEvent.ID, "Error", "Invalid Benchmark for the evaluation job")
	}
	return nil
}

func UpdateBenchmarkResults(job *api.EvaluationJobResource, runStatus *api.StatusEvent, result *api.BenchmarkResult) error {
	if job.Results == nil {
		job.Results = &api.EvaluationJobResults{}
	}
	if job.Results.Benchmarks == nil {
		job.Results.Benchmarks = make([]api.BenchmarkResult, 0)
	}

	for _, benchmark := range job.Results.Benchmarks {
		if benchmark.ID == runStatus.BenchmarkStatusEvent.ID &&
			benchmark.ProviderID == runStatus.BenchmarkStatusEvent.ProviderID &&
			benchmark.BenchmarkIndex == runStatus.BenchmarkStatusEvent.BenchmarkIndex {
			// we should never get here because the final result
			// can not change, hence we treat this as an error for now
			return serviceerrors.NewServiceError(messages.InternalServerError, "Error", fmt.Sprintf("Benchmark result already exists for benchmark[%d] %s in job %s", runStatus.BenchmarkStatusEvent.BenchmarkIndex, runStatus.BenchmarkStatusEvent.ID, job.Resource.ID))
		}
	}
	job.Results.Benchmarks = append(job.Results.Benchmarks, *result)

	return nil
}

func UpdateBenchmarkStatus(job *api.EvaluationJobResource, runStatus *api.StatusEvent, benchmarkStatus *api.BenchmarkStatus) {
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
