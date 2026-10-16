package validation

import (
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// ValidateEvaluationOCITag checks export-prefix syntax without restricting its length.
// Literal OCI artifact references are unaffected.
func ValidateEvaluationOCITag(evaluation *api.EvaluationJobConfig) error {
	if evaluation == nil || evaluation.Exports == nil || evaluation.Exports.OCI == nil {
		return nil
	}
	if err := api.ValidateOCIExportPrefix(evaluation.Exports.OCI.Coordinates.OCITag); err != nil {
		return serviceerrors.NewServiceError(messages.RequestValidationFailed, "Error", "exports.oci.coordinates.oci_tag: "+err.Error())
	}
	return nil
}
