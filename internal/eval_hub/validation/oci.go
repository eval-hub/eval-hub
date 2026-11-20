package validation

import (
	"strings"

	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// ValidateEvaluationOCISigning also checks inherited exports, which may bypass
// request decoding. It validates syntax and mode only, never key accessibility.
func ValidateEvaluationOCISigning(evaluation *api.EvaluationJobConfig, localMode bool) error {
	if evaluation == nil || evaluation.Exports == nil || evaluation.Exports.OCI == nil || evaluation.Exports.OCI.Signing == nil {
		return nil
	}
	signing := evaluation.Exports.OCI.Signing
	reason := ""
	switch {
	case signing.Type != "key-pair":
		reason = "only key-pair signing is supported"
	case signing.SecretRef == nil || *signing.SecretRef == "":
		reason = "key-pair signing requires secret_ref"
	default:
		if err := api.ValidateOCISigningReference(*signing.SecretRef); err != nil {
			reason = err.Error()
		} else if !localMode && strings.HasPrefix(*signing.SecretRef, "file:") {
			reason = "file URIs are supported only in local mode"
		}
	}
	if reason != "" {
		return serviceerrors.NewServiceError(messages.RequestValidationFailed, "Error", "exports.oci.signing: "+reason)
	}
	return nil
}

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
