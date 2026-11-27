package api

import (
	"fmt"
	"regexp"
)

// IsOCIExportEnabled reports whether a job has OCI exports configured.
func IsOCIExportEnabled(job *EvaluationJobResource) bool {
	return job != nil && job.Exports != nil && job.Exports.OCI != nil
}

var ociTagPattern = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9._-]*$`)

// ValidateOCIExportPrefix checks the complete export prefix before any truncation.
// Empty prefixes are allowed. Length is limited only when constructing the final tag.
// The exact prefix "evaluation-card" is reserved for evaluation card artifacts.
func ValidateOCIExportPrefix(prefix string) error {
	if prefix == "evaluation-card" {
		return fmt.Errorf("%q is reserved for evaluation card artifacts", prefix)
	}
	if prefix != "" && !ociTagPattern.MatchString(prefix) {
		return fmt.Errorf("OCI export prefix must match %s", ociTagPattern.String())
	}
	return nil
}

// ValidateOCIManifestTag checks the length and syntax of a complete OCI tag.
func ValidateOCIManifestTag(tag string) error {
	if tag == "" || len(tag) > 128 {
		return fmt.Errorf("OCI manifest tag must contain 1 to 128 characters")
	}
	if !ociTagPattern.MatchString(tag) {
		return fmt.Errorf("OCI manifest tag must match %s", ociTagPattern.String())
	}
	return nil
}

// OCISigningConfig selects the signing method and optionally references a key Secret.
// Signing is pending implementation. Settings are accepted and saved with the job,
// but no secret lookup, key loading, or signing is performed.
type OCISigningConfig struct {
	Type      string  `json:"type" validate:"required,oneof=key-pair"`
	SecretRef *string `json:"secret_ref,omitempty" validate:"omitempty,rfc1123_dns_label"`
}

// OCIArtifactReference identifies an OCI artifact by digest.
type OCIArtifactReference struct {
	OCIDigest    string `json:"oci_digest" validate:"required,sha256_digest"`
	OCIReference string `json:"oci_reference" validate:"required,notblank"`
}

// OCIProcessingState describes the common post-evaluation OCI workflow,
// independently of the evaluation job state.
type OCIProcessingState string

const (
	OCIProcessingRunning   OCIProcessingState = "running"
	OCIProcessingFailed    OCIProcessingState = "failed"
	OCIProcessingCompleted OCIProcessingState = "completed"
)

// IsOCIProcessingTerminalState reports whether an OCI workflow state is a terminal outcome.
func IsOCIProcessingTerminalState(state OCIProcessingState) bool {
	return state == OCIProcessingFailed || state == OCIProcessingCompleted
}

// OCIProcessingStatus reports actual card, bundle, and signing workflow information.
// It has no default state or message.
// Workflow reporting normally uses running followed by completed or failed.
// Status may remain absent or running after interruption or persistence failure.
type OCIProcessingStatus struct {
	State   OCIProcessingState `json:"state" validate:"required,oneof=running failed completed"`
	Message *MessageInfo       `json:"message" validate:"required"`
}

// OCISigningResult reports the method used to sign the evaluation bundle.
type OCISigningResult struct {
	Type string `json:"type" validate:"required,oneof=key-pair"`
}

// EvaluationOCIResults remains unset until artifact and signing workflows supply values.
type EvaluationOCIResults struct {
	Status           *OCIProcessingStatus  `json:"status,omitempty"`
	EvaluationCard   *OCIArtifactReference `json:"evaluation_card,omitempty"`
	EvaluationBundle *OCIArtifactReference `json:"evaluation_bundle,omitempty"`
	Signing          *OCISigningResult     `json:"signing,omitempty"`
}

// EligibleForOCIProcessing reports whether a job has OCI exports configured and
// a completed or partially failed outcome.
func EligibleForOCIProcessing(job *EvaluationJobResource) bool {
	return IsResultsExportEligible(job) && IsOCIExportEnabled(job)
}

// UpdateOCIResults partially merges OCI workflow status, card, and bundle references in memory.
// It performs no field validation or workflow enforcement. Storage updates have a
// stricter contract: UpdateEvaluationJobOCI requires a non-nil, validated status
// and validation of any supplied artifact references, even though this helper
// permits reference-only updates.
// A non-nil card or bundle intentionally replaces the existing reference
// (last-write-wins). Card replacement supports future evaluation-card regeneration;
// the current workflow has no regeneration path yet. Replacement updates the
// stored digest and reference, not the previously published immutable artifact.
// It initializes missing results and preserves all other fields.
// A nil status, card, or bundle leaves the corresponding value unchanged.
// If all three are nil, results are returned unchanged.
func UpdateOCIResults(results *EvaluationJobResults, status *OCIProcessingStatus, card, bundle *OCIArtifactReference) *EvaluationJobResults {
	if status == nil && card == nil && bundle == nil {
		return results
	}
	if results == nil {
		results = &EvaluationJobResults{}
	}
	if results.OCI == nil {
		results.OCI = &EvaluationOCIResults{}
	}
	if status != nil {
		results.OCI.Status = status
	}
	if card != nil {
		results.OCI.EvaluationCard = card
	}
	if bundle != nil {
		results.OCI.EvaluationBundle = bundle
	}
	return results
}
