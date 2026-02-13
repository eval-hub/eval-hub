package common

import (
	"fmt"

	"github.com/eval-hub/eval-hub/internal/constants"
	"github.com/eval-hub/eval-hub/internal/messages"
	"github.com/eval-hub/eval-hub/internal/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
)

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
	for _, benchmark := range job.Benchmarks {
		if benchmark.ID == runStatus.BenchmarkStatusEvent.ID &&
			benchmark.ProviderID == runStatus.BenchmarkStatusEvent.ProviderID {
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
			benchmark.ProviderID == runStatus.BenchmarkStatusEvent.ProviderID {
			// we should never get here because the final result
			// can not change, hence we treat this as an error for now
			return serviceerrors.NewServiceError(messages.InternalServerError, "Error", fmt.Sprintf("Benchmark result already exists for benchmark %s in job %s", runStatus.BenchmarkStatusEvent.ID, job.Resource.ID))
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
			benchmark.ProviderID == runStatus.BenchmarkStatusEvent.ProviderID {
			job.Status.Benchmarks[index] = *benchmarkStatus
			return
		}
	}
	job.Status.Benchmarks = append(job.Status.Benchmarks, *benchmarkStatus)
}
