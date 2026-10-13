package sql

import (
	"database/sql"
	"fmt"
	"reflect"

	"github.com/eval-hub/eval-hub/internal/eval_hub/validation"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// UpdateEvaluationJobOCI uses the same tenant-scoped locked read as benchmark updates.
// A status-only write preserves any previously recorded artifact reference.
func (s *sqlStorage) UpdateEvaluationJobOCI(id string, status *api.OCIProcessingStatus, card *api.OCIArtifactReference) error {
	if status == nil || status.Message == nil || status.Message.Message == "" || status.Message.MessageCode == "" {
		return fmt.Errorf("complete OCI processing status and message are required")
	}
	switch status.State {
	case api.OCIProcessingRunning, api.OCIProcessingFailed, api.OCIProcessingCompleted:
	default:
		return fmt.Errorf("invalid OCI processing state")
	}
	if card != nil {
		validate, err := validation.NewValidator()
		if err != nil {
			return fmt.Errorf("create OCI artifact validator: %w", err)
		}
		if err := validate.Struct(card); err != nil {
			return fmt.Errorf("invalid OCI artifact reference: %w", err)
		}
	}
	return s.withTransaction("update evaluation job OCI results", id, func(txn *sql.Tx) error {
		job, err := s.getEvaluationJobTransactionalForUpdate(txn, id)
		if err != nil {
			return err
		}
		if job.Status == nil || job.Status.State != api.OverallStateCompleted || job.Exports == nil || job.Exports.OCI == nil {
			return fmt.Errorf("OCI processing requires a completed OCI-enabled evaluation")
		}
		if job.Results != nil && job.Results.OCI != nil {
			results := job.Results.OCI
			if reflect.DeepEqual(results.Status, status) && (card == nil || reflect.DeepEqual(results.EvaluationCard, card)) {
				return nil
			}
		}
		job.Results = api.UpdateOCIResults(job.Results, status, card)
		return s.updateEvaluationJobTxn(txn, id, job.Status.State, &EvaluationJobEntity{
			Config: &job.EvaluationJobConfig, Status: job.Status, Results: job.Results,
		})
	})
}
