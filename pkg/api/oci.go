package api

import (
	"fmt"
	"regexp"
)

var ociExportPrefixPattern = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9._-]*$`)

// ValidateOCIExportPrefix checks the complete export prefix before any truncation.
// Empty prefixes are allowed. Length is limited only when constructing the final tag.
func ValidateOCIExportPrefix(prefix string) error {
	if prefix != "" && !ociExportPrefixPattern.MatchString(prefix) {
		return fmt.Errorf("OCI export prefix must match %s", ociExportPrefixPattern.String())
	}
	return nil
}

// OCISigningConfig selects the signing method and optionally references a key Secret.
// It retains configuration only; it does not resolve keys or schedule signing.
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

// OCIProcessingStatus reports actual card, bundle, and signing workflow information.
// It has no default state or message.
// Workflow reporting uses running followed by completed or failed.
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

// UpdateOCIResults updates workflow status and any supplied card and bundle references.
// It initializes missing results and preserves all other fields. A nil reference
// leaves the existing output unchanged; this function does not reset OCI results.
func UpdateOCIResults(results *EvaluationJobResults, status *OCIProcessingStatus, card, bundle *OCIArtifactReference) *EvaluationJobResults {
	if results == nil {
		results = &EvaluationJobResults{}
	}
	if results.OCI == nil {
		results.OCI = &EvaluationOCIResults{}
	}
	results.OCI.Status = status
	if card != nil {
		results.OCI.EvaluationCard = card
	}
	if bundle != nil {
		results.OCI.EvaluationBundle = bundle
	}
	return results
}
