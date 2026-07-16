package constants

const (
	MESSAGE_CODE_EVALUATION_JOB_CREATED   = "evaluation_job_created"
	MESSAGE_CODE_EVALUATION_JOB_RETRIEVED = "evaluation_job_retrieved"
	MESSAGE_CODE_EVALUATION_JOB_CANCELLED = "evaluation_job_cancelled"
	MESSAGE_CODE_EVALUATION_JOB_FAILED    = "evaluation_job_failed"
	MESSAGE_CODE_EVALUATION_JOB_UPDATED   = "evaluation_job_updated"

	// MESSAGE_CODE_GPU_UNAVAILABLE is set when an evaluation job's Kueue workload is inadmissible
	// because the requested queue does not have sufficient GPU capacity.
	MESSAGE_CODE_GPU_UNAVAILABLE = "gpu_unavailable"

	// MESSAGE_CODE_BENCHMARK_TIMEOUT is set when a benchmark is failed by the
	// watchdog because it exceeded the configured staleness timeout.
	MESSAGE_CODE_BENCHMARK_TIMEOUT = "benchmark_timeout"
)
