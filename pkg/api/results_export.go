package api

// IsResultsExportEligible reports whether a job is in a state eligible for results export.
// Completed and partially failed jobs are eligible, regardless of export configuration.
func IsResultsExportEligible(job *EvaluationJobResource) bool {
	return job != nil && job.Status != nil &&
		(job.Status.State == OverallStateCompleted || job.Status.State == OverallStatePartiallyFailed)
}
