package sql

import (
	"database/sql"

	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	se "github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func ociArtifactReferencesEquivalent(a, b *api.OCIArtifactReference) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.OCIDigest == b.OCIDigest && a.OCIReference == b.OCIReference
}

// UpdateEvaluationJobOCI uses the same tenant-scoped locked read as benchmark updates.
// It requires a non-nil status, unlike the partial-merge helper api.UpdateOCIResults;
// a nil status is a caller contract violation and returns an internal-server error.
// The caller must validate status fields and any supplied card before calling;
// this method checks status presence and workflow eligibility, not field validity.
// A nil card preserves the existing reference; a supplied card replaces it
// (last-write-wins) when the update is accepted.
func (s *sqlStorage) UpdateEvaluationJobOCI(id string, status *api.OCIProcessingStatus, card *api.OCIArtifactReference) error {
	if status == nil {
		return se.NewServiceError(messages.InternalServerError, "Error", "OCI processing status is required")
	}
	// OCI workflow messages originate from the server. Copy the status and message
	// before setting the origin so the caller's input stays unchanged, even if the
	// update fails. Use the copy for both duplicate-update comparison and persistence.
	serverStatus := *status
	if status.Message != nil {
		message := *status.Message
		serverStatus.Message = api.WithMessageOrigin(&message, api.MessageOriginServer)
	}
	status = &serverStatus
	return s.withTransaction("update evaluation job OCI results", id, func(txn *sql.Tx) error {
		job, err := s.getEvaluationJobTransactionalForUpdate(txn, id)
		if err != nil {
			return err
		}
		if !api.IsResultsExportEligible(job) || !api.IsOCIExportEnabled(job) {
			return se.NewServiceError(
				messages.JobCanNotBeUpdated,
				"Id", id,
				"NewStatus", "updated with OCI processing results",
				"Status", "not completed or partially failed with OCI exports enabled",
			)
		}
		if job.Results != nil && job.Results.OCI != nil {
			results := job.Results.OCI
			sameStatus := results.Status != nil && results.Status.State == status.State &&
				messageInfosEquivalent(results.Status.Message, status.Message)
			if sameStatus && (card == nil || ociArtifactReferencesEquivalent(results.EvaluationCard, card)) {
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
