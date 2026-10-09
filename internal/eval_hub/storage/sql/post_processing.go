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

// validatePostProcessingSourceCompletion ensures an evaluation input has
// completed before its post-processing job can complete.
func (s *sqlStorage) validatePostProcessingSourceCompletion(txn *sql.Tx, job *api.EvaluationJobResource) error {
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
			s.logger.Warn("post-processing source no longer exists; skipping source validation", "source_id", sourceID, "id", job.Resource.ID)
			return nil
		}
		return err
	}
	if source.Status == nil || source.Status.State != api.OverallStateCompleted {
		return se.NewServiceError(messages.PostProcessingSourceNotCompleted, "Id", sourceID)
	}
	return nil
}
