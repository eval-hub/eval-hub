package constants

const (
	MessageCodeEvaluationJobCreated   = "evaluation_job_created"
	MessageCodeEvaluationJobRetrieved = "evaluation_job_retrieved"
	MessageCodeEvaluationJobCancelled = "evaluation_job_cancelled"
	MessageCodeEvaluationJobFailed    = "evaluation_job_failed"
	MessageCodeEvaluationJobUpdated   = "evaluation_job_updated"

	// MessageCodeGPUUnavailable is set when an evaluation job's Kueue workload is inadmissible
	// because the requested queue does not have sufficient GPU capacity.
	MessageCodeGPUUnavailable = "gpu_unavailable"
)
