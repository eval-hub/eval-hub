package sql

import (
	"database/sql"
	"fmt"
	"reflect"

	"github.com/eval-hub/eval-hub/pkg/api"
)

// UpdateEvaluationJobOCI uses the same tenant-scoped locked read as benchmark updates.
// A status-only write preserves any previously recorded artifact reference.
func (s *sqlStorage) UpdateEvaluationJobOCI(id string, status *api.OCIProcessingStatus, card *api.OCIArtifactReference) error {
	if status == nil || status.Message == nil || status.Message.Message == "" || status.Message.MessageCode == "" {
		return fmt.Errorf("complete OCI processing status and message are required")
	}
	switch status.State {
	case api.OCIProcessingPending, api.OCIProcessingGeneratingEvaluationCard, api.OCIProcessingGeneratingEvaluationBundle, api.OCIProcessingSigning, api.OCIProcessingFailed, api.OCIProcessingCompleted:
	default:
		return fmt.Errorf("invalid OCI processing state")
	}
	if card != nil && (card.OCIDigest == "" || card.OCIReference == "") {
		return fmt.Errorf("complete OCI artifact reference is required")
	}
	return s.withTransaction("update evaluation job OCI results", id, func(txn *sql.Tx) error {
		job, err := s.getEvaluationJobTransactionalForUpdate(txn, id)
		if err != nil {
			return err
		}
		if job.Status == nil || job.Status.State != api.OverallStateCompleted || job.Exports == nil || job.Exports.OCI == nil {
			return fmt.Errorf("OCI processing requires a completed OCI-enabled evaluation")
		}
		if job.Results == nil {
			job.Results = &api.EvaluationJobResults{}
		}
		if job.Results.OCI == nil {
			job.Results.OCI = &api.EvaluationOCIResults{}
		}
		results := job.Results.OCI
		if reflect.DeepEqual(results.Status, status) && (card == nil || reflect.DeepEqual(results.EvaluationCard, card)) {
			return nil
		}
		results.Status = status
		if card != nil {
			results.EvaluationCard = card
		}
		return s.updateEvaluationJobTxn(txn, id, job.Status.State, &EvaluationJobEntity{
			Config: &job.EvaluationJobConfig, Status: job.Status, Results: job.Results,
		})
	})
}
