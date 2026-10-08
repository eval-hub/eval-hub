package sql

import (
	"database/sql"
	"errors"

	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/postprocessing"
	se "github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/internal/eval_hub/workloads"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// linkCompletedPostProcessing runs in the transaction that completes the job.
// Locking the source serializes competing completions: the last successful
// completion replaces the reference without losing any other source-job data.
func (s *sqlStorage) linkCompletedPostProcessing(txn *sql.Tx, job *api.EvaluationJobResource) error {
	if !postprocessing.IsPostProcessingJob(&job.EvaluationJobConfig) {
		return nil
	}
	operations, err := postprocessing.OperationsFromJob(&job.EvaluationJobConfig)
	if err != nil {
		return err
	}
	operation := operations.ConfidenceInterval
	if operation == nil || operation.ResultsDataRef == nil || operation.ResultsDataRef.EvalJob == nil {
		return nil
	}
	sourceID := operation.ResultsDataRef.EvalJob.ID
	if sourceID == job.Resource.ID {
		return se.NewServiceError(messages.RequestValidationFailed, "Error", "post-processing cannot reference itself")
	}
	source, err := s.getEvaluationJobTransactionalForUpdateWithType(txn, sourceID, workloads.Evaluation)
	if err != nil {
		var serviceErr *se.ServiceError
		if errors.As(err, &serviceErr) && serviceErr.MessageCode() == messages.ResourceNotFound {
			s.logger.Warn("post-processing source no longer exists; skipping link", "source_id", sourceID, "id", job.Resource.ID)
			return nil
		}
		return err
	}
	if source.Status == nil || source.Status.State != api.OverallStateCompleted {
		return se.NewServiceError(messages.PostProcessingSourceNotCompleted, "Id", sourceID)
	}
	if source.Results == nil {
		source.Results = &api.EvaluationJobResults{}
	}
	source.Results.PostProcessingRef = &api.PostProcessingRef{ID: job.Resource.ID}
	if err := s.updateEvaluationJobTxn(txn, sourceID, source.Status.State, &EvaluationJobEntity{
		Config:  &source.EvaluationJobConfig,
		Status:  source.Status,
		Results: source.Results,
	}); err != nil {
		return err
	}
	return nil
}

// unlinkDeletedPostProcessingJob clears the referenced evaluation job's link
// when it points to the post-processing job being deleted.
func (s *sqlStorage) unlinkDeletedPostProcessingJob(txn *sql.Tx, postProcessingJob *api.EvaluationJobResource) error {
	if !postprocessing.IsPostProcessingJob(&postProcessingJob.EvaluationJobConfig) {
		return nil
	}
	operations, err := postprocessing.OperationsFromJob(&postProcessingJob.EvaluationJobConfig)
	if err != nil {
		return err
	}
	operation := operations.ConfidenceInterval
	if operation == nil || operation.ResultsDataRef == nil || operation.ResultsDataRef.EvalJob == nil {
		return nil
	}

	sourceEvaluationJobID := operation.ResultsDataRef.EvalJob.ID
	if sourceEvaluationJobID == postProcessingJob.Resource.ID {
		return nil
	}
	sourceEvaluationJob, err := s.getEvaluationJobTransactionalForUpdateWithType(txn, sourceEvaluationJobID, workloads.Evaluation)
	if err != nil {
		var serviceErr *se.ServiceError
		if errors.As(err, &serviceErr) && serviceErr.MessageCode() == messages.ResourceNotFound {
			return nil
		}
		return err
	}
	if sourceEvaluationJob.Results == nil || sourceEvaluationJob.Results.PostProcessingRef == nil || sourceEvaluationJob.Results.PostProcessingRef.ID != postProcessingJob.Resource.ID {
		return nil
	}

	sourceEvaluationJob.Results.PostProcessingRef = nil
	return s.updateEvaluationJobTxn(txn, sourceEvaluationJobID, sourceEvaluationJob.Status.State, &EvaluationJobEntity{
		Config:  &sourceEvaluationJob.EvaluationJobConfig,
		Status:  sourceEvaluationJob.Status,
		Results: sourceEvaluationJob.Results,
	})
}
