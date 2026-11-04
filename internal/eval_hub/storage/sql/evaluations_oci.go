package sql

import (
	"database/sql"
	"reflect"

	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	se "github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// UpdateEvaluationJobOCI uses the same tenant-scoped locked read as benchmark updates.
// A status-only write preserves any previously recorded artifact reference.
// The caller must validate the service-owned status and any supplied artifact before calling.
func (s *sqlStorage) UpdateEvaluationJobOCI(id string, status *api.OCIProcessingStatus, card *api.OCIArtifactReference) error {
	if status == nil {
		return se.NewServiceError(messages.InternalServerError, "Error", "OCI processing status is required")
	}
	// OCI workflow status is generated and owned by the service. Normalize the
	// origin before the idempotency comparison so retries with an omitted or
	// spoofed origin remain no-ops and persist trustworthy provenance.
	api.WithMessageOrigin(status.Message, api.MessageOriginServer)
	return s.withTransaction("update evaluation job OCI results", id, func(txn *sql.Tx) error {
		job, err := s.getEvaluationJobTransactionalForUpdate(txn, id)
		if err != nil {
			return err
		}
		if !api.IsEvaluationCardProcessingEligible(job) || !api.IsOCIExportEnabled(job) {
			return se.NewServiceError(messages.InternalServerError, "Error", "OCI processing requires a completed or partially failed job with OCI exports enabled")
		}
		if job.Results != nil && job.Results.OCI != nil {
			results := job.Results.OCI
			if reflect.DeepEqual(results.Status, status) && (card == nil || reflect.DeepEqual(results.EvaluationCard, card)) {
				return nil
			}
			// Completed and failed are terminal OCI outcomes: there are no workflow
			// retries, so stale updates must not transition to a different state.
			if results.Status != nil && results.Status.State != status.State && api.IsOCIProcessingTerminalState(results.Status.State) {
				return se.NewServiceError(messages.JobCanNotBeUpdated, "Id", id, "NewStatus", status.State, "Status", results.Status.State)
			}
		}
		job.Results = api.UpdateOCIResults(job.Results, status, card)
		return s.updateEvaluationJobTxn(txn, id, job.Status.State, &EvaluationJobEntity{
			Config: &job.EvaluationJobConfig, Status: job.Status, Results: job.Results,
		})
	})
}
